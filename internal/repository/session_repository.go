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

func (r *sessionRepository) GetByIDForTenant(ctx context.Context, tenantID, id uuid.UUID) (*domain.ClassSession, error) {
	var session domain.ClassSession
	err := r.getDB(ctx).Joins("JOIN classes c ON c.id = class_sessions.class_id").
		Where("class_sessions.id = ? AND c.tenant_id = ?", id, tenantID).
		Preload("Class").Preload("Schedule").Preload("Enrollment").First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *sessionRepository) DeleteByTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	return r.getDB(ctx).Where("id = ? AND class_id IN (SELECT id FROM classes WHERE tenant_id = ?)", id, tenantID).Delete(&domain.ClassSession{}).Error
}

// FindForAttendance resolves the session an attendance record belongs to. The
// enrollment must not be suspended (KEL-149): a suspended enrollment frees its
// seat, so its student is not attending while suspended and an attendance write
// for it is refused by finding no session, the same way a mismatched
// schedule/enrollment pair already is. Every other status keeps today's
// behaviour, including late attendance for an enrollment that has since
// completed or been dropped.
func (r *sessionRepository) FindForAttendance(ctx context.Context, tenantID, scheduleID, enrollmentID uuid.UUID, date time.Time) (*domain.ClassSession, error) {
	var session domain.ClassSession
	err := r.getDB(ctx).Joins("JOIN classes c ON c.id = class_sessions.class_id").
		Where("c.tenant_id = ? AND class_sessions.schedule_id = ? AND class_sessions.session_date = ? AND ((class_sessions.enrollment_id = ? AND class_sessions.enrollment_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM enrollments pe WHERE pe.id = class_sessions.enrollment_id AND pe.status = ?)) OR (class_sessions.enrollment_id IS NULL AND EXISTS (SELECT 1 FROM enrollments e WHERE e.id = ? AND e.schedule_id = ? AND e.status <> ? AND e.deleted_at IS NULL)))", tenantID, scheduleID, date, enrollmentID, domain.EnrollmentStatusSuspended, enrollmentID, scheduleID, domain.EnrollmentStatusSuspended).
		First(&session).Error
	return &session, err
}

func (r *sessionRepository) IsTutorForSession(ctx context.Context, tenantID, sessionID, memberID uuid.UUID) (bool, error) {
	var count int64
	err := r.getDB(ctx).Table("class_sessions cs").Joins("JOIN classes c ON c.id = cs.class_id").Where("cs.id = ? AND cs.tutor_id = ? AND c.tenant_id = ?", sessionID, memberID, tenantID).Count(&count).Error
	return count > 0, err
}

