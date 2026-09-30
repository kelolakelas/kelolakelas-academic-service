package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// suspendEnrollmentRepoStub extends the release stub with ResumeUnderCapacity,
// the one locking-repository method the resume path needs. Its resume mirrors
// the real repository contract: on success the enrollment is written back as
// active; on resumeErr nothing is written and the enrollment stays suspended.
type suspendEnrollmentRepoStub struct {
	*releaseEnrollmentRepoStub
	resumeErr   error
	resumeCalls int
}

func (s *suspendEnrollmentRepoStub) ResumeUnderCapacity(_ context.Context, enrollment *domain.Enrollment) error {
	s.resumeCalls++
	if s.resumeErr != nil {
		return s.resumeErr
	}
	enrollment.Status = domain.EnrollmentStatusActive
	return nil
}

func newSuspendTestUsecase(repo *suspendEnrollmentRepoStub) EnrollmentUsecase {
	return NewEnrollmentUsecase(repo, &marketplaceStudentRepo{}, &marketplaceClassRepo{}, nil)
}

func TestSuspendEnrollmentMovesActiveToSuspended(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusActive},
	}}

	response, err := newSuspendTestUsecase(repo).SuspendEnrollment(context.Background(), enrollmentID)
	if err != nil {
		t.Fatalf("SuspendEnrollment() error = %v", err)
	}
	if response.Status != domain.EnrollmentStatusSuspended {
		t.Fatalf("status=%q, want suspended", response.Status)
	}
	if repo.written == nil || repo.written.Status != domain.EnrollmentStatusSuspended {
		t.Fatalf("persisted status=%v, want suspended", repo.written)
	}
}

func TestSuspendEnrollmentIsIdempotent(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusSuspended},
	}}

	response, err := newSuspendTestUsecase(repo).SuspendEnrollment(context.Background(), enrollmentID)
	if err != nil {
		t.Fatalf("repeated SuspendEnrollment() error = %v", err)
	}
	if response.Status != domain.EnrollmentStatusSuspended {
		t.Fatalf("status=%q, want suspended", response.Status)
	}
	if repo.written != nil {
		t.Fatalf("a repeated suspend must not write again, wrote %v", repo.written)
	}
}

// A pending enrollment holds a seat but is not started; suspending it would blur
// the line with the payment-failure release, so it must answer a conflict.
func TestSuspendEnrollmentRejectsPendingEnrollment(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusPending},
	}}

	_, err := newSuspendTestUsecase(repo).SuspendEnrollment(context.Background(), enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
	if repo.written != nil {
		t.Fatalf("a refused suspend must not write, wrote %v", repo.written)
	}
}

func TestSuspendEnrollmentRejectsTerminalEnrollment(t *testing.T) {
	for _, status := range []string{domain.EnrollmentStatusCompleted, domain.EnrollmentStatusDropped} {
		enrollmentID := uuid.New()
		repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
			enrollment: &domain.Enrollment{ID: enrollmentID, Status: status},
		}}

		_, err := newSuspendTestUsecase(repo).SuspendEnrollment(context.Background(), enrollmentID)
		if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
			t.Fatalf("status %q: error=%v, want ErrInvalidEnrollmentTransition", status, err)
		}
	}
}

func TestSuspendEnrollmentRejectsUnknownEnrollment(t *testing.T) {
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{}}

	_, err := newSuspendTestUsecase(repo).SuspendEnrollment(context.Background(), uuid.New())
	if !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("error=%v, want ErrEnrollmentNotFound", err)
	}
}

func TestResumeEnrollmentReclaimsSeat(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusSuspended},
	}}

	response, err := newSuspendTestUsecase(repo).ResumeEnrollment(context.Background(), enrollmentID)
	if err != nil {
		t.Fatalf("ResumeEnrollment() error = %v", err)
	}
	if repo.resumeCalls != 1 {
		t.Fatalf("resume calls=%d, want 1", repo.resumeCalls)
	}
	if response.Status != domain.EnrollmentStatusActive {
		t.Fatalf("status=%q, want active", response.Status)
	}
}

// Resume on an already active enrollment is the idempotent replay: it answers
// the enrollment unchanged and never re-runs the capacity check.
func TestResumeEnrollmentIsIdempotentWhenActive(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusActive},
	}}

	response, err := newSuspendTestUsecase(repo).ResumeEnrollment(context.Background(), enrollmentID)
	if err != nil {
		t.Fatalf("repeated ResumeEnrollment() error = %v", err)
	}
	if response.Status != domain.EnrollmentStatusActive {
		t.Fatalf("status=%q, want active", response.Status)
	}
	if repo.resumeCalls != 0 {
		t.Fatalf("resume calls=%d, want 0 for an already active enrollment", repo.resumeCalls)
	}
}

// The full-schedule conflict is the seat-reclamation refusal: the enrollment
// must stay suspended so the caller's retry can succeed once a seat frees up.
func TestResumeEnrollmentOnFullScheduleKeepsEnrollmentSuspended(t *testing.T) {
	enrollmentID := uuid.New()
	enrollment := &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusSuspended}
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{enrollment: enrollment}}
	repo.resumeErr = domain.ErrScheduleFull

	_, err := newSuspendTestUsecase(repo).ResumeEnrollment(context.Background(), enrollmentID)
	if !errors.Is(err, domain.ErrScheduleFull) {
		t.Fatalf("error=%v, want ErrScheduleFull", err)
	}
	if enrollment.Status != domain.EnrollmentStatusSuspended {
		t.Fatalf("status=%q, want the enrollment to stay suspended", enrollment.Status)
	}
	if repo.written != nil {
		t.Fatalf("a refused resume must not write, wrote %v", repo.written)
	}
}

