package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type studentRepository struct{ db *gorm.DB }

func NewStudentRepository(db *gorm.DB) StudentRepository { return &studentRepository{db: db} }

func (r *studentRepository) Create(ctx context.Context, student *domain.Student) error {
	return GetDB(ctx, r.db).Create(student).Error
}

func (r *studentRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Student, error) {
	var student domain.Student
	if err := GetDB(ctx, r.db).First(&student, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrStudentNotFound
		}
		return nil, err
	}
	return &student, nil
}

func (r *studentRepository) accessQuery(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID) *gorm.DB {
	db := r.db.WithContext(ctx).Where("students.id = ?", id)
	if parentID != nil {
		db = db.Where("students.parent_id = ?", *parentID)
	}
	if tenantID != nil {
		db = db.Joins("JOIN enrollments e ON e.student_id = students.id").Where("e.tenant_id = ?", *tenantID)
	}
	return db
}

func (r *studentRepository) GetByIDForAccess(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID) (*domain.Student, error) {
	var student domain.Student
	err := r.accessQuery(ctx, id, tenantID, parentID).First(&student).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrStudentNotFound
	}
	return &student, err
}

func (r *studentRepository) List(ctx context.Context, tenantID, parentID *uuid.UUID, query domain.StudentQuery) ([]domain.Student, int64, error) {
	db := r.db.WithContext(ctx).Model(&domain.Student{})
	if parentID != nil {
		db = db.Where("students.parent_id = ?", *parentID)
	}
	if tenantID != nil {
		db = db.Joins("JOIN enrollments e ON e.student_id = students.id").Where("e.tenant_id = ?", *tenantID).Distinct("students.id")
	}
	if query.Search != "" {
		search := "%" + query.Search + "%"
		db = db.Where("COALESCE(students.first_name, '') ILIKE ? OR COALESCE(students.last_name, '') ILIKE ? OR COALESCE(students.nickname, '') ILIKE ?", search, search, search)
	}
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var students []domain.Student
	err := db.Order("students.created_at DESC").Limit(query.PageSize).Offset((query.Page - 1) * query.PageSize).Find(&students).Error
	return students, total, err
}

func (r *studentRepository) CountActiveEnrollments(ctx context.Context, studentID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Table("enrollments").Where("student_id = ? AND status = ?", studentID, "active").Count(&count).Error
	return count, err
}

func (r *studentRepository) Update(ctx context.Context, student *domain.Student) error {
	return GetDB(ctx, r.db).Save(student).Error
}

func (r *studentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return GetDB(ctx, r.db).Delete(&domain.Student{}, "id = ?", id).Error
}