// FindSessionForAttendance resolves a session directly by id for attendance
// writes addressed at session_id (KEL-134). Unlike FindForAttendance it does
// not need schedule_id + date, so it also resolves reschedule replacements
// (schedule_id IS NULL, status 'scheduled', linked via
// rescheduled_from_session_id). The ownership filter is part of the query, so
// a session owned by another tenant reports gorm.ErrRecordNotFound. Cancelled
// sessions resolve too: the use case rejects writes to them with a validation
// error rather than "not found", so callers can tell "no such session" apart
// from "session cannot take attendance".
func (r *sessionRepository) FindSessionForAttendance(ctx context.Context, tenantID, sessionID uuid.UUID) (*domain.ClassSession, error) {
	var session domain.ClassSession
	err := r.getDB(ctx).Joins("JOIN classes c ON c.id = class_sessions.class_id").
		Where("class_sessions.id = ? AND c.tenant_id = ?", sessionID, tenantID).
		Preload("Class").Preload("Schedule").Preload("Enrollment").First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// ListSessionsForAttendanceCohort lists sessions of one class with the given
// status for the reschedule fallback (KEL-134): origin sessions of group
// reschedule replacements created before the migration carry no
// rescheduled_from_session_id link, so the cohort schedule is recovered by
// matching a 'rescheduled' session of the same class.
func (r *sessionRepository) ListSessionsForAttendanceCohort(ctx context.Context, tenantID, classID uuid.UUID, status string) ([]domain.ClassSession, error) {
	var sessions []domain.ClassSession
	err := r.getDB(ctx).Joins("JOIN classes c ON c.id = class_sessions.class_id").
		Where("c.tenant_id = ? AND class_sessions.class_id = ? AND class_sessions.status = ?", tenantID, classID, status).
		Find(&sessions).Error
	return sessions, err
}

func (r *sessionRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID, query domain.SessionQuery) ([]domain.ClassSession, int64, error) {
	db := r.getDB(ctx).Table("class_sessions cs").Joins("JOIN classes c ON c.id = cs.class_id").Where("c.tenant_id = ?", tenantID)
	if query.ClassID != nil {
		db = db.Where("cs.class_id = ?", *query.ClassID)
	}
	if query.ScheduleID != nil {
		db = db.Where("cs.schedule_id = ?", *query.ScheduleID)
	}
	if query.EnrollmentID != nil {
		db = db.Where("cs.enrollment_id = ?", *query.EnrollmentID)
	}
	if query.TutorID != nil {
		db = db.Where("cs.tutor_id = ?", *query.TutorID)
	}
	if query.Status != "" {
		db = db.Where("cs.status = ?", query.Status)
	}
	if query.DateFrom != nil {
		db = db.Where("cs.session_date >= ?", *query.DateFrom)
	}
	if query.DateTo != nil {
		db = db.Where("cs.session_date <= ?", *query.DateTo)
	}
	if query.Search != "" {
		db = db.Where("c.name ILIKE ? ESCAPE '\\'", "%"+escapeLikePattern(query.Search)+"%")
	}
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var sessions []domain.ClassSession
	err := db.Select("cs.*").Preload("Class").Preload("Schedule").Preload("Enrollment").
		Order("cs.session_date DESC, cs.start_time ASC").Limit(query.PageSize).Offset((query.Page - 1) * query.PageSize).Find(&sessions).Error
	return sessions, total, err
}

func (r *sessionRepository) Update(ctx context.Context, session *domain.ClassSession) error {
	return r.getDB(ctx).Save(session).Error
}

// CancelFutureSessionsBySchedule, CancelFutureSessionsByClass and
// UpdateFutureSessionsTutor are bulk writes, so the tenant filter is repeated in
// the SQL rather than trusted from an earlier read: a caller that passes another
// tenant's id must touch zero rows.
func (r *sessionRepository) CancelFutureSessionsBySchedule(ctx context.Context, tenantID, scheduleID uuid.UUID, fromDate time.Time) error {
	return r.getDB(ctx).
		Model(&domain.ClassSession{}).
		Where("schedule_id = ? AND session_date >= ? AND status = ?", scheduleID, fromDate, "scheduled").
		Where("class_id IN (SELECT id FROM classes WHERE tenant_id = ?)", tenantID).
		Update("status", "cancelled").Error
}

func (r *sessionRepository) CancelFutureSessionsByClass(ctx context.Context, tenantID, classID uuid.UUID, fromDate time.Time) error {
	return r.getDB(ctx).
		Model(&domain.ClassSession{}).
		Where("class_id = ? AND session_date >= ? AND status = ?", classID, fromDate, "scheduled").
		Where("class_id IN (SELECT id FROM classes WHERE tenant_id = ?)", tenantID).
		Update("status", "cancelled").Error
}

func (r *sessionRepository) UpdateFutureSessionsTutor(ctx context.Context, tenantID, scheduleID uuid.UUID, newTutorID uuid.UUID, newScheduleID uuid.UUID, fromDate time.Time) error {
	return r.getDB(ctx).
		Model(&domain.ClassSession{}).
		Where("schedule_id = ? AND session_date >= ? AND status = ?", scheduleID, fromDate, "scheduled").
		Where("class_id IN (SELECT id FROM classes WHERE tenant_id = ?)", tenantID).
		Updates(map[string]interface{}{
			"tutor_id":    newTutorID,
			"schedule_id": newScheduleID,
		}).Error
}
