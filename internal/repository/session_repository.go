package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type sessionRepository struct {
	db *gorm.DB
}

func NewSessionRepository(db *gorm.DB) SessionRepository {
	return &sessionRepository{db: db}
}

func (r *sessionRepository) getDB(ctx context.Context) *gorm.DB {
	return GetDB(ctx, r.db)
}

func (r *sessionRepository) Create(ctx context.Context, session *domain.ClassSession) error {
	return r.getDB(ctx).Create(session).Error
}

func (r *sessionRepository) BatchCreate(ctx context.Context, sessions []*domain.ClassSession) error {
	if len(sessions) == 0 {
		return nil
	}
	return r.getDB(ctx).Create(sessions).Error
}

func (r *sessionRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.ClassSession, error) {
	var session domain.ClassSession
	if err := r.getDB(ctx).Preload("Class").Preload("Schedule").Preload("Enrollment").First(&session, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *sessionRepository) Update(ctx context.Context, session *domain.ClassSession) error {
	return r.getDB(ctx).Save(session).Error
}

func (r *sessionRepository) DeleteFutureSessionsBySchedule(ctx context.Context, scheduleID uuid.UUID, fromDate time.Time) error {
	return r.getDB(ctx).
		Where("schedule_id = ? AND session_date >= ? AND status = ?", scheduleID, fromDate, "scheduled").
		Delete(&domain.ClassSession{}).Error
}

func (r *sessionRepository) UpdateFutureSessionsTutor(ctx context.Context, scheduleID uuid.UUID, newTutorID uuid.UUID, newScheduleID uuid.UUID, fromDate time.Time) error {
	return r.getDB(ctx).
		Model(&domain.ClassSession{}).
		Where("schedule_id = ? AND session_date >= ? AND status = ?", scheduleID, fromDate, "scheduled").
		Updates(map[string]interface{}{
			"tutor_id":    newTutorID,
			"schedule_id": newScheduleID,
		}).Error
}
