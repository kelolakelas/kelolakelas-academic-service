package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tutorin-id/tutorin-academic-service/internal/domain"
)

type scheduleRepository struct {
	db *gorm.DB
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

func (r *scheduleRepository) Update(ctx context.Context, schedule *domain.ClassSchedule) error {
	return r.getDB(ctx).Save(schedule).Error
}

func (r *scheduleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.getDB(ctx).Delete(&domain.ClassSchedule{}, "id = ?", id).Error
}