// The student re-enrolled in the same class while this enrollment was
// suspended: the unique live-enrollment check answers the same conflict family.
func TestResumeEnrollmentConflictMapsToConflict(t *testing.T) {
	enrollmentID := uuid.New()
	enrollment := &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusSuspended}
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{enrollment: enrollment}}
	repo.resumeErr = domain.ErrEnrollmentSuspendedConflict

	_, err := newSuspendTestUsecase(repo).ResumeEnrollment(context.Background(), enrollmentID)
	if !errors.Is(err, domain.ErrEnrollmentSuspendedConflict) {
		t.Fatalf("error=%v, want ErrEnrollmentSuspendedConflict", err)
	}
	if enrollment.Status != domain.EnrollmentStatusSuspended {
		t.Fatalf("status=%q, want the enrollment to stay suspended", enrollment.Status)
	}
}

// A pending enrollment was never suspended, so there is no seat to reclaim.
func TestResumeEnrollmentRejectsPendingEnrollment(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusPending},
	}}

	_, err := newSuspendTestUsecase(repo).ResumeEnrollment(context.Background(), enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
	if repo.resumeCalls != 0 {
		t.Fatalf("resume calls=%d, want 0 for a pending enrollment", repo.resumeCalls)
	}
}

func TestResumeEnrollmentRejectsUnknownEnrollment(t *testing.T) {
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{}}

	_, err := newSuspendTestUsecase(repo).ResumeEnrollment(context.Background(), uuid.New())
	if !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("error=%v, want ErrEnrollmentNotFound", err)
	}
}

func TestEndEnrollmentDropsActiveEnrollment(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusActive},
	}}

	response, err := newSuspendTestUsecase(repo).EndEnrollment(context.Background(), enrollmentID)
	if err != nil {
		t.Fatalf("EndEnrollment() error = %v", err)
	}
	if response.Status != domain.EnrollmentStatusDropped {
		t.Fatalf("status=%q, want dropped", response.Status)
	}
	if repo.written == nil || repo.written.Status != domain.EnrollmentStatusDropped {
		t.Fatalf("persisted status=%v, want dropped", repo.written)
	}
}

func TestEndEnrollmentDropsSuspendedEnrollment(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusSuspended},
	}}

	response, err := newSuspendTestUsecase(repo).EndEnrollment(context.Background(), enrollmentID)
	if err != nil {
		t.Fatalf("EndEnrollment() error = %v", err)
	}
	if response.Status != domain.EnrollmentStatusDropped {
		t.Fatalf("status=%q, want dropped", response.Status)
	}
}

func TestEndEnrollmentIsIdempotent(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusDropped},
	}}

	response, err := newSuspendTestUsecase(repo).EndEnrollment(context.Background(), enrollmentID)
	if err != nil {
		t.Fatalf("repeated EndEnrollment() error = %v", err)
	}
	if response.Status != domain.EnrollmentStatusDropped {
		t.Fatalf("status=%q, want dropped", response.Status)
	}
	if repo.written != nil {
		t.Fatalf("a repeated end must not write again, wrote %v", repo.written)
	}
}

// A pending enrollment has an invoice that only the parent cancellation or the
// payment-failure release may unwind; ending it here would drop the seat while
// billing still collects.
func TestEndEnrollmentRejectsPendingEnrollment(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusPending},
	}}

	_, err := newSuspendTestUsecase(repo).EndEnrollment(context.Background(), enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
	if repo.written != nil {
		t.Fatalf("a refused end must not write, wrote %v", repo.written)
	}
}

func TestEndEnrollmentRejectsCompletedEnrollment(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusCompleted},
	}}

	_, err := newSuspendTestUsecase(repo).EndEnrollment(context.Background(), enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
}

func TestEndEnrollmentRejectsUnknownEnrollment(t *testing.T) {
	repo := &suspendEnrollmentRepoStub{releaseEnrollmentRepoStub: &releaseEnrollmentRepoStub{}}

	_, err := newSuspendTestUsecase(repo).EndEnrollment(context.Background(), uuid.New())
	if !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("error=%v, want ErrEnrollmentNotFound", err)
	}
}

// A suspended enrollment was active before billing parked it: the parent cannot
// cancel it, and — the safety property — billing's withdrawal must never run
// for it, because its invoice stays collectable while it is parked.
func TestCancelPendingRejectsSuspendedWithoutTouchingBilling(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	billing := &cancelBillingStub{}
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: domain.EnrollmentStatusSuspended},
	}
	uc := newCancelTestUsecase(repo, billing)

	_, err := uc.CancelPendingEnrollment(context.Background(), parentID, enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
	if billing.calls != 0 {
		t.Fatalf("billing withdrawal calls=%d, want 0 for a suspended enrollment", billing.calls)
	}
	if repo.written != nil {
		t.Fatalf("a refused cancel must not write, wrote %v", repo.written)
	}
}

// gorm.ErrRecordNotFound is kept as the sentinel the read paths return.
var _ = gorm.ErrRecordNotFound
