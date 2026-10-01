package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type attendanceSessionRepo struct {
	repository.SessionRepository
	session   *domain.ClassSession
	findError error
	assigned  bool
}

func (r *attendanceSessionRepo) FindForAttendance(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (*domain.ClassSession, error) {
	return r.session, r.findError
}
func (r *attendanceSessionRepo) FindSessionForAttendance(_ context.Context, _ uuid.UUID, id uuid.UUID) (*domain.ClassSession, error) {
	if r.session == nil || r.session.ID != id {
		return nil, r.findError
	}
	return r.session, r.findError
}
func (r *attendanceSessionRepo) ListSessionsForAttendanceCohort(context.Context, uuid.UUID, uuid.UUID, string) ([]domain.ClassSession, error) {
	return nil, nil
}
func (r *attendanceSessionRepo) IsTutorForSession(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	return r.assigned, nil
}

type attendanceRepo struct {
	repository.AttendanceRepository
	existing *domain.Attendance
	created  *domain.Attendance
}

func (r *attendanceRepo) GetByUnique(context.Context, uuid.UUID, uuid.UUID, time.Time) (*domain.Attendance, error) {
	return r.existing, nil
}
func (r *attendanceRepo) GetBySessionEnrollment(_ context.Context, _, _ uuid.UUID) (*domain.Attendance, error) {
	return r.existing, nil
}
func (r *attendanceRepo) UpsertBulk(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ []domain.BulkAttendanceItem) ([]domain.Attendance, error) {
	return nil, nil
}
func (r *attendanceRepo) Create(_ context.Context, attendance *domain.Attendance) error {
	r.created = attendance
	return nil
}

// The legacy schedule_id + date form keeps working (KEL-134 AC4): a matching
// schedule resolves and stores, a mismatched one is rejected, and a duplicate
// is refused.
func TestAttendanceCreateScheduleMatching(t *testing.T) {
	enrollmentID, sessionID, scheduleID := uuid.New(), uuid.New(), uuid.New()
	date := "2026-08-22"
	tests := []struct {
		name      string
		findError error
		existing  *domain.Attendance
		wantError error
		wantStore bool
	}{
		{name: "matching schedule", wantStore: true},
		{name: "different schedule rejected", findError: domain.ErrScheduleClassMismatch, wantError: domain.ErrScheduleClassMismatch},
		{name: "duplicate attendance rejected", existing: &domain.Attendance{ID: uuid.New()}, wantError: domain.ErrAttendanceDuplicate},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sessions := &attendanceSessionRepo{
				session:   &domain.ClassSession{ID: sessionID, ScheduleID: &scheduleID},
				findError: test.findError,
				assigned:  true,
			}
			attendances := &attendanceRepo{existing: test.existing}
			uc := NewAttendanceUsecase(attendances, sessions)
			result, err := uc.Create(context.Background(), uuid.New(), uuid.New(), &domain.CreateAttendanceRequest{
				EnrollmentID: enrollmentID, ScheduleID: &scheduleID, Date: date, Status: "present",
			})
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
			if (result != nil) != test.wantStore {
				t.Fatalf("stored result = %v, want stored=%v", result != nil, test.wantStore)
			}
		})
	}
}

type attendanceInputRepo struct {
	repository.AttendanceRepository
}

func (r *attendanceInputRepo) GetBySessionEnrollment(context.Context, uuid.UUID, uuid.UUID) (*domain.Attendance, error) {
	return nil, nil
}

func TestAttendanceCreateRejectsUnassignedTutor(t *testing.T) {
	scheduleID := uuid.New()
	sessions := &attendanceSessionRepo{session: &domain.ClassSession{ID: uuid.New(), ScheduleID: &scheduleID}}
	sessions.assigned = false
	uc := NewAttendanceUsecase(&attendanceInputRepo{}, sessions)
	_, err := uc.Create(context.Background(), uuid.New(), uuid.New(), &domain.CreateAttendanceRequest{
		EnrollmentID: uuid.New(), ScheduleID: &scheduleID, Date: "2026-08-22", Status: "present",
	})
	if !errors.Is(err, domain.ErrAttendanceForbidden) {
		t.Fatalf("error = %v, want %v", err, domain.ErrAttendanceForbidden)
	}
}
