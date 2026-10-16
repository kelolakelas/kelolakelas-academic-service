package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

var (
	ErrScheduleHistoryConflict = errors.New("effective date would change completed sessions or attendance history")
	ErrScheduleSeatsConflict   = errors.New("schedule capacity or deletion conflicts with reserved enrollments")
)

// ScheduleChangeGuard is implemented by the production schedule repository.
// The caller must hold the schedule FOR UPDATE, also used by seat reservations.
type ScheduleChangeGuard interface {
	ValidateChange(context.Context, uuid.UUID, uuid.UUID, time.Time, int, bool) error
}

func (r *scheduleRepository) ValidateChange(ctx context.Context, tenant, id uuid.UUID, effective time.Time, capacity int, deleting bool) error {
	db := r.getDB(ctx)
	loc, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	date := time.Date(effective.Year(), effective.Month(), effective.Day(), 0, 0, 0, 0, time.UTC)
	if date.Before(today) {
		return ErrScheduleHistoryConflict
	}
	// Serialize against session edits; attendance FK insertion also takes a lock
	// on its session row. Historic attendance rows themselves are never rewritten.
	var sessions []struct{ ID uuid.UUID }
	if err := db.Raw("SELECT id FROM class_sessions WHERE schedule_id = ? AND session_date >= ? AND deleted_at IS NULL FOR UPDATE", id, date).Scan(&sessions).Error; err != nil {
		return err
	}
	var history int64
	if err := db.Raw(`SELECT count(*) FROM class_sessions s WHERE s.schedule_id = ? AND s.session_date >= ? AND s.deleted_at IS NULL AND (s.status = 'completed' OR EXISTS (SELECT 1 FROM attendances a WHERE a.session_id = s.id))`, id, date).Scan(&history).Error; err != nil {
		return err
	}
	if history > 0 {
		return ErrScheduleHistoryConflict
	}
	var occupied int64
	if err := db.Model(&domain.Enrollment{}).Where("tenant_id = ? AND schedule_id = ? AND status IN ?", tenant, id, []string{"pending", "active"}).Count(&occupied).Error; err != nil {
		return err
	}
	if occupied > int64(capacity) || (deleting && occupied > 0) {
		return ErrScheduleSeatsConflict
	}
	return nil
}
