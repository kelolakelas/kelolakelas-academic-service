package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type categoryRepository struct {
	db *gorm.DB
}

func (r *categoryRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID, query domain.ListQuery) ([]domain.Category, int64, error) {
	db := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if query.Search != "" {
		db = db.Where("name ILIKE ? ESCAPE '\\'", "%"+escapeLikePattern(query.Search)+"%")
	}
	var total int64
	if err := db.Model(&domain.Category{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []domain.Category
	if err := db.Order("created_at DESC").Limit(query.PageSize).Offset((query.Page - 1) * query.PageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func NewCategoryRepository(db *gorm.DB) CategoryRepository {
	return &categoryRepository{db: db}
}

func (r *categoryRepository) Create(ctx context.Context, category *domain.Category) error {
	return r.getDB(ctx).Create(category).Error
}

func (r *categoryRepository) getDB(ctx context.Context) *gorm.DB {
	return GetDB(ctx, r.db)
}

func (r *categoryRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Category, error) {
	var category domain.Category
	if err := r.getDB(ctx).First(&category, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &category, nil
}

func (r *categoryRepository) Update(ctx context.Context, category *domain.Category) error {
	return r.getDB(ctx).Save(category).Error
}

// UpdateByTenant persists the mutable columns of a category that the caller has
// already loaded and authorized.
//
// The WHERE clause repeats the tenant scope so a concurrent tenant change (or a
// mistake in the caller) cannot write across tenants, and a zero row count is
// reported as gorm.ErrRecordNotFound instead of a silent no-op.
func (r *categoryRepository) UpdateByTenant(ctx context.Context, category *domain.Category) error {
	db := r.getDB(ctx).Model(&domain.Category{}).
		Where("id = ? AND tenant_id = ?", category.ID, category.TenantID).
		Updates(map[string]interface{}{
			"name":        category.Name,
			"description": category.Description,
			"updated_at":  time.Now(),
		})
	if db.Error != nil {
		return db.Error
	}
	if db.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *categoryRepository) CountActiveClasses(ctx context.Context, categoryID uuid.UUID) (int64, error) {
	var count int64
	err := r.getDB(ctx).Model(&domain.Class{}).Where("category_id = ?", categoryID).Count(&count).Error
	return count, err
}

func (r *categoryRepository) DeleteByTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	return r.getDB(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&domain.Category{}).Error
}
