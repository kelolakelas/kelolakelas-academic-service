package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

type voucherInvoiceBilling struct {
	billing.Client
	requests      []billing.InvoiceRequest
	err           error
	transactionID uuid.UUID
}

func (b *voucherInvoiceBilling) GenerateInvoice(_ context.Context, req billing.InvoiceRequest) (*billing.InvoiceResponse, error) {
	b.requests = append(b.requests, req)
	if b.err != nil {
		return nil, b.err
	}
	return &billing.InvoiceResponse{TransactionID: b.transactionID, CheckoutSessionURL: "https://checkout.test", GrossAmount: 75, DiscountAmount: 25}, nil
}

func TestVoucherInvoiceSnapshotAndRetry(t *testing.T) {
	f := newFeeFixture(1)
	class := &domain.Class{ID: f.classID, TenantID: f.tenantID, Type: "group", Price: 100, IsPublished: true, EnrollmentStatus: "open"}
	b := &voucherInvoiceBilling{transactionID: uuid.New(), err: errors.New("temporary billing outage")}
	u := NewEnrollmentUsecase(f.repo, &marketplaceStudentRepo{student: &domain.Student{ID: f.studentID, ParentID: f.parentID}}, &marketplaceClassRepo{class: class}, b, marketplaceTx{})
	req := &domain.PublicEnrollmentRequest{StudentID: f.studentID, BillingCycle: "monthly", ScheduleID: &f.scheduleID, VoucherCode: "HEMAT"}
	if _, err := u.EnrollPublic(t.Context(), f.parentID, f.classID, req, "snapshot"); err == nil {
		t.Fatal("expected transient failure")
	}
	b.err = nil
	result, err := u.EnrollPublic(t.Context(), f.parentID, f.classID, req, "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if result.Payment.GrossAmount != 75 || len(b.requests) != 2 || f.repo.count() != 1 {
		t.Fatalf("result=%+v requests=%+v rows=%d", result, b.requests, f.repo.count())
	}
	for _, request := range b.requests {
		if request.TenantID != f.tenantID || request.SubtotalAmount != 100 || request.VoucherCode != "HEMAT" {
			t.Fatalf("request=%+v", request)
		}
	}
	class.Price = 999
	req.VoucherCode = "OTHER"
	replay, err := u.EnrollPublic(t.Context(), f.parentID, f.classID, req, "snapshot")
	if err != nil || replay.Payment.GrossAmount != 75 || replay.Payment.TransactionID != b.transactionID || len(b.requests) != 2 {
		t.Fatalf("replay=%+v err=%v calls=%d", replay, err, len(b.requests))
	}
}

func TestVoucherReplacementInvoiceRejectionDropsRecoveredAttempt(t *testing.T) {
	f := newFeeFixture(1)
	f.billing.err = errors.New("billing temporarily unavailable")
	if err := f.enrollPublic("replacement"); err == nil {
		t.Fatal("expected transient invoice error")
	}
	f.billing.err = billing.ErrVoucherRejected
	if err := f.enrollPublic("replacement"); !errors.Is(err, domain.ErrVoucherRejected) {
		t.Fatalf("error=%v", err)
	}
	row, err := f.repo.GetByIdempotencyKeyAny(t.Context(), "replacement")
	if err != nil || row.Status != "dropped" || row.PaymentStatus != domain.PaymentStatusVoucherRejected || f.repo.liveSeats(f.scheduleID) != 0 {
		t.Fatalf("row=%+v err=%v", row, err)
	}
	if err := f.enrollPublic("replacement"); !errors.Is(err, domain.ErrVoucherRejected) || f.billing.calls != 2 {
		t.Fatalf("replay error=%v calls=%d", err, f.billing.calls)
	}
}

func TestVoucherRejectionReleaseFailureIsNotValidationError(t *testing.T) {
	f := newFeeFixture(1)
	f.billing.err = billing.ErrVoucherRejected
	f.repo.updateErr = errors.New("database unavailable")
	if err := f.enrollPublic("release-failure"); err == nil || errors.Is(err, domain.ErrVoucherRejected) {
		t.Fatalf("error=%v", err)
	}
	if f.repo.liveSeats(f.scheduleID) != 1 {
		t.Fatal("failed database release must not pretend to have freed seat")
	}
}

// TestVoucherRejectionReleasesTheAttempt mirrors the platform fee rejection
// contract for billing's voucher_rejected 422 (KEL-162): the enrollment is
// dropped so it holds no seat, its payment status records the reason, and a
// same-key replay answers the rejection instead of retrying the invoice.
func TestVoucherRejectionReleasesTheAttempt(t *testing.T) {
	for name, enroll := range map[string]func(*feeFixture, string) error{
		"parent checkout":   (*feeFixture).enrollPublic,
		"tenant enrollment": (*feeFixture).enrollTenant,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFeeFixture(1)
			f.billing.err = billing.ErrVoucherRejected
			if err := enroll(f, "voucher-key"); !errors.Is(err, domain.ErrVoucherRejected) {
				t.Fatalf("rejected attempt = %v, want ErrVoucherRejected", err)
			}
			rejected, err := f.repo.GetByIdempotencyKeyAny(context.Background(), "voucher-key")
			if err != nil {
				t.Fatal(err)
			}
			if rejected.Status != "dropped" || rejected.PaymentStatus != domain.PaymentStatusVoucherRejected || rejected.PaymentTransactionID != nil {
				t.Fatalf("rejected enrollment = status %q payment %q transaction %v, want dropped/%s/nil", rejected.Status, rejected.PaymentStatus, rejected.PaymentTransactionID, domain.PaymentStatusVoucherRejected)
			}
			if seats := f.repo.liveSeats(f.scheduleID); seats != 0 {
				t.Fatalf("seats held after rejection = %d, want 0", seats)
			}

			if err := enroll(f, "voucher-key"); !errors.Is(err, domain.ErrVoucherRejected) {
				t.Fatalf("same-key replay = %v, want ErrVoucherRejected", err)
			}
			if f.billing.calls != 1 || f.repo.count() != 1 {
				t.Fatalf("after replay: billing calls = %d, enrollments = %d, want 1 and 1", f.billing.calls, f.repo.count())
			}

			// A new checkout without the voucher must still be able to take the seat.
			f.billing.err = nil
			if err := enroll(f, "fresh-key"); err != nil {
				t.Fatalf("fresh attempt after voucher rejection = %v", err)
			}
		})
	}
}
