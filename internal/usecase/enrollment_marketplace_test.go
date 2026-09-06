package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

type marketplaceEnrollmentRepo struct {
	existing  *domain.Enrollment
	created   *domain.Enrollment
	createErr error
	updateErr error
}

func (m *marketplaceEnrollmentRepo) Create(context.Context, *domain.Enrollment) error { return nil }
func (m *marketplaceEnrollmentRepo) GetByID(context.Context, uuid.UUID) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}
func (m *marketplaceEnrollmentRepo) GetByIDForAccess(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.Enrollment, error) {
	return nil, nil
}
func (m *marketplaceEnrollmentRepo) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) ([]*domain.Enrollment, int64, error) {
	return nil, 0, nil
}
func (m *marketplaceEnrollmentRepo) ExistsActive(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (m *marketplaceEnrollmentRepo) GetByIdempotencyKey(context.Context, uuid.UUID, string) (*domain.Enrollment, error) {
	if m.existing == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return m.existing, nil
}
func (m *marketplaceEnrollmentRepo) CreateIfCapacityAvailable(_ context.Context, enrollment *domain.Enrollment) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.created = enrollment
	return nil
}
func (m *marketplaceEnrollmentRepo) IsTutorForEnrollment(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (m *marketplaceEnrollmentRepo) GetActiveByClassID(context.Context, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, nil
}
func (m *marketplaceEnrollmentRepo) GetActiveByScheduleID(context.Context, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, nil
}
func (m *marketplaceEnrollmentRepo) AssignSchedule(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (m *marketplaceEnrollmentRepo) Update(_ context.Context, enrollment *domain.Enrollment) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.created = enrollment
	return nil
}
func (m *marketplaceEnrollmentRepo) Delete(context.Context, uuid.UUID) error { return nil }

type marketplaceStudentRepo struct{ student *domain.Student }

func (m *marketplaceStudentRepo) Create(context.Context, *domain.Student) error { return nil }
func (m *marketplaceStudentRepo) GetByID(context.Context, uuid.UUID) (*domain.Student, error) {
	if m.student == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return m.student, nil
}
func (m *marketplaceStudentRepo) GetByIDForAccess(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) (*domain.Student, error) {
	return m.student, nil
}
func (m *marketplaceStudentRepo) List(context.Context, *uuid.UUID, *uuid.UUID, domain.StudentQuery) ([]domain.Student, int64, error) {
	return nil, 0, nil
}
func (m *marketplaceStudentRepo) CountActiveEnrollments(context.Context, uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *marketplaceStudentRepo) Update(context.Context, *domain.Student) error { return nil }
func (m *marketplaceStudentRepo) Delete(context.Context, uuid.UUID) error       { return nil }

type marketplaceClassRepo struct{ class *domain.Class }

func (m *marketplaceClassRepo) Create(context.Context, *domain.Class) error { return nil }
func (m *marketplaceClassRepo) ListByTenant(context.Context, uuid.UUID, domain.ListQuery) ([]domain.Class, int64, error) {
	return nil, 0, nil
}
func (m *marketplaceClassRepo) GetByID(context.Context, uuid.UUID) (*domain.Class, error) {
	if m.class == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return m.class, nil
}
func (m *marketplaceClassRepo) UpdatePublicationStatus(context.Context, uuid.UUID, uuid.UUID, bool) (*domain.Class, error) {
	return m.class, nil
}
func (m *marketplaceClassRepo) Update(context.Context, *domain.Class) error { return nil }
func (m *marketplaceClassRepo) DeleteByTenant(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

type marketplaceBilling struct {
	err   error
	calls int
}

func (m *marketplaceBilling) GenerateInvoice(context.Context, billing.InvoiceRequest) (*billing.InvoiceResponse, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return &billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://checkout.test"}, nil
}

type marketplaceTx struct{}

func (marketplaceTx) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

var _ repository.EnrollmentRepository = (*marketplaceEnrollmentRepo)(nil)

func TestEnrollPublic(t *testing.T) {
	parentID, otherParentID, studentID, classID, tenantID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	baseStudent := &domain.Student{ID: studentID, ParentID: parentID}
	baseClass := &domain.Class{ID: classID, TenantID: tenantID, Price: 250000, IsPublished: true, EnrollmentStatus: "open"}
	tests := []struct {
		name       string
		parentID   uuid.UUID
		student    *domain.Student
		class      *domain.Class
		billingErr error
		key        string
		wantErr    error
		wantCalls  int
	}{
		{name: "ownership denied", parentID: otherParentID, student: baseStudent, class: baseClass, key: "a", wantErr: domain.ErrStudentOwnership},
		{name: "closed class denied", parentID: parentID, student: baseStudent, class: &domain.Class{ID: classID, TenantID: tenantID, IsPublished: true, EnrollmentStatus: "closed"}, key: "b", wantErr: domain.ErrClassNotEnrollable},
		{name: "billing error", parentID: parentID, student: baseStudent, class: baseClass, billingErr: errors.New("billing down"), key: "c", wantCalls: 1},
		{name: "success", parentID: parentID, student: baseStudent, class: baseClass, key: "d", wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &marketplaceEnrollmentRepo{}
			billingMock := &marketplaceBilling{err: test.billingErr}
			uc := NewEnrollmentUsecase(repo, &marketplaceStudentRepo{student: test.student}, &marketplaceClassRepo{class: test.class}, billingMock, marketplaceTx{}).(*enrollmentUsecase)
			result, err := uc.EnrollPublic(context.Background(), test.parentID, classID, &domain.PublicEnrollmentRequest{StudentID: studentID, BillingCycle: "monthly"}, test.key)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want %v", err, test.wantErr)
				}
			} else if test.name == "success" && (err != nil || result.Payment.GrossAmount != 250000) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if billingMock.calls != test.wantCalls {
				t.Fatalf("billing calls=%d, want %d", billingMock.calls, test.wantCalls)
			}
		})
	}
}

func TestEnrollPublicIdempotency(t *testing.T) {
	parentID, studentID, classID := uuid.New(), uuid.New(), uuid.New()
	transactionID := uuid.New()
	existing := &domain.Enrollment{ID: uuid.New(), StudentID: studentID, ClassID: classID, BillingCycle: "monthly", PaymentStatus: "pending", GrossAmount: 100, PaymentTransactionID: &transactionID}
	repo := &marketplaceEnrollmentRepo{existing: existing}
	uc := NewEnrollmentUsecase(repo, &marketplaceStudentRepo{}, &marketplaceClassRepo{}, &marketplaceBilling{}, marketplaceTx{}).(*enrollmentUsecase)
	if _, err := uc.EnrollPublic(context.Background(), parentID, classID, &domain.PublicEnrollmentRequest{StudentID: studentID, BillingCycle: "monthly"}, "same"); err != nil {
		t.Fatalf("replay error: %v", err)
	}
	if _, err := uc.EnrollPublic(context.Background(), parentID, uuid.New(), &domain.PublicEnrollmentRequest{StudentID: studentID, BillingCycle: "monthly"}, "same"); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("conflict error = %v", err)
	}
}

var _ = time.Time{}
