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

type concurrentEnrollmentRepo struct {
	repository.EnrollmentRepository
	mu       sync.Mutex
	occupied bool
}

func (r *concurrentEnrollmentRepo) GetByIdempotencyKey(context.Context, uuid.UUID, string) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}
func (r *concurrentEnrollmentRepo) CreateIfCapacityAvailable(_ context.Context, enrollment *domain.Enrollment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if enrollment.ScheduleID == nil {
		return domain.ErrScheduleNotFound
	}
	if r.occupied {
		return domain.ErrScheduleFull
	}
	r.occupied = true
	return nil
}
func (r *concurrentEnrollmentRepo) Update(context.Context, *domain.Enrollment) error { return nil }

// The embedded interfaces keep this test double focused on the concurrent path.
type concurrentStudentRepo struct {
	repository.StudentRepository
	student *domain.Student
}

func (r *concurrentStudentRepo) GetByID(context.Context, uuid.UUID) (*domain.Student, error) {
	return r.student, nil
}

type concurrentClassRepo struct {
	repository.ClassRepository
	class *domain.Class
}

func (r *concurrentClassRepo) GetByID(context.Context, uuid.UUID) (*domain.Class, error) {
	return r.class, nil
}

type concurrentBilling struct{}

func (concurrentBilling) GenerateInvoice(context.Context, billing.InvoiceRequest) (*billing.InvoiceResponse, error) {
	return &billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://checkout.test"}, nil
}

type concurrentTx struct{}

func (concurrentTx) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func TestEnrollPublicConcurrentScheduleCapacity(t *testing.T) {
	tenantID, classID, scheduleID := uuid.New(), uuid.New(), uuid.New()
	class := &domain.Class{ID: classID, TenantID: tenantID, Type: "group", IsPublished: true, EnrollmentStatus: "open", Price: 100}
	parentID := uuid.New()
	results := make(chan error, 2)
	repo := &concurrentEnrollmentRepo{}
	uc := NewEnrollmentUsecase(repo, &concurrentStudentRepo{student: &domain.Student{ParentID: parentID}}, &concurrentClassRepo{class: class}, concurrentBilling{}, concurrentTx{})

	var waitGroup sync.WaitGroup
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, err := uc.EnrollPublic(context.Background(), parentID, classID, &domain.PublicEnrollmentRequest{
				StudentID: uuid.New(), BillingCycle: "monthly", ScheduleID: &scheduleID,
			}, uuid.NewString())
			results <- err
		}()
	}
	waitGroup.Wait()
	close(results)

	var success, full int
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, domain.ErrScheduleFull):
			full++
		default:
			t.Fatalf("unexpected enrollment error: %v", err)
		}
	}
	if success != 1 || full != 1 {
		t.Fatalf("concurrent results: success=%d full=%d, want one each", success, full)
	}
}
