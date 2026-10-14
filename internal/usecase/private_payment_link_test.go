package usecase

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/paymentlink"
	"testing"
	"time"
)

type linkRequests struct {
	repository.PrivateScheduleRequestRepository
	r      *domain.PrivateScheduleRequest
	tenant uuid.UUID
}

func (m *linkRequests) Get(_ context.Context, id uuid.UUID, tenant, parent *uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	if tenant == nil || *tenant != m.tenant || parent != nil || id != m.r.ID {
		return nil, domain.ErrPrivateRequestNotFound
	}
	return m.r, nil
}

type linkEnrollments struct {
	repository.EnrollmentRepository
	e      *domain.Enrollment
	parent uuid.UUID
	key    string
}

func (m *linkEnrollments) GetByIdempotencyKey(_ context.Context, parent uuid.UUID, key string) (*domain.Enrollment, error) {
	if parent != m.parent || key != m.key {
		panic("unscoped enrollment lookup")
	}
	return m.e, nil
}

type linkClient struct {
	transaction     *paymentlink.Transaction
	reads, sends    int
	getErr, sendErr error
	inputs          []paymentlink.Input
}

func (m *linkClient) GetTransaction(_ context.Context, id uuid.UUID, auth string) (*paymentlink.Transaction, error) {
	m.reads++
	if id != m.transaction.ID || auth != "Bearer verified" {
		panic("wrong billing contract")
	}
	return m.transaction, m.getErr
}
func (m *linkClient) Send(_ context.Context, input paymentlink.Input) (*paymentlink.Message, error) {
	m.sends++
	m.inputs = append(m.inputs, input)
	return &paymentlink.Message{ID: input.TransactionID, ConversationID: input.RequestID}, m.sendErr
}
func linkFixture() (*privatePaymentLinkUsecase, *linkClient, *domain.PrivateScheduleRequest, *domain.Enrollment) {
	r := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: uuid.New(), ParentID: uuid.New(), StudentID: uuid.New(), ClassID: uuid.New(), Status: "approved"}
	key := "private-request:" + r.ID.String()
	tx := uuid.New()
	expiry := time.Now().Add(time.Hour)
	e := &domain.Enrollment{ID: uuid.New(), TenantID: r.TenantID, StudentID: r.StudentID, ClassID: r.ClassID, Status: "pending", IdempotencyKey: &key, PaymentTransactionID: &tx}
	c := &linkClient{transaction: &paymentlink.Transaction{ID: tx, TenantID: r.TenantID, ParentID: r.ParentID, StudentID: r.StudentID, EnrollmentID: e.ID, Status: "pending", CheckoutSessionURL: "https://checkout.example/pay", InvoiceExpiresAt: &expiry}}
	return &privatePaymentLinkUsecase{&linkRequests{r: r, tenant: r.TenantID}, &linkEnrollments{e: e, parent: r.ParentID, key: key}, c}, c, r, e
}
func TestPrivatePaymentLinkEligibilityAndIsolation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.PrivateScheduleRequest, *domain.Enrollment, *paymentlink.Transaction)
		want   error
	}{
		{"unapproved", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			r.Status = "pending"
		}, ErrPaymentLinkInactive},
		{"dropped", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			e.Status = "dropped"
		}, ErrPaymentLinkInactive},
		{"no transaction", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			e.PaymentTransactionID = nil
		}, ErrPaymentLinkInactive},
		{"enrollment tenant", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			e.TenantID = uuid.New()
		}, domain.ErrPrivateRequestNotFound},
		{"enrollment student", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			e.StudentID = uuid.New()
		}, domain.ErrPrivateRequestNotFound},
		{"enrollment class", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			e.ClassID = uuid.New()
		}, domain.ErrPrivateRequestNotFound},
		{"key", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			e.IdempotencyKey = nil
		}, domain.ErrPrivateRequestNotFound},
		{"billing tenant", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			b.TenantID = uuid.New()
		}, paymentlink.ErrUnavailable},
		{"billing parent", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			b.ParentID = uuid.New()
		}, paymentlink.ErrUnavailable},
		{"billing student", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			b.StudentID = uuid.New()
		}, paymentlink.ErrUnavailable},
		{"billing enrollment", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			b.EnrollmentID = uuid.New()
		}, paymentlink.ErrUnavailable},
		{"no expiry", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			b.InvoiceExpiresAt = nil
		}, ErrPaymentLinkInactive},
		{"expired", func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			past := time.Now().Add(-time.Second)
			b.InvoiceExpiresAt = &past
		}, ErrPaymentLinkInactive},
	}
	for _, status := range []string{"paid", "failed", "expired", "cancelled", "refunded", "creating", "unknown", ""} {
		s := status
		cases = append(cases, struct {
			name   string
			change func(*domain.PrivateScheduleRequest, *domain.Enrollment, *paymentlink.Transaction)
			want   error
		}{"status " + s, func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) { b.Status = s }, ErrPaymentLinkInactive})
	}
	for _, url := range []string{"", "javascript:alert(1)", "https://user:pass@example.com", "/relative", "https://", " https://example.com", "https://example.com\n", "https://%zz"} {
		v := url
		cases = append(cases, struct {
			name   string
			change func(*domain.PrivateScheduleRequest, *domain.Enrollment, *paymentlink.Transaction)
			want   error
		}{"URL " + v, func(r *domain.PrivateScheduleRequest, e *domain.Enrollment, b *paymentlink.Transaction) {
			b.CheckoutSessionURL = v
		}, ErrPaymentLinkInactive})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, c, r, e := linkFixture()
			tc.change(r, e, c.transaction)
			_, err := u.SendPaymentLink(context.Background(), r.TenantID, r.ID, "Bearer verified")
			if !errors.Is(err, tc.want) || c.sends != 0 {
				t.Fatalf("err=%v sends=%d", err, c.sends)
			}
		})
	}
	u, c, r, _ := linkFixture()
	_, err := u.SendPaymentLink(context.Background(), uuid.New(), r.ID, "Bearer verified")
	if !errors.Is(err, domain.ErrPrivateRequestNotFound) || c.reads != 0 || c.sends != 0 {
		t.Fatalf("foreign tenant: %v", err)
	}
}
func TestPrivatePaymentLinkRetryReadsCurrentInvoice(t *testing.T) {
	u, c, r, _ := linkFixture()
	ctx := context.Background()
	c.sendErr = errors.New("lost response")
	if _, err := u.SendPaymentLink(ctx, r.TenantID, r.ID, "Bearer verified"); !errors.Is(err, paymentlink.ErrUnavailable) {
		t.Fatal(err)
	}
	c.sendErr = nil
	for i := 0; i < 2; i++ {
		if _, err := u.SendPaymentLink(ctx, r.TenantID, r.ID, "Bearer verified"); err != nil {
			t.Fatal(err)
		}
	}
	if c.reads != 3 || c.sends != 3 || c.inputs[0] != c.inputs[1] || c.inputs[1] != c.inputs[2] {
		t.Fatalf("retry contract: %+v", c)
	}
	c.transaction.Status = "paid"
	if _, err := u.SendPaymentLink(ctx, r.TenantID, r.ID, "Bearer verified"); !errors.Is(err, ErrPaymentLinkInactive) || c.sends != 3 {
		t.Fatalf("paid retry: %v", err)
	}
	c.getErr = errors.New("billing unavailable")
	if _, err := u.SendPaymentLink(ctx, r.TenantID, r.ID, "Bearer verified"); !errors.Is(err, paymentlink.ErrUnavailable) || c.sends != 3 {
		t.Fatalf("upstream: %v", err)
	}
	// Embedded nil repository methods panic if the send path ever creates an
	// enrollment, updates payment metadata, or invokes purchase/invoice generation.
}
