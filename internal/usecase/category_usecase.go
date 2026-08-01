package usecase

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

var (
	ErrTenantInactiveOrNotFound = errors.New("Tenant is inactive or not found")
)

type CategoryUsecase interface {
	CreateCategory(ctx context.Context, tenantID uuid.UUID, req *domain.CreateCategoryRequest) (*domain.CategoryResponse, error)
}

type categoryUsecase struct {
	categoryRepo repository.CategoryRepository
	tenantClient grpcclient.TenantClient
}

func NewCategoryUsecase(categoryRepo repository.CategoryRepository, tenantClient grpcclient.TenantClient) CategoryUsecase {
	return &categoryUsecase{
		categoryRepo: categoryRepo,
		tenantClient: tenantClient,
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
