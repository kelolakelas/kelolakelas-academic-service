package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tutorin-id/tutorin-academic-service/internal/domain"
)

type classRepository struct {
	db *gorm.DB
}

func NewClassRepository(db *gorm.DB) ClassRepository {
	return &classRepository{db: db}
}

func (r *classRepository) Create(ctx context.Context, class *domain.Class) error {
	return r.db.WithContext(ctx).Create(class).Error
}

func (r *classRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Class, error) {
	var class domain.Class
	if err := r.db.WithContext(ctx).Preload("Category").First(&class, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &class, nil
}

func (r *classRepository) Update(ctx context.Context, class *domain.Class) error {
	return r.db.WithContext(ctx).Save(class).Error
}

func (r *classRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&domain.Class{}, "id = ?", id).Error
}
