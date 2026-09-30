package usecase

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

// feeEnrollmentRepo keeps enrollments in memory with the predicates the PostgreSQL
// repository uses: a seat and the student/class uniqueness count only pending and
// active rows, and an Idempotency-Key is unique across every row.
type feeEnrollmentRepo struct {
	repository.EnrollmentRepository
	mu        sync.Mutex
	rows      map[uuid.UUID]*domain.Enrollment
	parents   map[uuid.UUID]uuid.UUID // student -> parent
	capacity  map[uuid.UUID]int       // schedule -> capacity
	updateErr error
}

func newFeeEnrollmentRepo() *feeEnrollmentRepo {
	return &feeEnrollmentRepo{rows: map[uuid.UUID]*domain.Enrollment{}, parents: map[uuid.UUID]uuid.UUID{}, capacity: map[uuid.UUID]int{}}
}

func live(e *domain.Enrollment) bool { return e.Status == "pending" || e.Status == "active" }

func (r *feeEnrollmentRepo) find(match func(*domain.Enrollment) bool) (*domain.Enrollment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if match(row) {
			copied := *row
			return &copied, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func keyIs(e *domain.Enrollment, key string) bool {
	return e.IdempotencyKey != nil && *e.IdempotencyKey == key
}

func (r *feeEnrollmentRepo) GetByIdempotencyKey(_ context.Context, parentID uuid.UUID, key string) (*domain.Enrollment, error) {
	return r.find(func(e *domain.Enrollment) bool { return keyIs(e, key) && r.parents[e.StudentID] == parentID })
}
func (r *feeEnrollmentRepo) GetByIdempotencyKeyForTenant(_ context.Context, tenantID uuid.UUID, key string) (*domain.Enrollment, error) {
	return r.find(func(e *domain.Enrollment) bool { return keyIs(e, key) && e.TenantID == tenantID })
}
func (r *feeEnrollmentRepo) GetByIdempotencyKeyAny(_ context.Context, key string) (*domain.Enrollment, error) {
	return r.find(func(e *domain.Enrollment) bool { return keyIs(e, key) })
}
func (r *feeEnrollmentRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Enrollment, error) {
	return r.find(func(e *domain.Enrollment) bool { return e.ID == id })
}
func (r *feeEnrollmentRepo) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Enrollment, error) {
	return r.GetByID(ctx, id)
}

// KEL-149: keeps the stub a full EnrollmentLockingRepository after the interface
// grew ResumeUnderCapacity; the marketplace fee path never calls it.
func (r *feeEnrollmentRepo) ResumeUnderCapacity(context.Context, *domain.Enrollment) error {
	return nil
}

func (r *feeEnrollmentRepo) CreateIfCapacityAvailable(_ context.Context, enrollment *domain.Enrollment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	seats := 0
	for _, row := range r.rows {
		if keyIs(row, *enrollment.IdempotencyKey) {
			return errors.New("unique violation on idx_enrollments_idempotency_key")
		}
		if live(row) && row.StudentID == enrollment.StudentID && row.ClassID == enrollment.ClassID {
			return domain.ErrDuplicateEnrollment
		}
		if live(row) && enrollment.ScheduleID != nil && row.ScheduleID != nil && *row.ScheduleID == *enrollment.ScheduleID {
			seats++
		}
	}
	if enrollment.ScheduleID != nil && seats >= r.capacity[*enrollment.ScheduleID] {
		return domain.ErrScheduleFull
	}
	copied := *enrollment
	r.rows[enrollment.ID] = &copied
	return nil
}

func (r *feeEnrollmentRepo) Update(_ context.Context, enrollment *domain.Enrollment) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *enrollment
	r.rows[enrollment.ID] = &copied
	return nil
}

func (r *feeEnrollmentRepo) liveSeats(scheduleID uuid.UUID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	seats := 0
	for _, row := range r.rows {
		if live(row) && row.ScheduleID != nil && *row.ScheduleID == scheduleID {
			seats++
		}
	}
	return seats
}

func (r *feeEnrollmentRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rows)
}

// feeBilling refuses every invoice with err until err is cleared, like billing
// before and after an admin lowers the platform fee.
type feeBilling struct {
	billing.Client
	err   error
	calls int
}

func (b *feeBilling) GenerateInvoice(context.Context, billing.InvoiceRequest) (*billing.InvoiceResponse, error) {
	b.calls++
	if b.err != nil {
		return nil, b.err
	}
	return &billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://checkout.test"}, nil
}

type feeFixture struct {
	repo       *feeEnrollmentRepo
	billing    *feeBilling
	uc         EnrollmentUsecase
	parentID   uuid.UUID
	studentID  uuid.UUID
	tenantID   uuid.UUID
	classID    uuid.UUID
	scheduleID uuid.UUID
}

// newFeeFixture builds a group class whose only schedule has capacity seats.
func newFeeFixture(capacity int) *feeFixture {
	f := &feeFixture{repo: newFeeEnrollmentRepo(), billing: &feeBilling{err: billing.ErrPlatformFeeExceedsGross}, parentID: uuid.New(), studentID: uuid.New(), tenantID: uuid.New(), classID: uuid.New(), scheduleID: uuid.New()}
	f.repo.parents[f.studentID] = f.parentID
	f.repo.capacity[f.scheduleID] = capacity
	class := &domain.Class{ID: f.classID, TenantID: f.tenantID, Type: "group", Price: 100, IsPublished: true, EnrollmentStatus: "open"}
	f.uc = NewEnrollmentUsecase(f.repo, &marketplaceStudentRepo{student: &domain.Student{ID: f.studentID, ParentID: f.parentID}}, &marketplaceClassRepo{class: class}, f.billing, marketplaceTx{})
	return f
}

