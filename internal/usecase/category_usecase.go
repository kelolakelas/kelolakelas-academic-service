package usecase

import (
	"context"
	"errors"
	"math"

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
