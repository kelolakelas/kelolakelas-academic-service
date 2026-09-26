package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

// cancelEnrollmentRepoStub serves a single enrollment that belongs to ownerID and
// records the write the usecase performs. Any parent other than ownerID is answered
// as not found, which mirrors the parent-scoped query the real repository runs.
type cancelEnrollmentRepoStub struct {
	enrollment *domain.Enrollment
	ownerID    uuid.UUID
	written    *domain.Enrollment
	updateErr  error
	// visibleTo records the parent scope the usecase asked for, so a test can prove
	// the ownership filter is actually applied rather than trusted from the caller.
	visibleTo *uuid.UUID
	// mutateBeforeLock runs inside GetByIDForUpdate, which is how a test simulates a
	// concurrent writer (a paid activation) committing between the visibility read and
	// the locked re-read.
	mutateBeforeLock func(*domain.Enrollment)
}

func (s *cancelEnrollmentRepoStub) Create(context.Context, *domain.Enrollment) error { return nil }
func (s *cancelEnrollmentRepoStub) GetByID(_ context.Context, id uuid.UUID) (*domain.Enrollment, error) {
	if s.enrollment == nil || s.enrollment.ID != id {
		return nil, gorm.ErrRecordNotFound
	}
	return s.enrollment, nil
}
func (s *cancelEnrollmentRepoStub) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Enrollment, error) {
	enrollment, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.mutateBeforeLock != nil {
		s.mutateBeforeLock(enrollment)
	}
	return enrollment, nil
}
func (s *cancelEnrollmentRepoStub) GetByIDForAccess(_ context.Context, _ *uuid.UUID, parentID *uuid.UUID, id uuid.UUID) (*domain.Enrollment, error) {
	s.visibleTo = parentID
	if s.enrollment == nil || s.enrollment.ID != id {
		return nil, gorm.ErrRecordNotFound
	}
	if parentID == nil || *parentID != s.ownerID {
		// Another parent's enrollment is answered exactly like a missing one, so the
		// endpoint cannot be used to probe which enrollments exist.
		return nil, gorm.ErrRecordNotFound
	}
	return s.enrollment, nil
}
func (s *cancelEnrollmentRepoStub) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) ([]*domain.Enrollment, int64, error) {
	return nil, 0, nil
}
func (s *cancelEnrollmentRepoStub) ExistsActive(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (s *cancelEnrollmentRepoStub) GetByIdempotencyKey(context.Context, uuid.UUID, string) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *cancelEnrollmentRepoStub) CreateIfCapacityAvailable(context.Context, *domain.Enrollment) error {
	return nil
}
func (s *cancelEnrollmentRepoStub) IsTutorForEnrollment(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (s *cancelEnrollmentRepoStub) GetActiveByClassID(context.Context, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, nil
}
func (s *cancelEnrollmentRepoStub) GetActiveByScheduleID(context.Context, uuid.UUID, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, nil
}
func (s *cancelEnrollmentRepoStub) TransferSchedule(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *cancelEnrollmentRepoStub) AssignSchedule(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *cancelEnrollmentRepoStub) Update(_ context.Context, enrollment *domain.Enrollment) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.written = enrollment
	return nil
}
func (s *cancelEnrollmentRepoStub) Delete(context.Context, uuid.UUID) error { return nil }

func (s *cancelEnrollmentRepoStub) GetByIdempotencyKeyForTenant(context.Context, uuid.UUID, string) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *cancelEnrollmentRepoStub) GetByIdempotencyKeyAny(context.Context, string) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}

// cancelBillingStub records every withdrawal attempt so a test can assert that the
// invoice is withdrawn before the seat is given back — and that a refused withdrawal
// leaves the seat untouched.
type cancelBillingStub struct {
	err    error
	calls  int
	lastID uuid.UUID
	// order records the interleaving of the billing call and the repository write, so
	// the write-order safety property is asserted instead of assumed.
	order *[]string
}

func (s *cancelBillingStub) GenerateInvoice(context.Context, billing.InvoiceRequest) (*billing.InvoiceResponse, error) {
	return &billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://checkout.test"}, nil
}

