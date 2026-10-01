package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type attendanceRepository struct {
	db *gorm.DB
}

func NewAttendanceRepository(db *gorm.DB) AttendanceRepository {
	return &attendanceRepository{db: db}
}

func (r *attendanceRepository) Create(ctx context.Context, attendance *domain.Attendance) error {
	return r.db.WithContext(ctx).Create(attendance).Error
}

func (r *attendanceRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Attendance, error) {
	var item domain.Attendance
	err := r.db.WithContext(ctx).Preload("Session").First(&item, "id = ?", id).Error
	return &item, err
}

func (r *attendanceRepository) GetByIDForTenant(ctx context.Context, tenantID, id uuid.UUID) (*domain.Attendance, error) {
	var item domain.Attendance
	err := r.db.WithContext(ctx).
		Table("attendances a").
		Joins("JOIN class_sessions cs ON cs.id = a.session_id").
		Joins("JOIN classes c ON c.id = cs.class_id").
		Where("a.id = ? AND c.tenant_id = ?", id, tenantID).
		Select("a.*").
		Preload("Session").
		First(&item).Error
	return &item, err
}

func (r *attendanceRepository) GetByUnique(ctx context.Context, enrollmentID, sessionID uuid.UUID, date time.Time) (*domain.Attendance, error) {
	var item domain.Attendance
	err := r.db.WithContext(ctx).
		Where("enrollment_id = ? AND session_id = ? AND date = ?", enrollmentID, sessionID, date).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

// GetBySessionEnrollment resolves one attendance row by (session, enrollment)
// for session-addressed reads (KEL-134).
func (r *attendanceRepository) GetBySessionEnrollment(ctx context.Context, sessionID, enrollmentID uuid.UUID) (*domain.Attendance, error) {
	var item domain.Attendance
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND enrollment_id = ?", sessionID, enrollmentID).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

// UpsertBulk writes every row of one session atomically and idempotently
// (KEL-134): one statement with ON CONFLICT (session_id, enrollment_id)
// DO UPDATE, so repeating the same request updates rows instead of creating
// duplicates, and two concurrent identical requests serialize on the unique
// index with at most one winner per row. The tenant scope is part of the
// statement: a caller passing another tenant's session id matches zero rows
// in the session guard and writes nothing. Rows keep the session's own date
// so legacy date-scoped reads keep working.
func (r *attendanceRepository) UpsertBulk(ctx context.Context, tenantID, sessionID uuid.UUID, items []domain.BulkAttendanceItem) ([]domain.Attendance, error) {
	if len(items) == 0 {
		return nil, errors.New("no attendance items")
	}
	var out []domain.Attendance
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Session guard first, tenant-scoped in SQL: a session owned by
		// another tenant reports gorm.ErrRecordNotFound and nothing is
		// written. The guard also supplies the session date for the rows.
		var sessionDate time.Time
		if err := tx.Table("class_sessions cs").
			Joins("JOIN classes c ON c.id = cs.class_id").
			Where("cs.id = ? AND c.tenant_id = ?", sessionID, tenantID).
			Select("cs.session_date").
			Scan(&sessionDate).Error; err != nil {
			return err
		}
		if sessionDate.IsZero() {
			return gorm.ErrRecordNotFound
		}
		valueStrings := make([]string, 0, len(items))
		args := make([]any, 0, len(items)*3+2)
		for _, item := range items {
			valueStrings = append(valueStrings, "(?::uuid,?::uuid,?)")
			args = append(args, uuid.New(), item.EnrollmentID, item.Status)
		}
		// The tenant predicate stays in the write statement rather than
		// trusting the earlier guard read: a caller that passes another
		// tenant's session id must touch zero rows even if the guard raced.
		// GORM rebinds ? to $N for postgres, so the VALUES placeholders and
		// these two share one positional sequence.
		args = append(args, sessionID, tenantID)
		stmt := `INSERT INTO attendances (id, enrollment_id, session_id, date, status, created_at, updated_at)
			SELECT v.id, v.enrollment_id, s.id, s.session_date, v.status, now(), now()
			FROM (VALUES ` + strings.Join(valueStrings, ",") + `) AS v(id, enrollment_id, status)
			JOIN (SELECT cs.id, cs.session_date FROM class_sessions cs JOIN classes c ON c.id = cs.class_id WHERE cs.id = ?::uuid AND c.tenant_id = ?::uuid) AS s ON true
			ON CONFLICT (session_id, enrollment_id) DO UPDATE SET status = EXCLUDED.status, date = EXCLUDED.date, updated_at = now()`
		if err := tx.Exec(stmt, args...).Error; err != nil {
			return err
		}
		enrollmentIDs := make([]uuid.UUID, 0, len(items))
		for _, item := range items {
			enrollmentIDs = append(enrollmentIDs, item.EnrollmentID)
		}
		if err := tx.Where("session_id = ? AND enrollment_id IN ?", sessionID, enrollmentIDs).Order("enrollment_id").Find(&out).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *attendanceRepository) List(ctx context.Context, tenantID uuid.UUID, query domain.AttendanceQuery) ([]domain.Attendance, int64, error) {
	db := r.db.WithContext(ctx).
		Table("attendances a").
		Joins("JOIN class_sessions cs ON cs.id = a.session_id").
		Joins("JOIN classes c ON c.id = cs.class_id").
		Where("c.tenant_id = ?", tenantID)

	if query.EnrollmentID != nil {
		db = db.Where("a.enrollment_id = ?", *query.EnrollmentID)
	}
	if query.StudentID != nil {
		db = db.Joins("JOIN enrollments e ON e.id = a.enrollment_id").
			Where("e.student_id = ?", *query.StudentID)
	}
	if query.ScheduleID != nil {
		db = db.Joins("JOIN class_schedules sch ON sch.id = cs.schedule_id").
			Where("sch.id = ?", *query.ScheduleID)
	}
	if query.Status != "" {
		db = db.Where("a.status = ?", query.Status)
	}
	if query.DateFrom != nil {
		db = db.Where("a.date >= ?", *query.DateFrom)
	}
	if query.DateTo != nil {
		db = db.Where("a.date <= ?", *query.DateTo)
	}

	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []domain.Attendance
	err := db.Select("a.*").
		Preload("Session").
		Order("a.date DESC").
		Limit(query.PageSize).
		Offset((query.Page - 1) * query.PageSize).
		Find(&items).Error
	return items, total, err
}

func (r *attendanceRepository) Update(ctx context.Context, attendance *domain.Attendance) error {
	return r.db.WithContext(ctx).Save(attendance).Error
}
