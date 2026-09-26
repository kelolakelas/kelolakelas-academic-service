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

	if _, err := uc.EnrollPublic(context.Background(), parentID, classID, &domain.PublicEnrollmentRequest{StudentID: studentID, BillingCycle: "monthly", SenderEmail: "parent@example.com"}, "kel75-fresh"); err != nil {
		t.Fatalf("EnrollPublic() error = %v", err)
	}
	if len(billingMock.requests) != 1 {
		t.Fatalf("invoice requests = %d, want 1", len(billingMock.requests))
	}
	if billingMock.requests[0].SenderEmail != "parent@example.com" {
		t.Fatalf("sender_email = %q, want parent@example.com", billingMock.requests[0].SenderEmail)
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

	if _, err := uc.EnrollPublic(context.Background(), parentID, classID, &domain.PublicEnrollmentRequest{StudentID: studentID, BillingCycle: "monthly", SenderEmail: "replay@example.com"}, "kel75-replay"); err != nil {
		t.Fatalf("EnrollPublic() replay error = %v", err)
	}
	if len(billingMock.requests) != 1 {
		t.Fatalf("invoice requests = %d, want 1", len(billingMock.requests))
	}
	if billingMock.requests[0].SenderEmail != "replay@example.com" {
		t.Fatalf("sender_email = %q, want replay@example.com on the regenerated invoice", billingMock.requests[0].SenderEmail)
	}
}

// Tenant-created enrollments have no verified parent token, so EnrollStudent must
// keep its current behaviour and never invent a sender email.
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
	if billingMock.requests[0].SenderEmail != "" {
		t.Fatalf("sender_email = %q, want empty for the tenant-created path", billingMock.requests[0].SenderEmail)
	}
}