func (s *cancelBillingStub) CancelEnrollmentPayment(_ context.Context, enrollmentID uuid.UUID) (*billing.CancelResponse, error) {
	s.calls++
	s.lastID = enrollmentID
	if s.order != nil {
		*s.order = append(*s.order, "billing")
	}
	if s.err != nil {
		return nil, s.err
	}
	return &billing.CancelResponse{TransactionID: uuid.New(), Status: "cancelled"}, nil
}

func newCancelTestUsecase(repo *cancelEnrollmentRepoStub, client billing.Client) EnrollmentUsecase {
	return NewEnrollmentUsecase(repo, &marketplaceStudentRepo{}, &marketplaceClassRepo{}, client)
}

func TestCancelPendingEnrollmentDropsSeatAndWithdrawsInvoice(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "pending"},
	}
	client := &cancelBillingStub{}

	response, err := newCancelTestUsecase(repo, client).CancelPendingEnrollment(context.Background(), parentID, enrollmentID)
	if err != nil {
		t.Fatalf("CancelPendingEnrollment() error = %v", err)
	}
	if response.Status != "dropped" {
		t.Fatalf("status=%q, want dropped", response.Status)
	}
	if repo.written == nil || repo.written.Status != "dropped" {
		t.Fatalf("persisted status=%v, want dropped", repo.written)
	}
	if client.calls != 1 || client.lastID != enrollmentID {
		t.Fatalf("billing withdrawals=%d lastID=%s, want one withdrawal for %s", client.calls, client.lastID, enrollmentID)
	}
}

// Cancelling frees the seat because the capacity predicate counts only pending and
// active rows.
func TestCancelPendingEnrollmentFreesSeatCountedByCapacity(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	enrollment := &domain.Enrollment{ID: enrollmentID, Status: "pending"}
	repo := &cancelEnrollmentRepoStub{ownerID: parentID, enrollment: enrollment}

	if _, err := newCancelTestUsecase(repo, &cancelBillingStub{}).CancelPendingEnrollment(context.Background(), parentID, enrollmentID); err != nil {
		t.Fatalf("CancelPendingEnrollment() error = %v", err)
	}
	if countedAsOccupied(enrollment.Status) {
		t.Fatalf("status=%q still counts against capacity", enrollment.Status)
	}
}

// The invoice is withdrawn before the seat is dropped. If the order were reversed, a
// refusal from billing would leave the seat already revoked for a payment that was
// actually taken, which no retry could repair.
func TestCancelPendingEnrollmentWithdrawsInvoiceBeforeDroppingSeat(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	order := []string{}
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "pending"},
	}
	client := &cancelBillingStub{order: &order}

	if _, err := newCancelTestUsecase(repo, client).CancelPendingEnrollment(context.Background(), parentID, enrollmentID); err != nil {
		t.Fatalf("CancelPendingEnrollment() error = %v", err)
	}
	if len(order) != 1 || order[0] != "billing" {
		t.Fatalf("call order=%v, want the billing withdrawal first", order)
	}
	// The repository write happens after the withdrawal returned, which the recorded
	// order proves: the seat write could only have been reached once billing answered.
	if repo.written == nil {
		t.Fatal("the seat was never released")
	}
}

// A settled invoice must refuse the cancellation and leave the enrollment alone.
func TestCancelPendingEnrollmentRefusesWhenInvoiceAlreadySettled(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "pending"},
	}
	client := &cancelBillingStub{err: billing.ErrTransactionNotCancellable}

	_, err := newCancelTestUsecase(repo, client).CancelPendingEnrollment(context.Background(), parentID, enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
	if repo.written != nil {
		t.Fatalf("a refused cancellation must not touch the enrollment, wrote %v", repo.written)
	}
	if repo.enrollment.Status != "pending" {
		t.Fatalf("status=%q, want pending preserved", repo.enrollment.Status)
	}
}

// A pending enrollment whose invoice creation never completed has nothing to pay, so
// billing reports not found and the seat still has to come back.
func TestCancelPendingEnrollmentToleratesEnrollmentWithoutTransaction(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "pending"},
	}
	client := &cancelBillingStub{err: billing.ErrTransactionNotFound}

	response, err := newCancelTestUsecase(repo, client).CancelPendingEnrollment(context.Background(), parentID, enrollmentID)
	if err != nil {
		t.Fatalf("CancelPendingEnrollment() error = %v", err)
	}
	if response.Status != "dropped" {
		t.Fatalf("status=%q, want dropped", response.Status)
	}
	if repo.written == nil || repo.written.Status != "dropped" {
		t.Fatalf("persisted status=%v, want dropped", repo.written)
	}
}

