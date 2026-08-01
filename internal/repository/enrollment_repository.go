package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type enrollmentRepository struct {
	db *gorm.DB
}

func NewEnrollmentRepository(db *gorm.DB) EnrollmentRepository {
	return &enrollmentRepository{db: db}
}

func (r *enrollmentRepository) getDB(ctx context.Context) *gorm.DB {
	return GetDB(ctx, r.db)
}

func (r *enrollmentRepository) Create(ctx context.Context, enrollment *domain.Enrollment) error {
	return r.getDB(ctx).Create(enrollment).Error
}

func (r *enrollmentRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Enrollment, error) {
	var enrollment domain.Enrollment
	if err := r.getDB(ctx).Preload("Student").Preload("Class").First(&enrollment, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &enrollment, nil
}

func (r *enrollmentRepository) GetActiveByClassID(ctx context.Context, classID uuid.UUID) ([]*domain.Enrollment, error) {
	var enrollments []*domain.Enrollment
	if err := r.getDB(ctx).Preload("Student").Where("class_id = ? AND status = ?", classID, "active").Find(&enrollments).Error; err != nil {
		return nil, err
	}
	return enrollments, nil
}

func (r *enrollmentRepository) Update(ctx context.Context, enrollment *domain.Enrollment) error {
	return r.getDB(ctx).Save(enrollment).Error
}

func (r *enrollmentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.getDB(ctx).Delete(&domain.Enrollment{}, "id = ?", id).Error
}
