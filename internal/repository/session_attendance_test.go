package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestFindForAttendanceFindsGroupSessionByEnrollmentSchedule(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}

	sessionID, classID, scheduleID, enrollmentID, tenantID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	date := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT .*FROM "class_sessions".*EXISTS \(SELECT 1 FROM enrollments`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "class_id", "schedule_id", "enrollment_id", "tutor_id", "session_date", "start_time", "end_time", "status", "deleted_at"}).
			AddRow(sessionID, classID, scheduleID, nil, uuid.New(), date, "16:00:00", "17:00:00", "scheduled", nil))

	repo := &sessionRepository{db: db}
	session, err := repo.FindForAttendance(context.Background(), tenantID, scheduleID, enrollmentID, date)
	if err != nil {
		t.Fatalf("FindForAttendance error: %v", err)
	}
	if session.ID != sessionID || session.ScheduleID == nil || *session.ScheduleID != scheduleID {
		t.Fatalf("session=%+v, want group session %s on schedule %s", session, sessionID, scheduleID)
	}
	if session.EnrollmentID != nil {
		t.Fatalf("group session enrollment_id=%v, want nil", session.EnrollmentID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
