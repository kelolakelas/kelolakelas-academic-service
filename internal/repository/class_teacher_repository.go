package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type classTeacherRepository struct {
	db *gorm.DB
}

func NewClassTeacherRepository(db *gorm.DB) ClassTeacherRepository {
	return &classTeacherRepository{db: db}
}

func (r *classTeacherRepository) Assign(ctx context.Context, classTeacher *domain.ClassTeacher) error {
	return GetDB(ctx, r.db).Create(classTeacher).Error
}

func (r *classTeacherRepository) Unassign(ctx context.Context, classID, teacherID uuid.UUID) error {
	return GetDB(ctx, r.db).
		Where("class_id = ? AND teacher_id = ?", classID, teacherID).
		Delete(&domain.ClassTeacher{}).Error
}
