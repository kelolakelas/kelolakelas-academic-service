package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

type previewBilling struct {
	billing.Client
	req  billing.VoucherPreviewRequest
	resp *billing.VoucherPreviewResponse
	err  error
}

func (b *previewBilling) PreviewVoucher(_ context.Context, req billing.VoucherPreviewRequest) (*billing.VoucherPreviewResponse, error) {
	b.req = req
	if b.err != nil {
		return nil, b.err
	}
	if b.resp != nil {
		return b.resp, nil
	}
	return &billing.VoucherPreviewResponse{DiscountAmount: 100, GrossAmount: 900}, nil
}

func newPreviewFixture(billingClient billing.Client, class *domain.Class) *feeFixture {
	f := &feeFixture{repo: newFeeEnrollmentRepo(), billing: &feeBilling{}, parentID: uuid.New(), studentID: uuid.New(), tenantID: uuid.New(), classID: class.ID, scheduleID: uuid.New()}
	f.repo.parents[f.studentID] = f.parentID
	f.repo.capacity[f.scheduleID] = 5
	f.uc = NewEnrollmentUsecase(f.repo, &marketplaceStudentRepo{student: &domain.Student{ID: f.studentID, ParentID: f.parentID}}, &marketplaceClassRepo{class: class}, billingClient, marketplaceTx{})
	return f
}

func previewOf(u EnrollmentUsecase) VoucherPreviewUsecase {
	p, ok := u.(VoucherPreviewUsecase)
	if !ok {
		panic("enrollment usecase does not implement VoucherPreviewUsecase")
	}
	return p
}

func TestPreviewVoucherUsesClassTenantAndPrice(t *testing.T) {
	class := &domain.Class{ID: uuid.New(), TenantID: uuid.New(), Type: "group", Price: 1000, IsPublished: true, EnrollmentStatus: "open"}
	b := &previewBilling{}
	f := newPreviewFixture(b, class)
	res, err := previewOf(f.uc).PreviewVoucher(context.Background(), f.parentID, class.ID, "HEMAT")
	if err != nil {
		t.Fatalf("PreviewVoucher() error = %v", err)
	}
	if res.DiscountAmount != 100 || res.GrossAmount != 900 {
		t.Fatalf("preview = %+v", res)
	}
	if b.req.TenantID != class.TenantID || b.req.SubtotalAmount != class.Price || b.req.VoucherCode != "HEMAT" {
		t.Fatalf("billing request = %+v, want tenant %v subtotal %d code HEMAT", b.req, class.TenantID, class.Price)
	}
}

func TestPreviewVoucherDoesNotReserveAndValidatesEligibility(t *testing.T) {
	for _, tt := range []struct {
		name         string
		parent       uuid.UUID
		published    bool
		status, code string
		want         error
	}{
		{"valid", uuid.New(), true, "open", "HEMAT", nil},
		{"parent required", uuid.Nil, true, "open", "HEMAT", domain.ErrParentRequired},
		{"unpublished", uuid.New(), false, "open", "HEMAT", domain.ErrClassNotEnrollable},
		{"closed", uuid.New(), true, "closed", "HEMAT", domain.ErrClassNotEnrollable},
		{"long code", uuid.New(), true, "open", strings.Repeat("X", 256), domain.ErrVoucherRejected},
	} {
		t.Run(tt.name, func(t *testing.T) {
			class := &domain.Class{ID: uuid.New(), TenantID: uuid.New(), Type: "group", Price: 1000, IsPublished: tt.published, EnrollmentStatus: tt.status}
			b := &previewBilling{}
			f := newPreviewFixture(b, class)
			_, err := previewOf(f.uc).PreviewVoucher(t.Context(), tt.parent, class.ID, tt.code)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v want=%v", err, tt.want)
			}
			if f.repo.count() != 0 || f.repo.liveSeats(f.scheduleID) != 0 {
				t.Fatal("preview created enrollment or reserved seat")
			}
			if tt.want != nil && b.req.TenantID != uuid.Nil {
				t.Fatal("invalid preview called billing")
			}
		})
	}
}

func TestPreviewVoucherRejection(t *testing.T) {
	class := &domain.Class{ID: uuid.New(), TenantID: uuid.New(), Type: "group", Price: 1000, IsPublished: true, EnrollmentStatus: "open"}
	f := newPreviewFixture(&previewBilling{err: billing.ErrVoucherRejected}, class)
	_, err := previewOf(f.uc).PreviewVoucher(context.Background(), f.parentID, class.ID, "X")
	if !errors.Is(err, domain.ErrVoucherRejected) {
		t.Fatalf("error = %v, want domain.ErrVoucherRejected", err)
	}
}

func TestPreviewVoucherPrivateClassRefused(t *testing.T) {
	class := &domain.Class{ID: uuid.New(), TenantID: uuid.New(), Type: "private", Price: 1000, IsPublished: true, EnrollmentStatus: "open"}
	f := newPreviewFixture(&previewBilling{}, class)
	if _, err := previewOf(f.uc).PreviewVoucher(context.Background(), f.parentID, class.ID, "X"); !errors.Is(err, domain.ErrPrivateCheckout) {
		t.Fatalf("error = %v, want ErrPrivateCheckout", err)
	}
}

func TestPreviewVoucherEmptyCodeRejected(t *testing.T) {
	class := &domain.Class{ID: uuid.New(), TenantID: uuid.New(), Type: "group", Price: 1000, IsPublished: true, EnrollmentStatus: "open"}
	f := newPreviewFixture(&previewBilling{}, class)
	if _, err := previewOf(f.uc).PreviewVoucher(context.Background(), f.parentID, class.ID, "  "); !errors.Is(err, domain.ErrVoucherRejected) {
		t.Fatalf("error = %v, want ErrVoucherRejected", err)
	}
}
