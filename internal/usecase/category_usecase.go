package usecase

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
	"gorm.io/gorm"
)

var (
	ErrTenantInactiveOrNotFound = errors.New("Tenant is inactive or not found")
)

type CategoryUsecase interface {
	CreateCategory(ctx context.Context, tenantID uuid.UUID, req *domain.CreateCategoryRequest) (*domain.CategoryResponse, error)
	ListCategories(ctx context.Context, tenantID uuid.UUID, query domain.ListQuery) (*domain.CategoryListResponse, error)
	UpdateCategory(ctx context.Context, tenantID, id uuid.UUID, req *domain.UpdateCategoryRequest) (*domain.CategoryResponse, error)
	DeleteCategory(ctx context.Context, tenantID, id uuid.UUID) error
}

func (u *categoryUsecase) ListCategories(ctx context.Context, tenantID uuid.UUID, query domain.ListQuery) (*domain.CategoryListResponse, error) {
	items, total, err := u.categoryRepo.ListByTenant(ctx, tenantID, query)
	if err != nil {
		return nil, err
	}
	responses := make([]domain.CategoryResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, domain.CategoryResponse{ID: item.ID, TenantID: item.TenantID, Name: item.Name, Description: item.Description, CreatedAt: item.CreatedAt})
	}
	return &domain.CategoryListResponse{Items: responses, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}}, nil
}

type categoryUsecase struct {
	categoryRepo repository.CategoryRepository
	tenantClient grpcclient.TenantClient
	txManager    repository.TransactionManager
}

func NewCategoryUsecase(categoryRepo repository.CategoryRepository, tenantClient grpcclient.TenantClient, txManager repository.TransactionManager) CategoryUsecase {
	return &categoryUsecase{
		categoryRepo: categoryRepo,
		tenantClient: tenantClient,
		txManager:    txManager,
	}
}

func (u *categoryUsecase) DeleteCategory(ctx context.Context, tenantID, id uuid.UUID) error {
	return u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		category, err := u.categoryRepo.GetByID(txCtx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) || category == nil {
			return domain.ErrCategoryNotFound
		}
		if err != nil {
			return err
		}
		if category.TenantID != tenantID {
			return domain.ErrCategoryForbidden
		}
		count, err := u.categoryRepo.CountActiveClasses(txCtx, id)
		if err != nil {
			return err
		}
		if count > 0 {
			return domain.ErrCategoryActiveClasses
		}
		return u.categoryRepo.DeleteByTenant(txCtx, tenantID, id)
	})
}

// UpdateCategory patches the name/description of a tenant-owned category.
//
// Only the fields the caller actually sent are applied, so an update can never
// blank an attribute by omission. Classes reference categories by id, so a
// rename never touches any class row: existing classes keep pointing at the
// same category id. The write goes through UpdateByTenant, which repeats the
// tenant predicate at the database layer.
func (u *categoryUsecase) UpdateCategory(ctx context.Context, tenantID, id uuid.UUID, req *domain.UpdateCategoryRequest) (*domain.CategoryResponse, error) {
	if req == nil {
		return nil, domain.ErrCategoryNameRequired
	}
	var response *domain.CategoryResponse
	err := u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		category, err := u.categoryRepo.GetByID(txCtx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) || category == nil {
			return domain.ErrCategoryNotFound
		}
		if err != nil {
			return err
		}
		if category.TenantID != tenantID {
			return domain.ErrCategoryForbidden
		}

		if req.Name != nil {
			if strings.TrimSpace(*req.Name) == "" {
				return domain.ErrCategoryNameRequired
			}
			category.Name = *req.Name
		}
		if req.Description != nil {
			description := *req.Description
			category.Description = &description
		}

		if err := u.categoryRepo.UpdateByTenant(txCtx, category); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrCategoryNotFound
			}
			return err
		}

		response = categoryResponseFrom(category)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func categoryResponseFrom(category *domain.Category) *domain.CategoryResponse {
	return &domain.CategoryResponse{
		ID:          category.ID,
		TenantID:    category.TenantID,
		Name:        category.Name,
		Description: category.Description,
		CreatedAt:   category.CreatedAt,
	}
}

func (u *categoryUsecase) CreateCategory(ctx context.Context, tenantID uuid.UUID, req *domain.CreateCategoryRequest) (*domain.CategoryResponse, error) {
	// 1. Validate tenant status via gRPC
	isActive, _, err := u.tenantClient.ValidateTenantStatus(ctx, tenantID.String())
	if err != nil || !isActive {
		return nil, ErrTenantInactiveOrNotFound
	}

	// 2. Create Category
	category := &domain.Category{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
	}

	if err := u.categoryRepo.Create(ctx, category); err != nil {
		return nil, err
	}

	return &domain.CategoryResponse{
		ID:          category.ID,
		TenantID:    category.TenantID,
		Name:        category.Name,
		Description: category.Description,
		CreatedAt:   category.CreatedAt,
	}, nil
}
