package usecase

import (
	"context"
	"errors"
	"math"
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
	}, nil
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