func (f *feeFixture) enrollPublic(key string) error {
	_, err := f.uc.EnrollPublic(context.Background(), f.parentID, f.classID, &domain.PublicEnrollmentRequest{StudentID: f.studentID, BillingCycle: "monthly", ScheduleID: &f.scheduleID}, key)
	return err
}

func (f *feeFixture) enrollTenant(key string) error {
	_, err := f.uc.EnrollStudent(context.Background(), f.tenantID, &domain.EnrollStudentRequest{StudentID: f.studentID, ClassID: f.classID, BillingCycle: "monthly", ScheduleID: &f.scheduleID, IdempotencyKey: key})
	return err
}

func TestPlatformFeeRejectionReleasesTheAttempt(t *testing.T) {
	for name, enroll := range map[string]func(*feeFixture, string) error{
		"parent checkout":   (*feeFixture).enrollPublic,
		"tenant enrollment": (*feeFixture).enrollTenant,
	} {
		t.Run(name, func(t *testing.T) {
			// Capacity 1: the rejected attempt must not take the last seat.
			f := newFeeFixture(1)
			if err := enroll(f, "rejected-key"); !errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
				t.Fatalf("rejected attempt = %v, want ErrPlatformFeeExceedsGross", err)
			}
			rejected, err := f.repo.GetByIdempotencyKeyAny(context.Background(), "rejected-key")
			if err != nil {
				t.Fatal(err)
			}
			if rejected.Status != "dropped" || rejected.PaymentStatus != domain.PaymentStatusPlatformFeeRejected || rejected.PaymentTransactionID != nil {
				t.Fatalf("rejected enrollment = status %q payment %q transaction %v, want dropped/%s/nil", rejected.Status, rejected.PaymentStatus, rejected.PaymentTransactionID, domain.PaymentStatusPlatformFeeRejected)
			}
			if seats := f.repo.liveSeats(f.scheduleID); seats != 0 {
				t.Fatalf("seats held after rejection = %d, want 0", seats)
			}

			// Replaying the same key answers the rejection again; it neither asks
			// billing for a new invoice nor creates another enrollment.
			if err := enroll(f, "rejected-key"); !errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
				t.Fatalf("same-key replay = %v, want ErrPlatformFeeExceedsGross", err)
			}
			if f.billing.calls != 1 || f.repo.count() != 1 {
				t.Fatalf("after replay: billing calls = %d, enrollments = %d, want 1 and 1", f.billing.calls, f.repo.count())
			}

			// Once billing accepts again, a new attempt for the same student and
			// class is not blocked as a duplicate and takes the last seat.
			f.billing.err = nil
			if err := enroll(f, "new-key"); err != nil {
				t.Fatalf("new attempt after rejection = %v, want success", err)
			}
			if seats := f.repo.liveSeats(f.scheduleID); seats != 1 {
				t.Fatalf("seats after new attempt = %d, want 1", seats)
			}
			// The rejected key still never turns into a success.
			if err := enroll(f, "rejected-key"); !errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
				t.Fatalf("replay after a later success = %v, want ErrPlatformFeeExceedsGross", err)
			}
		})
	}
}

// Another student can take the seat a rejected attempt would have held.
func TestPlatformFeeRejectionLeavesSeatForOthers(t *testing.T) {
	f := newFeeFixture(1)
	if err := f.enrollPublic(uuid.NewString()); !errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
		t.Fatalf("rejected attempt = %v", err)
	}
	other := uuid.New()
	f.repo.parents[other] = f.parentID
	f.billing.err = nil
	uc := NewEnrollmentUsecase(f.repo, &marketplaceStudentRepo{student: &domain.Student{ID: other, ParentID: f.parentID}}, &marketplaceClassRepo{class: &domain.Class{ID: f.classID, TenantID: f.tenantID, Type: "group", Price: 100, IsPublished: true, EnrollmentStatus: "open"}}, f.billing, marketplaceTx{})
	if _, err := uc.EnrollPublic(context.Background(), f.parentID, f.classID, &domain.PublicEnrollmentRequest{StudentID: other, BillingCycle: "monthly", ScheduleID: &f.scheduleID}, uuid.NewString()); err != nil {
		t.Fatalf("other student = %v, want the free seat", err)
	}
}

// A transient billing failure keeps the previous behaviour: the pending enrollment
// stays, so a same-key retry can still request its invoice.
func TestTransientInvoiceFailureKeepsPendingEnrollment(t *testing.T) {
	for name, enroll := range map[string]func(*feeFixture, string) error{
		"parent checkout":   (*feeFixture).enrollPublic,
		"tenant enrollment": (*feeFixture).enrollTenant,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFeeFixture(1)
			f.billing.err = errors.New("billing service returned status 503")
			err := enroll(f, "key")
			if err == nil || errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
				t.Fatalf("error = %v, want a generic invoice error", err)
			}
			row, lookupErr := f.repo.GetByIdempotencyKeyAny(context.Background(), "key")
			if lookupErr != nil || row.Status != "pending" || row.PaymentStatus != "pending" {
				t.Fatalf("enrollment = %+v (%v), want it still pending", row, lookupErr)
			}
		})
	}
}

// When the rejected attempt cannot be released, the caller gets an internal error
// rather than a 422 that claims no seat is held.
func TestPlatformFeeRejectionReleaseFailureIsNotReportedAsRejection(t *testing.T) {
	f := newFeeFixture(1)
	f.repo.updateErr = errors.New("database down")
	err := f.enrollPublic("key")
	if err == nil || errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
		t.Fatalf("error = %v, want an internal error", err)
	}
}
