package usecase

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

// emailInvoiceBilling records every invoice request so a test can prove the
// parent's email claim reaches billing on the paths that must carry it (KEL-75).
type emailInvoiceBilling struct {
	marketplaceBilling
	requests []billing.InvoiceRequest
}

func (m *emailInvoiceBilling) GenerateInvoice(ctx context.Context, request billing.InvoiceRequest) (*billing.InvoiceResponse, error) {
	m.requests = append(m.requests, request)
	return m.marketplaceBilling.GenerateInvoice(ctx, request)
}

// The fresh checkout must forward the email claim as sender_email.
func TestEnrollPublicForwardsSenderEmailOnFreshEnrollment(t *testing.T) {
	parentID, studentID, classID, tenantID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	billingMock := &emailInvoiceBilling{}
	uc := NewEnrollmentUsecase(
		&marketplaceEnrollmentRepo{},
		&marketplaceStudentRepo{student: &domain.Student{ID: studentID, ParentID: parentID}},
		&marketplaceClassRepo{class: &domain.Class{ID: classID, TenantID: tenantID, Price: 250000, IsPublished: true, EnrollmentStatus: "open"}},
		billingMock, marketplaceTx{},
	)

	if _, err := uc.EnrollPublic(context.Background(), parentID, classID, &domain.PublicEnrollmentRequest{StudentID: studentID, BillingCycle: "monthly", SenderEmail: "parent@example.com", PaymentMethod: "SP"}, "kel75-fresh"); err != nil {
		t.Fatalf("EnrollPublic() error = %v", err)
	}
	if len(billingMock.requests) != 1 {
		t.Fatalf("invoice requests = %d, want 1", len(billingMock.requests))
	}
	if billingMock.requests[0].SenderEmail != "parent@example.com" || billingMock.requests[0].PaymentMethod != "SP" {
		t.Fatalf("invoice request = %+v, want email and SP", billingMock.requests[0])
	}
}

// The idempotent replay for the same pending enrollment regenerates the invoice
// and must carry the parent email too.
func TestEnrollPublicReplayForwardsSenderEmail(t *testing.T) {
	parentID, studentID, classID, tenantID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	// A pending enrollment without a transaction takes the regeneration path.
	existing := &domain.Enrollment{ID: uuid.New(), TenantID: tenantID, StudentID: studentID, ClassID: classID, BillingCycle: "monthly", PaymentStatus: "pending", GrossAmount: 100}
	repo := &marketplaceEnrollmentRepo{existing: existing}
	billingMock := &emailInvoiceBilling{}
	uc := NewEnrollmentUsecase(
		repo,
		&marketplaceStudentRepo{student: &domain.Student{ID: studentID, ParentID: parentID}},
		&marketplaceClassRepo{class: &domain.Class{ID: classID, TenantID: tenantID, Price: 250000, IsPublished: true, EnrollmentStatus: "open"}},
		billingMock, marketplaceTx{},
	)

	if _, err := uc.EnrollPublic(context.Background(), parentID, classID, &domain.PublicEnrollmentRequest{StudentID: studentID, BillingCycle: "monthly", SenderEmail: "replay@example.com", PaymentMethod: "VA"}, "kel75-replay"); err != nil {
		t.Fatalf("EnrollPublic() replay error = %v", err)
	}
	if len(billingMock.requests) != 1 {
		t.Fatalf("invoice requests = %d, want 1", len(billingMock.requests))
	}
	if billingMock.requests[0].SenderEmail != "replay@example.com" || billingMock.requests[0].PaymentMethod != "VA" {
		t.Fatalf("replay invoice = %+v, want email and VA", billingMock.requests[0])
	}
}

// Tenant-created enrollments have no verified parent token, so EnrollStudent must
// keep its current behaviour and never invent a sender email.
func TestEnrollStudentForwardsPaymentMethod(t *testing.T) {
	tenantID, studentID, classID := uuid.New(), uuid.New(), uuid.New()
	billingMock := &emailInvoiceBilling{}
	uc := NewEnrollmentUsecase(
		&marketplaceEnrollmentRepo{},
		&marketplaceStudentRepo{student: &domain.Student{ID: studentID, ParentID: uuid.New()}},
		&marketplaceClassRepo{class: &domain.Class{ID: classID, TenantID: tenantID, Price: 250000, IsPublished: true, EnrollmentStatus: "open"}},
		billingMock, marketplaceTx{},
	)
	if _, err := uc.EnrollStudent(context.Background(), tenantID, &domain.EnrollStudentRequest{StudentID: studentID, ClassID: classID, BillingCycle: "monthly", IdempotencyKey: "tenant-channel", PaymentMethod: "BC"}); err != nil {
		t.Fatal(err)
	}
	if len(billingMock.requests) != 1 || billingMock.requests[0].PaymentMethod != "BC" {
		t.Fatalf("invoice requests = %+v", billingMock.requests)
	}
}

func TestEnrollStudentDoesNotForwardSenderEmail(t *testing.T) {
	tenantID, studentID, classID := uuid.New(), uuid.New(), uuid.New()
	billingMock := &emailInvoiceBilling{}
	uc := NewEnrollmentUsecase(
		&marketplaceEnrollmentRepo{},
		&marketplaceStudentRepo{student: &domain.Student{ID: studentID, ParentID: uuid.New()}},
		&marketplaceClassRepo{class: &domain.Class{ID: classID, TenantID: tenantID, Price: 250000, IsPublished: true, EnrollmentStatus: "open"}},
		billingMock, marketplaceTx{},
	)

	if _, err := uc.EnrollStudent(context.Background(), tenantID, &domain.EnrollStudentRequest{StudentID: studentID, ClassID: classID, BillingCycle: "monthly", IdempotencyKey: "kel75-tenant"}); err != nil {
		t.Fatalf("EnrollStudent() error = %v", err)
	}
	if len(billingMock.requests) != 1 {
		t.Fatalf("invoice requests = %d, want 1", len(billingMock.requests))
	}
	if billingMock.requests[0].SenderEmail != "" || billingMock.requests[0].PaymentMethod != "" {
		t.Fatalf("tenant invoice = %+v, want legacy empty email and method", billingMock.requests[0])
	}
}
