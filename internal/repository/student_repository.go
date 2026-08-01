package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type studentRepository struct{ db *gorm.DB }

func NewStudentRepository(db *gorm.DB) StudentRepository { return &studentRepository{db: db} }

func (r *studentRepository) Create(ctx context.Context, student *domain.Student) error {
	return r.db.WithContext(ctx).Create(student).Error
}

func (r *studentRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Student, error) {
	var student domain.Student
	if err := r.db.WithContext(ctx).First(&student, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &student, nil
}

func (r *studentRepository) Update(ctx context.Context, student *domain.Student) error {
	return r.db.WithContext(ctx).Save(student).Error
}

func (r *studentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&domain.Student{}, "id = ?", id).Error
}
