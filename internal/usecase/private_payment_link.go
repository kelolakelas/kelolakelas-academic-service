package usecase

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/paymentlink"
	"net/url"
	"strings"
	"time"
)

var ErrPaymentLinkInactive = errors.New("Payment link is inactive or unavailable; an existing pending invoice with a valid checkout URL is required")

type PrivatePaymentLinkUsecase interface {
	SendPaymentLink(context.Context, uuid.UUID, uuid.UUID, string) (*paymentlink.Message, error)
}
type privatePaymentLinkUsecase struct {
	requests    repository.PrivateScheduleRequestRepository
	enrollments repository.EnrollmentRepository
	client      paymentlink.Client
}

func NewPrivatePaymentLinkUsecase(requests repository.PrivateScheduleRequestRepository, enrollments repository.EnrollmentRepository, client paymentlink.Client) PrivatePaymentLinkUsecase {
	return &privatePaymentLinkUsecase{requests, enrollments, client}
}

// SendPaymentLink never creates or mutates an invoice or enrollment. Every retry
// reads current billing eligibility before delegating atomic idempotency to chat.
func (u *privatePaymentLinkUsecase) SendPaymentLink(ctx context.Context, tenant, id uuid.UUID, authorization string) (*paymentlink.Message, error) {
	if tenant == uuid.Nil || id == uuid.Nil {
		return nil, domain.ErrPrivateRequestNotFound
	}
	request, err := u.requests.Get(ctx, id, &tenant, nil)
	if err != nil {
		return nil, err
	}
	if request == nil || request.ID != id || request.TenantID != tenant || request.ParentID == uuid.Nil || request.StudentID == uuid.Nil {
		return nil, domain.ErrPrivateRequestNotFound
	}
	if request.Status != "approved" {
		return nil, ErrPaymentLinkInactive
	}
	key := "private-request:" + id.String()
	enrollment, err := u.enrollments.GetByIdempotencyKey(ctx, request.ParentID, key)
	if err != nil {
		return nil, err
	}
	if enrollment == nil || enrollment.ID == uuid.Nil || enrollment.TenantID != tenant || enrollment.StudentID != request.StudentID || enrollment.ClassID != request.ClassID || enrollment.IdempotencyKey == nil || *enrollment.IdempotencyKey != key {
		return nil, domain.ErrPrivateRequestNotFound
	}
	if enrollment.Status != "pending" || enrollment.PaymentTransactionID == nil || *enrollment.PaymentTransactionID == uuid.Nil {
		return nil, ErrPaymentLinkInactive
	}
	if u.client == nil {
		return nil, paymentlink.ErrUnavailable
	}
	transaction, err := u.client.GetTransaction(ctx, *enrollment.PaymentTransactionID, authorization)
	if err != nil {
		return nil, paymentlink.ErrUnavailable
	}
	if transaction == nil || transaction.ID != *enrollment.PaymentTransactionID || transaction.TenantID != tenant || transaction.ParentID != request.ParentID || transaction.StudentID != request.StudentID || transaction.EnrollmentID != enrollment.ID {
		return nil, paymentlink.ErrUnavailable
	}
	checkout := transaction.CheckoutSessionURL
	parsed, parseErr := url.Parse(checkout)
	if transaction.Status != "pending" || transaction.InvoiceExpiresAt == nil || !transaction.InvoiceExpiresAt.After(time.Now()) || parseErr != nil || parsed == nil || len(checkout) > 1800 || strings.TrimSpace(checkout) != checkout || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" {
		return nil, ErrPaymentLinkInactive
	}
	message, err := u.client.Send(ctx, paymentlink.Input{RequestID: id, TenantID: tenant, ParentUserID: request.ParentID, TransactionID: transaction.ID, CheckoutSessionURL: checkout, InvoiceExpiresAt: *transaction.InvoiceExpiresAt})
	if errors.Is(err, paymentlink.ErrInactive) {
		return nil, ErrPaymentLinkInactive
	}
	if err != nil {
		return nil, paymentlink.ErrUnavailable
	}
	return message, nil
}