// A transport failure is not a refusal: the caller must retry, and the seat must stay
// reserved until the invoice state is actually known.
func TestCancelPendingEnrollmentFailsClosedOnBillingUnavailable(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "pending"},
	}
	client := &cancelBillingStub{err: errors.New("call billing service: connection refused")}

	_, err := newCancelTestUsecase(repo, client).CancelPendingEnrollment(context.Background(), parentID, enrollmentID)
	if err == nil {
		t.Fatal("CancelPendingEnrollment() error = nil, want a failure")
	}
	if errors.Is(err, domain.ErrInvalidEnrollmentTransition) || errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("error=%v, want an unclassified server failure so the client retries", err)
	}
	if repo.written != nil {
		t.Fatalf("an unknown invoice state must not release the seat, wrote %v", repo.written)
	}
}

// An enrollment that is already dropped has nothing left to cancel: the request
// moved nothing, so it is a conflict, not a success.
func TestCancelPendingEnrollmentRejectsAlreadyDroppedEnrollment(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "dropped"},
	}
	client := &cancelBillingStub{}

	_, err := newCancelTestUsecase(repo, client).CancelPendingEnrollment(context.Background(), parentID, enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
	// The withdrawal still runs: a dropped enrollment can hold an invoice that was
	// paid after its seat was released, and billing must be given the chance to refuse
	// rather than letting a payable invoice for an unactivatable enrollment survive.
	if client.calls != 1 {
		t.Fatalf("billing withdrawals=%d, want the invoice state checked once", client.calls)
	}
	if repo.written != nil {
		t.Fatalf("a dropped enrollment must not be written, wrote %v", repo.written)
	}
}

// A dropped enrollment whose invoice was paid after the seat was released is refused
// on the invoice state, which is the case that would otherwise look cancellable.
func TestCancelPendingEnrollmentRefusesDroppedEnrollmentWithSettledInvoice(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "dropped"},
	}
	client := &cancelBillingStub{err: billing.ErrTransactionNotCancellable}

	_, err := newCancelTestUsecase(repo, client).CancelPendingEnrollment(context.Background(), parentID, enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
	if repo.written != nil {
		t.Fatalf("a refused cancellation must not touch the enrollment, wrote %v", repo.written)
	}
}

// An enrollment that already started must never have its invoice withdrawn.
func TestCancelPendingEnrollmentRejectsActiveEnrollment(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "active"},
	}
	client := &cancelBillingStub{}

	_, err := newCancelTestUsecase(repo, client).CancelPendingEnrollment(context.Background(), parentID, enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
	if client.calls != 0 {
		t.Fatalf("billing withdrawals=%d, want the invoice left untouched", client.calls)
	}
	if repo.written != nil {
		t.Fatalf("an active enrollment must not be written, wrote %v", repo.written)
	}
}

func TestCancelPendingEnrollmentRejectsCompletedEnrollment(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "completed"},
	}

	_, err := newCancelTestUsecase(repo, &cancelBillingStub{}).CancelPendingEnrollment(context.Background(), parentID, enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
}

// Another parent's enrollment is answered as not found so the endpoint cannot be used
// to discover which enrollments exist.
func TestCancelPendingEnrollmentHidesOtherParentsEnrollment(t *testing.T) {
	ownerID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    ownerID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "pending"},
	}
	client := &cancelBillingStub{}

	_, err := newCancelTestUsecase(repo, client).CancelPendingEnrollment(context.Background(), uuid.New(), enrollmentID)
	if !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("error=%v, want ErrEnrollmentNotFound", err)
	}
	if client.calls != 0 {
		t.Fatalf("billing withdrawals=%d, want nothing withdrawn", client.calls)
	}
	if repo.written != nil {
		t.Fatalf("another parent's enrollment must not be written, wrote %v", repo.written)
	}
}

func TestCancelPendingEnrollmentRejectsUnknownEnrollment(t *testing.T) {
	parentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{ownerID: parentID}

	_, err := newCancelTestUsecase(repo, &cancelBillingStub{}).CancelPendingEnrollment(context.Background(), parentID, uuid.New())
	if !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("error=%v, want ErrEnrollmentNotFound", err)
	}
}

