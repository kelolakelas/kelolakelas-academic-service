package usecase

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
	"gorm.io/gorm"
)

type ClassUsecase interface {
	CreateClass(ctx context.Context, tenantID uuid.UUID, req *domain.CreateClassRequest) (*domain.ClassResponse, error)
	ListClasses(ctx context.Context, tenantID uuid.UUID, query domain.ListQuery) (*domain.ClassListResponse, error)
	DeleteClass(ctx context.Context, tenantID, id uuid.UUID) error
	UpdateClassPublication(ctx context.Context, tenantID, id uuid.UUID, req *domain.UpdateClassPublicationRequest) (*domain.ClassResponse, error)
	UpdateClass(ctx context.Context, tenantID, id uuid.UUID, req *domain.UpdateClassRequest) (*domain.ClassResponse, error)
}

func (u *classUsecase) ListClasses(ctx context.Context, tenantID uuid.UUID, query domain.ListQuery) (*domain.ClassListResponse, error) {
	items, total, err := u.classRepo.ListByTenant(ctx, tenantID, query)
	if err != nil {
		return nil, err
	}
	responses := make([]domain.ClassResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, domain.ClassResponse{ID: item.ID, TenantID: item.TenantID, CategoryID: item.CategoryID, Name: item.Name, Description: item.Description, Type: item.Type, Price: item.Price, CreatedAt: item.CreatedAt})
		responses[len(responses)-1].IsPublished = item.IsPublished
		responses[len(responses)-1].EnrollmentStatus = item.EnrollmentStatus
	}
	return &domain.ClassListResponse{Items: responses, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}}, nil
}

type classUsecase struct {
	classRepo      repository.ClassRepository
	categoryRepo   repository.CategoryRepository
	scheduleRepo   repository.ScheduleRepository
	sessionRepo    repository.SessionRepository
	enrollmentRepo repository.EnrollmentRepository
	tenantClient   grpcclient.TenantClient
	txManager      repository.TransactionManager
}

func NewClassUsecase(classRepo repository.ClassRepository, scheduleRepo repository.ScheduleRepository, sessionRepo repository.SessionRepository, enrollmentRepo repository.EnrollmentRepository, tenantClient grpcclient.TenantClient, txManager repository.TransactionManager, categoryRepos ...repository.CategoryRepository) ClassUsecase {
	var categoryRepo repository.CategoryRepository
	if len(categoryRepos) > 0 {
		categoryRepo = categoryRepos[0]
	}
	return &classUsecase{
		classRepo:      classRepo,
		categoryRepo:   categoryRepo,
		scheduleRepo:   scheduleRepo,
		sessionRepo:    sessionRepo,
		enrollmentRepo: enrollmentRepo,
		tenantClient:   tenantClient,
		txManager:      txManager,
	}
}

func (u *classUsecase) DeleteClass(ctx context.Context, tenantID, id uuid.UUID) error {
	return u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		class, err := u.classRepo.GetByID(txCtx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) || class == nil {
			return domain.ErrClassNotFound
		}
		if err != nil {
			return err
		}
		if class.TenantID != tenantID {
			return domain.ErrClassForbidden
		}
		enrollments, err := u.enrollmentRepo.GetActiveByClassID(txCtx, id)
		if err != nil {
			return err
		}
		if len(enrollments) > 0 {
			return domain.ErrClassActiveEnrollments
		}
		if err := u.scheduleRepo.DeleteByClass(txCtx, tenantID, id); err != nil {
			return err
		}
		if err := u.sessionRepo.CancelFutureSessionsByClass(txCtx, id, normalizeDate(time.Now())); err != nil {
			return err
		}
		return u.classRepo.DeleteByTenant(txCtx, tenantID, id)
	})
}

func (u *classUsecase) UpdateClassPublication(ctx context.Context, tenantID, id uuid.UUID, req *domain.UpdateClassPublicationRequest) (*domain.ClassResponse, error) {
	class, err := u.classRepo.UpdatePublicationStatus(ctx, id, tenantID, *req.IsPublished)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrClassNotFound
		}
		return nil, err
	}
	if class == nil {
		return nil, domain.ErrClassNotFound
	}
	return classResponseFrom(class), nil
}

