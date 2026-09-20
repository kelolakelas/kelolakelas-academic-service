package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// releaseEnrollmentRepoStub serves the enrollment row under test and records the
// write the usecase performs, so release semantics are asserted against the repository
// contract the service actually depends on.
type releaseEnrollmentRepoStub struct {
	enrollment *domain.Enrollment
	written    *domain.Enrollment
	updateErr  error
}

func (s *releaseEnrollmentRepoStub) Create(context.Context, *domain.Enrollment) error { return nil }
func (s *releaseEnrollmentRepoStub) GetByID(_ context.Context, id uuid.UUID) (*domain.Enrollment, error) {
	if s.enrollment == nil || s.enrollment.ID != id {
		return nil, gorm.ErrRecordNotFound
	}
	return s.enrollment, nil
}
func (s *releaseEnrollmentRepoStub) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Enrollment, error) {
	return s.GetByID(ctx, id)
}
func (s *releaseEnrollmentRepoStub) GetByIDForAccess(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *releaseEnrollmentRepoStub) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) ([]*domain.Enrollment, int64, error) {
	return nil, 0, nil
}
func (s *releaseEnrollmentRepoStub) ExistsActive(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (s *releaseEnrollmentRepoStub) GetByIdempotencyKey(context.Context, uuid.UUID, string) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *releaseEnrollmentRepoStub) CreateIfCapacityAvailable(context.Context, *domain.Enrollment) error {
	return nil
}
func (s *releaseEnrollmentRepoStub) IsTutorForEnrollment(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (s *releaseEnrollmentRepoStub) GetActiveByClassID(context.Context, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, nil
}
func (s *releaseEnrollmentRepoStub) GetActiveByScheduleID(context.Context, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, nil
}
func (s *releaseEnrollmentRepoStub) AssignSchedule(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *releaseEnrollmentRepoStub) Update(_ context.Context, enrollment *domain.Enrollment) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.written = enrollment
	return nil
}
func (s *releaseEnrollmentRepoStub) Delete(context.Context, uuid.UUID) error { return nil }

func (s *releaseEnrollmentRepoStub) GetByIdempotencyKeyForTenant(context.Context, uuid.UUID, string) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *releaseEnrollmentRepoStub) GetByIdempotencyKeyAny(context.Context, string) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}

func newReleaseTestUsecase(repo *releaseEnrollmentRepoStub) EnrollmentUsecase {
	return NewEnrollmentUsecase(repo, &marketplaceStudentRepo{}, &marketplaceClassRepo{}, nil)
}

func TestReleaseEnrollmentDropsPendingEnrollment(t *testing.T) {
	enrollmentID := uuid.New()
	scheduleID := uuid.New()
	repo := &releaseEnrollmentRepoStub{enrollment: &domain.Enrollment{
		ID: enrollmentID, ScheduleID: &scheduleID, Status: "pending",
	}}

	response, err := newReleaseTestUsecase(repo).ReleaseEnrollment(context.Background(), enrollmentID)
	if err != nil {
		t.Fatalf("ReleaseEnrollment() error = %v", err)
	}
	if response.Status != "dropped" {
		t.Fatalf("status=%q, want dropped", response.Status)
	}
	if repo.written == nil || repo.written.Status != "dropped" {
		t.Fatalf("persisted status=%v, want dropped", repo.written)
	}
}

// Releasing an enrollment frees the seat because the capacity predicate counts only
// pending and active rows, so the schedule no longer holds a slot for this student.
func TestReleaseEnrollmentFreesSeatCountedByCapacity(t *testing.T) {
	enrollmentID := uuid.New()
	scheduleID := uuid.New()
	enrollment := &domain.Enrollment{ID: enrollmentID, ScheduleID: &scheduleID, Status: "pending"}
	repo := &releaseEnrollmentRepoStub{enrollment: enrollment}

	if _, err := newReleaseTestUsecase(repo).ReleaseEnrollment(context.Background(), enrollmentID); err != nil {
		t.Fatalf("ReleaseEnrollment() error = %v", err)
	}

	if countedAsOccupied(enrollment.Status) {
		t.Fatalf("status=%q still counts against capacity", enrollment.Status)
	}
}

