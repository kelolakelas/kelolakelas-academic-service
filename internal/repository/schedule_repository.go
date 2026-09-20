package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type scheduleRepository struct {
	db *gorm.DB
}

func (r *scheduleRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID, query domain.ListQuery) ([]domain.ClassSchedule, int64, error) {
	db := r.getDB(ctx).Table("class_schedules cs").Joins("JOIN classes c ON c.id = cs.class_id").Where("c.tenant_id = ? AND c.deleted_at IS NULL AND cs.deleted_at IS NULL", tenantID)
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []domain.ClassSchedule
	if err := db.Select("cs.*").Preload("Class").Preload("Enrollment").Order("cs.valid_from DESC NULLS LAST, cs.day_of_week ASC, cs.start_time ASC").Limit(query.PageSize).Offset((query.Page - 1) * query.PageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func NewScheduleRepository(db *gorm.DB) ScheduleRepository {
	return &scheduleRepository{db: db}
}

func (r *scheduleRepository) getDB(ctx context.Context) *gorm.DB {
	return GetDB(ctx, r.db)
}

func (r *scheduleRepository) Create(ctx context.Context, schedule *domain.ClassSchedule) error {
	return r.getDB(ctx).Create(schedule).Error
}

func (r *scheduleRepository) BatchCreate(ctx context.Context, schedules []*domain.ClassSchedule) error {
	if len(schedules) == 0 {
		return nil
	}
	return r.getDB(ctx).Create(schedules).Error
}

func (r *scheduleRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.ClassSchedule, error) {
	var schedule domain.ClassSchedule
	if err := r.getDB(ctx).Preload("Class").Preload("Enrollment").First(&schedule, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &schedule, nil
}

// GetByIDForTenant resolves a schedule only when its parent class belongs to the
// given tenant. The ownership filter lives in the query (a join on classes), so a
// schedule owned by another tenant is indistinguishable from a missing one and
// callers cannot probe for its existence.
func (r *scheduleRepository) GetByIDForTenant(ctx context.Context, tenantID, id uuid.UUID) (*domain.ClassSchedule, error) {
	var schedule domain.ClassSchedule
	err := r.getDB(ctx).Joins("JOIN classes c ON c.id = class_schedules.class_id").
		Where("class_schedules.id = ? AND c.tenant_id = ?", id, tenantID).
		Preload("Class").Preload("Enrollment").First(&schedule).Error
	if err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (r *scheduleRepository) Update(ctx context.Context, schedule *domain.ClassSchedule) error {
	return r.getDB(ctx).Save(schedule).Error
}

func (r *scheduleRepository) DeleteByTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	return r.getDB(ctx).Where("id = ? AND class_id IN (SELECT id FROM classes WHERE tenant_id = ?)", id, tenantID).Delete(&domain.ClassSchedule{}).Error
}

func (r *scheduleRepository) DeleteByClass(ctx context.Context, tenantID, classID uuid.UUID) error {
	return r.getDB(ctx).Where("class_id = ? AND class_id IN (SELECT id FROM classes WHERE tenant_id = ?)", classID, tenantID).Delete(&domain.ClassSchedule{}).Error
}
