package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type sessionGenerationRepository struct {
	db *gorm.DB
}

func NewSessionGenerationRepository(db *gorm.DB) SessionGenerationRepository {
	return &sessionGenerationRepository{db: db}
}

func (r *sessionGenerationRepository) getDB(ctx context.Context) *gorm.DB {
	return GetDB(ctx, r.db)
}

// LockNextScheduleDueForGeneration returns the first live schedule, by id after
// afterID, whose sessions are not yet generated through horizonEnd and whose
// validity has not ended inside the range already covered. It returns nil when none
// is due. It must run inside a transaction: the row is locked FOR UPDATE so
// generation serializes with permanent changes, deletion and capacity-checked
// enrollment on the same schedule. SKIP LOCKED lets several instances share the work
// instead of waiting on each other. The afterID cursor keeps one pass moving past a
// schedule whose generation failed.
func (r *sessionGenerationRepository) LockNextScheduleDueForGeneration(ctx context.Context, horizonEnd time.Time, afterID uuid.UUID) (*domain.ClassSchedule, error) {
	var schedules []domain.ClassSchedule
	err := r.getDB(ctx).
		Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "class_schedules"}, Options: "SKIP LOCKED"}).
		Joins("JOIN classes c ON c.id = class_schedules.class_id AND c.deleted_at IS NULL").
		// A private schedule belongs to one enrollment; once that enrollment is no
		// longer live (dropped, completed or deleted) the schedule has ended for good.
		Joins("LEFT JOIN enrollments e ON e.id = class_schedules.enrollment_id").
		Where("class_schedules.enrollment_id IS NULL OR (e.status IN ? AND e.deleted_at IS NULL)", []string{"pending", "active"}).
		Where("class_schedules.id > ?", afterID).
		Where("class_schedules.sessions_generated_until IS NULL OR class_schedules.sessions_generated_until < ?", horizonEnd).
		Where("class_schedules.valid_until IS NULL OR class_schedules.sessions_generated_until IS NULL OR class_schedules.valid_until > class_schedules.sessions_generated_until").
		Order("class_schedules.id").
		Limit(1).
		Find(&schedules).Error
	if err != nil || len(schedules) == 0 {
		return nil, err
	}
	return &schedules[0], nil
}

// InsertMissingSessions inserts the sessions and silently skips every schedule and
// date that already has a row, soft-deleted and cancelled rows included, so a
// repeated or concurrent run never duplicates or revives a session. It returns the
// number of rows actually inserted.
func (r *sessionGenerationRepository) InsertMissingSessions(ctx context.Context, sessions []*domain.ClassSession) (int64, error) {
	if len(sessions) == 0 {
		return 0, nil
	}
	result := r.getDB(ctx).Clauses(clause.OnConflict{
		Columns:     []clause.Column{{Name: "schedule_id"}, {Name: "session_date"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "schedule_id IS NOT NULL"}}},
		DoNothing:   true,
	}).Create(&sessions)
	return result.RowsAffected, result.Error
}

// MarkSessionsGeneratedUntil records the generated range. The watermark never moves
// backwards, so a late run with an older horizon cannot reopen covered dates.
func (r *sessionGenerationRepository) MarkSessionsGeneratedUntil(ctx context.Context, scheduleID uuid.UUID, until time.Time) error {
	return r.getDB(ctx).Exec(
		"UPDATE class_schedules SET sessions_generated_until = GREATEST(COALESCE(sessions_generated_until, ?::date), ?::date) WHERE id = ?",
		until, until, scheduleID,
	).Error
}
