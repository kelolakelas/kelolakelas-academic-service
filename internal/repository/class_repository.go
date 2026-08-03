package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type classRepository struct {
	db *gorm.DB
}

func (r *classRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID, query domain.ListQuery) ([]domain.Class, int64, error) {
	db := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if query.Search != "" {
		db = db.Where("name ILIKE ?", "%"+query.Search+"%")
	}
	var total int64
	if err := db.Model(&domain.Class{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []domain.Class
	if err := db.Preload("Category").Order("created_at DESC").Limit(query.PageSize).Offset((query.Page - 1) * query.PageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func NewClassRepository(db *gorm.DB) ClassRepository {
	return &classRepository{db: db}
}

func (r *classRepository) Create(ctx context.Context, class *domain.Class) error {
	return r.getDB(ctx).Create(class).Error
}

func (r *classRepository) getDB(ctx context.Context) *gorm.DB {
	return GetDB(ctx, r.db)
}

func (r *classRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Class, error) {
	var class domain.Class
	if err := r.getDB(ctx).Preload("Category").First(&class, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &class, nil
}

func (r *classRepository) Update(ctx context.Context, class *domain.Class) error {
	return r.getDB(ctx).Save(class).Error
}

func (r *classRepository) DeleteByTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	return r.getDB(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&domain.Class{}).Error
}
