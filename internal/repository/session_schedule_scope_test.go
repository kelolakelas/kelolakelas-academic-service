package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The scoped reads and every bulk write put the tenant filter inside the SQL
// itself, so a caller that passes another tenant's id must produce a statement
// that matches zero rows instead of touching data. These tests assert on the
// generated SQL because the use case tests use in-memory fakes and therefore
// cannot prove the predicate is really present in the query.

func newScopedRepositoryDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return db, mock
}

// A row owned by another tenant is invisible: the query joins classes and filters
// on the tenant, and GORM turns the empty result into ErrRecordNotFound which the
// use case maps to a 404.
func TestGetByIDForTenantFiltersOnTheOwningClassTenant(t *testing.T) {
	db, mock := newScopedRepositoryDB(t)
	tenantID, sessionID, scheduleID := uuid.New(), uuid.New(), uuid.New()

	mock.ExpectQuery(`SELECT .*FROM "class_sessions" JOIN classes c ON c\.id = class_sessions\.class_id WHERE \(class_sessions\.id = \$1 AND c\.tenant_id = \$2\)`).
		WithArgs(sessionID, tenantID, 1).
		WillReturnError(gorm.ErrRecordNotFound)
	if _, err := (&sessionRepository{db: db}).GetByIDForTenant(context.Background(), tenantID, sessionID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("session GetByIDForTenant err=%v want gorm.ErrRecordNotFound", err)
	}

	mock.ExpectQuery(`SELECT .*FROM "class_schedules" JOIN classes c ON c\.id = class_schedules\.class_id WHERE \(class_schedules\.id = \$1 AND c\.tenant_id = \$2\)`).
		WithArgs(scheduleID, tenantID, 1).
		WillReturnError(gorm.ErrRecordNotFound)
	if _, err := (&scheduleRepository{db: db}).GetByIDForTenant(context.Background(), tenantID, scheduleID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("schedule GetByIDForTenant err=%v want gorm.ErrRecordNotFound", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// The scoped read returns the row when the class really belongs to the caller.
func TestGetByIDForTenantReturnsTheOwnersSession(t *testing.T) {
	db, mock := newScopedRepositoryDB(t)
	tenantID, sessionID, classID := uuid.New(), uuid.New(), uuid.New()
	date := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT .*FROM "class_sessions" JOIN classes c`).
		WithArgs(sessionID, tenantID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "class_id", "schedule_id", "enrollment_id", "tutor_id", "session_date", "start_time", "end_time", "status", "deleted_at"}).
			AddRow(sessionID, classID, nil, nil, uuid.New(), date, "16:00:00", "17:00:00", "scheduled", nil))
	// GetByIDForTenant preloads the owning class as well.
	mock.ExpectQuery(`SELECT .*FROM "classes" WHERE "classes"\."id" = \$1`).
		WithArgs(classID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "deleted_at"}).
			AddRow(classID, tenantID, "Kelas Matematika", nil))

	session, err := (&sessionRepository{db: db}).GetByIDForTenant(context.Background(), tenantID, sessionID)
	if err != nil {
		t.Fatalf("GetByIDForTenant error: %v", err)
	}
	if session.ID != sessionID || session.ClassID != classID {
		t.Fatalf("session=%+v want id=%s class=%s", session, sessionID, classID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// Every bulk write repeats the tenant filter. A caller passing another tenant's
// schedule or class id runs a statement whose WHERE clause cannot match that
// tenant's rows, which is what keeps a request-supplied id from mutating data.
func TestBulkSessionWritesCarryTheTenantPredicate(t *testing.T) {
	db, mock := newScopedRepositoryDB(t)
	tenantID, scheduleID, classID := uuid.New(), uuid.New(), uuid.New()
	newTutorID, newScheduleID := uuid.New(), uuid.New()
	date := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	repo := &sessionRepository{db: db}

	tenantPredicate := `class_id IN \(SELECT id FROM classes WHERE tenant_id = \$`
	cases := []struct {
		name  string
		query string
		args  []driver.Value
		call  func() error
	}{
		{
			name:  "cancel by schedule",
			query: `UPDATE "class_sessions" SET "status"=\$1 WHERE \(schedule_id = \$2 AND session_date >= \$3 AND status = \$4\) AND ` + tenantPredicate + `5\)`,
			args:  []driver.Value{"cancelled", scheduleID, date, "scheduled", tenantID},
			call: func() error {
				return repo.CancelFutureSessionsBySchedule(context.Background(), tenantID, scheduleID, date)
			},
		},
		{
			name:  "cancel by class",
			query: `UPDATE "class_sessions" SET "status"=\$1 WHERE \(class_id = \$2 AND session_date >= \$3 AND status = \$4\) AND ` + tenantPredicate + `5\)`,
			args:  []driver.Value{"cancelled", classID, date, "scheduled", tenantID},
			call:  func() error { return repo.CancelFutureSessionsByClass(context.Background(), tenantID, classID, date) },
		},
		{
			name:  "update future tutors",
			query: `UPDATE "class_sessions" SET "schedule_id"=\$1,"tutor_id"=\$2 WHERE \(schedule_id = \$3 AND session_date >= \$4 AND status = \$5\) AND ` + tenantPredicate + `6\)`,
			args:  []driver.Value{newScheduleID, newTutorID, scheduleID, date, "scheduled", tenantID},
			call: func() error {
				return repo.UpdateFutureSessionsTutor(context.Background(), tenantID, scheduleID, newTutorID, newScheduleID, date)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock.ExpectBegin()
			mock.ExpectExec(tc.query).WithArgs(tc.args...).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectCommit()
			if err := tc.call(); err != nil {
				t.Fatalf("write error: %v", err)
			}
		})
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// GetActiveByScheduleID reads enrollments, which hold student references, so the
// tenant is part of the predicate rather than a post-filter.
func TestGetActiveByScheduleIDFiltersOnTenant(t *testing.T) {
	db, mock := newScopedRepositoryDB(t)
	tenantID, scheduleID, studentID := uuid.New(), uuid.New(), uuid.New()

	mock.ExpectQuery(`SELECT .*FROM "enrollments" WHERE \(schedule_id = \$1 AND tenant_id = \$2 AND status IN \(\$3,\$4\) AND deleted_at IS NULL\)`).
		WithArgs(scheduleID, tenantID, "pending", "active").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "student_id", "class_id", "schedule_id", "status", "billing_cycle"}).
			AddRow(uuid.New(), tenantID, studentID, uuid.New(), scheduleID, "active", "monthly"))
	// GetActiveByScheduleID preloads the student, so the student lookup is part
	// of the observable behaviour of the scoped read.
	mock.ExpectQuery(`SELECT .*FROM "students" WHERE "students"\."id" = \$1`).
		WithArgs(studentID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "deleted_at"}).
			AddRow(studentID, tenantID, "Siswa", nil))

	enrollments, err := (&enrollmentRepository{db: db}).GetActiveByScheduleID(context.Background(), tenantID, scheduleID)
	if err != nil {
		t.Fatalf("GetActiveByScheduleID error: %v", err)
	}
	if len(enrollments) != 1 || enrollments[0].TenantID != tenantID {
		t.Fatalf("enrollments=%+v want exactly one row for tenant %s", enrollments, tenantID)
	}
	if enrollments[0].Student == nil || enrollments[0].Student.ID != studentID {
		t.Fatalf("student=%+v want the preloaded student %s", enrollments[0].Student, studentID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// A foreign schedule yields no rows and therefore no student data.
func TestGetActiveByScheduleIDReturnsNothingForAnotherTenant(t *testing.T) {
	db, mock := newScopedRepositoryDB(t)
	callerTenant, scheduleID := uuid.New(), uuid.New()

	mock.ExpectQuery(`SELECT .*FROM "enrollments" WHERE \(schedule_id = \$1 AND tenant_id = \$2`).
		WithArgs(scheduleID, callerTenant, "pending", "active").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "student_id", "class_id", "schedule_id", "status", "billing_cycle"}))

	enrollments, err := (&enrollmentRepository{db: db}).GetActiveByScheduleID(context.Background(), callerTenant, scheduleID)
	if err != nil {
		t.Fatalf("GetActiveByScheduleID error: %v", err)
	}
	if len(enrollments) != 0 {
		t.Fatalf("enrollments=%+v want none for a foreign tenant", enrollments)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
