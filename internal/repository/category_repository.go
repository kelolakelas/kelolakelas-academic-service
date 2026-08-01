package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type categoryRepository struct {
	db *gorm.DB
}

func NewCategoryRepository(db *gorm.DB) CategoryRepository {
	return &categoryRepository{db: db}
}

func (r *categoryRepository) Create(ctx context.Context, category *domain.Category) error {
	return r.db.WithContext(ctx).Create(category).Error
}

func (r *categoryRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Category, error) {
	var category domain.Category
	if err := r.db.WithContext(ctx).First(&category, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &category, nil
}

func (r *categoryRepository) Update(ctx context.Context, category *domain.Category) error {
	return r.db.WithContext(ctx).Save(category).Error
}

func (r *categoryRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&domain.Category{}, "id = ?", id).Error
}
