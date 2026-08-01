package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

type ClassUsecase interface {
	CreateClass(ctx context.Context, tenantID uuid.UUID, req *domain.CreateClassRequest) (*domain.ClassResponse, error)
}

type classUsecase struct {
	classRepo    repository.ClassRepository
	tenantClient grpcclient.TenantClient
}

func NewClassUsecase(classRepo repository.ClassRepository, tenantClient grpcclient.TenantClient) ClassUsecase {
	return &classUsecase{
		classRepo:    classRepo,
		tenantClient: tenantClient,
	}
}

func (u *classUsecase) CreateClass(ctx context.Context, tenantID uuid.UUID, req *domain.CreateClassRequest) (*domain.ClassResponse, error) {
	// 1. Validate tenant status via gRPC
	isActive, _, err := u.tenantClient.ValidateTenantStatus(ctx, tenantID.String())
	if err != nil || !isActive {
		return nil, ErrTenantInactiveOrNotFound
	}

	// 2. Create Class
	class := &domain.Class{
		ID:          uuid.New(),
		TenantID:    tenantID,
		CategoryID:  req.CategoryID,
		Name:        req.Name,
		Description: req.Description,
		Type:        req.Type,
		Price:       req.Price,
		Capacity:    req.Capacity,
	}

	if err := u.classRepo.Create(ctx, class); err != nil {
		return nil, err
	}

	return &domain.ClassResponse{
		ID:          class.ID,
		TenantID:    class.TenantID,
		CategoryID:  class.CategoryID,
		Name:        class.Name,
		Description: class.Description,
		Type:        class.Type,
		Price:       class.Price,
		Capacity:    class.Capacity,
		CreatedAt:   class.CreatedAt,
	}, nil
}