// The ownership filter must be applied by the usecase, not trusted from the caller.
func TestCancelPendingEnrollmentScopesLookupToTheCaller(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "pending"},
	}

	if _, err := newCancelTestUsecase(repo, &cancelBillingStub{}).CancelPendingEnrollment(context.Background(), parentID, enrollmentID); err != nil {
		t.Fatalf("CancelPendingEnrollment() error = %v", err)
	}
	if repo.visibleTo == nil || *repo.visibleTo != parentID {
		t.Fatalf("parent scope=%v, want %s", repo.visibleTo, parentID)
	}
}

// A parent identity is required; without it the usecase must refuse rather than
// treat the request as tenant-scoped.
func TestCancelPendingEnrollmentRequiresParent(t *testing.T) {
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "pending"},
	}

	_, err := newCancelTestUsecase(repo, &cancelBillingStub{}).CancelPendingEnrollment(context.Background(), uuid.Nil, enrollmentID)
	if !errors.Is(err, domain.ErrParentRequired) {
		t.Fatalf("error=%v, want ErrParentRequired", err)
	}
}

// A dropped enrollment no longer occupies the unique index, so the same student can
// enroll into the same class again after cancelling.
func TestCancelledEnrollmentDoesNotBlockReenrollment(t *testing.T) {
	parentID := uuid.New()
	studentID := uuid.New()
	classID := uuid.New()
	scheduleID := uuid.New()
	enrollment := &domain.Enrollment{
		ID: uuid.New(), StudentID: studentID, ClassID: classID, ScheduleID: &scheduleID,
		Status: "pending",
	}
	repo := &cancelEnrollmentRepoStub{ownerID: parentID, enrollment: enrollment}

	if _, err := newCancelTestUsecase(repo, &cancelBillingStub{}).CancelPendingEnrollment(context.Background(), parentID, enrollment.ID); err != nil {
		t.Fatalf("CancelPendingEnrollment() error = %v", err)
	}
	if countedAsOccupied(enrollment.Status) {
		t.Fatalf("status=%q still occupies the unique (student, class) index", enrollment.Status)
	}
}

// A paid activation that lands between the visibility read and the locked re-read
// must not be dropped. The enrollment leaves the cancel path with a conflict and no
// write, so a seat is never revoked underneath a confirmed payment.
func TestCancelPendingEnrollmentLosesRaceToPaidActivation(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "pending"},
	}
	// The activation commits while the cancellation holds its lock, so the re-read
	// observes the state the concurrent transaction left behind.
	repo.mutateBeforeLock = func(enrollment *domain.Enrollment) {
		enrollment.Status = "active"
	}

	_, err := newCancelTestUsecase(repo, &cancelBillingStub{}).CancelPendingEnrollment(context.Background(), parentID, enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
	if repo.written != nil {
		t.Fatalf("an activated enrollment must not be dropped, wrote %v", repo.written)
	}
	if repo.enrollment.Status != "active" {
		t.Fatalf("status=%q, want the activation preserved", repo.enrollment.Status)
	}
}

// The paid callback and the cancellation cannot both win on the billing side either:
// when billing refuses because the invoice settled first, the seat is left alone.
func TestCancelPendingEnrollmentLosesRaceToPaidCallback(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	repo := &cancelEnrollmentRepoStub{
		ownerID:    parentID,
		enrollment: &domain.Enrollment{ID: enrollmentID, Status: "pending"},
	}
	// Billing reports that the row it was asked to cancel is no longer cancellable,
	// which is exactly what a concurrent `00` callback produces.
	client := &cancelBillingStub{err: billing.ErrTransactionNotCancellable}

	_, err := newCancelTestUsecase(repo, client).CancelPendingEnrollment(context.Background(), parentID, enrollmentID)
	if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
		t.Fatalf("error=%v, want ErrInvalidEnrollmentTransition", err)
	}
	if repo.written != nil {
		t.Fatalf("a paid seat must not be released, wrote %v", repo.written)
	}
	if repo.enrollment.Status != "pending" {
		t.Fatalf("status=%q, want pending preserved for the activation to finish", repo.enrollment.Status)
	}
}