// UpdateClass patches the sellable attributes of a tenant-owned class.
//
// Only the fields the caller actually sent are applied, so an update can never
// wipe an attribute by omission. Class type is intentionally validated but not
// writable: switching between private and group changes schedule and capacity
// obligations that already exist (see domain.ErrClassTypeImmutable).
//
// Price changes are forward-only by design. Enrollments persist their own
// gross_amount snapshot at creation time, so raising or lowering a class price
// never re-prices an enrollment that already exists.
func (u *classUsecase) UpdateClass(ctx context.Context, tenantID, id uuid.UUID, req *domain.UpdateClassRequest) (*domain.ClassResponse, error) {
	if u.categoryRepo == nil {
		return nil, errors.New("class update requires a category repository")
	}

	var response *domain.ClassResponse
	err := u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		class, err := u.classRepo.GetByID(txCtx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) || class == nil {
			return domain.ErrClassNotFound
		}
		if err != nil {
			return err
		}
		// A class owned by another tenant is reported as not found rather than
		// forbidden so the endpoint does not confirm the existence of foreign rows.
		if class.TenantID != tenantID {
			return domain.ErrClassNotFound
		}

		if req.CategoryID != nil && *req.CategoryID != class.CategoryID {
			category, err := u.categoryRepo.GetByID(txCtx, *req.CategoryID)
			if errors.Is(err, gorm.ErrRecordNotFound) || category == nil {
				return domain.ErrCategoryNotFound
			}
			if err != nil {
				return err
			}
			// Soft-deleted categories are excluded by GORM's default scope, so a
			// missing row already covers the deleted-category edge case.
			if category.TenantID != tenantID {
				return domain.ErrCategoryForbidden
			}
			class.CategoryID = *req.CategoryID
		}

		if req.Type != nil && *req.Type != class.Type {
			return domain.ErrClassTypeImmutable
		}

		if req.Name != nil {
			if strings.TrimSpace(*req.Name) == "" {
				return domain.ErrClassNameRequired
			}
			class.Name = *req.Name
		}
		if req.Description != nil {
			description := *req.Description
			class.Description = &description
		}
		if req.Price != nil {
			if *req.Price < 0 {
				return domain.ErrInvalidClassPrice
			}
			class.Price = *req.Price
		}

		if err := u.classRepo.UpdateByTenant(txCtx, class); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrClassNotFound
			}
			return err
		}

		response = classResponseFrom(class)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func classResponseFrom(class *domain.Class) *domain.ClassResponse {
	return &domain.ClassResponse{
		ID:               class.ID,
		TenantID:         class.TenantID,
		CategoryID:       class.CategoryID,
		Name:             class.Name,
		Description:      class.Description,
		Type:             class.Type,
		Price:            class.Price,
		CreatedAt:        class.CreatedAt,
		IsPublished:      class.IsPublished,
		EnrollmentStatus: class.EnrollmentStatus,
	}
}

func (u *classUsecase) CreateClass(ctx context.Context, tenantID uuid.UUID, req *domain.CreateClassRequest) (*domain.ClassResponse, error) {
	// 1. Validate tenant status via gRPC
	isActive, _, err := u.tenantClient.ValidateTenantStatus(ctx, tenantID.String())
	if err != nil || !isActive {
		return nil, ErrTenantInactiveOrNotFound
	}
	if u.categoryRepo != nil {
		category, err := u.categoryRepo.GetByID(ctx, req.CategoryID)
		if errors.Is(err, gorm.ErrRecordNotFound) || category == nil {
			return nil, domain.ErrCategoryNotFound
		}
		if err != nil {
			return nil, err
		}
		if category.TenantID != tenantID {
			return nil, domain.ErrCategoryForbidden
		}
	}

	// 2. Create Class
	class := &domain.Class{
		ID:               uuid.New(),
		TenantID:         tenantID,
		CategoryID:       req.CategoryID,
		Name:             req.Name,
		Description:      req.Description,
		Type:             req.Type,
		Price:            req.Price,
		IsPublished:      req.IsPublished,
		EnrollmentStatus: req.EnrollmentStatus,
	}
	if class.EnrollmentStatus == "" {
		class.EnrollmentStatus = "open"
	}

	if err := u.classRepo.Create(ctx, class); err != nil {
		return nil, err
	}

	return &domain.ClassResponse{
		ID:               class.ID,
		TenantID:         class.TenantID,
		CategoryID:       class.CategoryID,
		Name:             class.Name,
		Description:      class.Description,
		Type:             class.Type,
		Price:            class.Price,
		IsPublished:      class.IsPublished,
		EnrollmentStatus: class.EnrollmentStatus,
		CreatedAt:        class.CreatedAt,
	}, nil
}
