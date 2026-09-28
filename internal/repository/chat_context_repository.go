package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type ChatContextRepository interface {
	ScheduleRequest(ctx context.Context, id uuid.UUID) (*domain.ScheduleRequestChatContext, error)
	Report(ctx context.Context, id uuid.UUID) (*domain.ReportChatContext, error)
}

type chatContextRepository struct{ db *gorm.DB }

func NewChatContextRepository(db *gorm.DB) ChatContextRepository {
	return &chatContextRepository{db: db}
}

func (r *chatContextRepository) ScheduleRequest(ctx context.Context, id uuid.UUID) (*domain.ScheduleRequestChatContext, error) {
	var item domain.ScheduleRequestChatContext
	err := r.db.WithContext(ctx).Table("private_schedule_requests AS req").
		Select("req.id, req.tenant_id, req.parent_id, req.class_id, classes.name AS class_name, req.student_id, students.first_name AS student_first_name, req.status").
		Joins("JOIN classes ON classes.id = req.class_id AND classes.tenant_id = req.tenant_id AND classes.deleted_at IS NULL").
		Joins("JOIN students ON students.id = req.student_id AND students.parent_id = req.parent_id AND students.deleted_at IS NULL").
		Where("req.id = ?", id).Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrChatContextNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *chatContextRepository) Report(ctx context.Context, id uuid.UUID) (*domain.ReportChatContext, error) {
	var item domain.ReportChatContext
	err := r.db.WithContext(ctx).Table("reports AS reports").
		Select("reports.id, reports.tenant_id, reports.enrollment_id, enrollments.student_id, students.first_name AS student_first_name, students.parent_id, classes.name AS class_name, reports.title, reports.reporter_id").
		Joins("JOIN enrollments ON enrollments.id = reports.enrollment_id AND enrollments.tenant_id = reports.tenant_id AND enrollments.deleted_at IS NULL").
		Joins("JOIN students ON students.id = enrollments.student_id AND students.deleted_at IS NULL").
		Joins("JOIN classes ON classes.id = enrollments.class_id AND classes.tenant_id = reports.tenant_id AND classes.deleted_at IS NULL").
		Where("reports.id = ? AND reports.deleted_at IS NULL", id).Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrChatContextNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}