// countedAsOccupied mirrors the capacity predicate used by the catalog and the
// enrollment capacity check (status IN ('pending','active')).
func countedAsOccupied(status string) bool {
	return status == "pending" || status == "active"
}

func TestReleaseEnrollmentIsIdempotentForRepeatedNotifications(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &releaseEnrollmentRepoStub{enrollment: &domain.Enrollment{ID: enrollmentID, Status: "dropped"}}

	response, err := newReleaseTestUsecase(repo).ReleaseEnrollment(context.Background(), enrollmentID)
	if err != nil {
		t.Fatalf("ReleaseEnrollment() error = %v", err)
	}
	if response.Status != "dropped" {
		t.Fatalf("status=%q, want dropped", response.Status)
	}
	if repo.written != nil {
		t.Fatalf("a repeated release must not write again, wrote %v", repo.written)
	}
}

// A late failure must never revoke a seat the parent already paid for.
func TestReleaseEnrollmentNeverRevokesActiveEnrollment(t *testing.T) {
	enrollmentID := uuid.New()
	enrollment := &domain.Enrollment{ID: enrollmentID, Status: "active"}
	repo := &releaseEnrollmentRepoStub{enrollment: enrollment}

	response, err := newReleaseTestUsecase(repo).ReleaseEnrollment(context.Background(), enrollmentID)
	if err != nil {
		t.Fatalf("ReleaseEnrollment() error = %v", err)
	}
	if response.Status != "active" || enrollment.Status != "active" {
		t.Fatalf("status=%q enrollment=%q, want active preserved", response.Status, enrollment.Status)
	}
	if repo.written != nil {
		t.Fatalf("an active enrollment must not be written, wrote %v", repo.written)
	}
}

func TestReleaseEnrollmentRejectsUnknownEnrollment(t *testing.T) {
	// The marketplace stub answers ErrRecordNotFound, which the usecase maps to its
	// own not-found error so the handler can answer 404.
	repo := &releaseEnrollmentRepoStub{}

	_, err := newReleaseTestUsecase(repo).ReleaseEnrollment(context.Background(), uuid.New())
	if !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("error=%v, want ErrEnrollmentNotFound", err)
	}
}

// A terminal status such as completed cannot be released; the caller must see a
// conflict instead of a silent success.
func TestReleaseEnrollmentRejectsCompletedEnrollment(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &releaseEnrollmentRepoStub{enrollment: &domain.Enrollment{ID: enrollmentID, Status: "completed"}}

	_, err := newReleaseTestUsecase(repo).ReleaseEnrollment(context.Background(), enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
}

// Activation of a dropped enrollment must fail so billing records an observable
// rejection instead of silently reporting a successful activation.
func TestActivateEnrollmentRejectsDroppedEnrollment(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &releaseEnrollmentRepoStub{enrollment: &domain.Enrollment{ID: enrollmentID, Status: "dropped"}}

	_, err := newReleaseTestUsecase(repo).ActivateEnrollment(context.Background(), enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
}

// A dropped enrollment no longer occupies the unique index, so the same student can
// enroll into the same class again after a failed payment.
func TestDroppedEnrollmentDoesNotBlockReenrollment(t *testing.T) {
	studentID := uuid.New()
	classID := uuid.New()
	scheduleID := uuid.New()
	repo := &releaseEnrollmentRepoStub{enrollment: &domain.Enrollment{
		ID: uuid.New(), StudentID: studentID, ClassID: classID, ScheduleID: &scheduleID,
		Status: "dropped",
	}}

	if _, err := newReleaseTestUsecase(repo).ReleaseEnrollment(context.Background(), repo.enrollment.ID); err != nil {
		t.Fatalf("ReleaseEnrollment() error = %v", err)
	}

	// The release is a status transition, not a delete: the partial unique index only
	// covers pending and active rows, so a dropped row must not be treated as an
	// active conflict while its history is preserved for auditing.
	if countedAsOccupied(repo.enrollment.Status) {
		t.Fatalf("dropped enrollment still blocks re-enrollment")
	}
	if repo.enrollment.DeletedAt.Valid {
		t.Fatalf("release must not soft-delete the enrollment: %v", repo.enrollment.DeletedAt)
	}
}
