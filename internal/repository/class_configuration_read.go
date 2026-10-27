package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/gorm"
)

// ClassConfigurationReader exposes only tenant-owned saved editor data.
type ClassConfigurationReader interface {
	Class(context.Context, uuid.UUID, uuid.UUID) (*domain.Class, error)
	Schedules(context.Context, uuid.UUID, uuid.UUID, domain.ListQuery) ([]domain.ClassSchedule, int64, error)
}

type classConfigurationReader struct{ db *gorm.DB }

func NewClassConfigurationReader(db *gorm.DB) ClassConfigurationReader {
	return &classConfigurationReader{db: db}
}

func (r *classConfigurationReader) Class(ctx context.Context, tenant, id uuid.UUID) (*domain.Class, error) {
	var class domain.Class
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenant).
		Preload("TeacherAssignments").Preload("Category", "tenant_id = ?", tenant).First(&class).Error
	return &class, err
}

func (r *classConfigurationReader) Schedules(ctx context.Context, tenant, id uuid.UUID, query domain.ListQuery) ([]domain.ClassSchedule, int64, error) {
	// Resolve ownership even for an empty page; missing/foreign/deleted classes
	// must not become an indistinguishable successful empty configuration.
	var class domain.Class
	if err := r.db.WithContext(ctx).Select("id").Where("id = ? AND tenant_id = ?", id, tenant).First(&class).Error; err != nil {
		return nil, 0, err
	}
	q := r.db.WithContext(ctx).Model(&domain.ClassSchedule{}).
		Joins("JOIN classes c ON c.id = class_schedules.class_id AND c.deleted_at IS NULL").
		Where("class_schedules.class_id = ? AND c.tenant_id = ?", id, tenant)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []domain.ClassSchedule{}
	err := q.Select("class_schedules.*").
		Order("class_schedules.valid_from DESC NULLS LAST, class_schedules.day_of_week ASC, class_schedules.start_time ASC, class_schedules.id ASC").
		Limit(query.PageSize).Offset((query.Page - 1) * query.PageSize).Find(&items).Error
	// No enrollment/parent/payment preload is needed to edit a schedule.
	return items, total, err
}
