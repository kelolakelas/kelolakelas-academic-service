package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Client interface {
	GenerateInvoice(ctx context.Context, request InvoiceRequest) (*InvoiceResponse, error)
	// CancelEnrollmentPayment withdraws the unpaid invoice of an enrollment the parent
	// cancelled. The operation is idempotent on the billing side, so a retry after a
	// partial failure is safe.
	CancelEnrollmentPayment(ctx context.Context, enrollmentID uuid.UUID) (*CancelResponse, error)
}

type InvoiceRequest struct {
	TenantID       uuid.UUID `json:"tenant_id"`
	StudentID      uuid.UUID `json:"student_id"`
	ClassID        uuid.UUID `json:"class_id"`
	EnrollmentID   uuid.UUID `json:"enrollment_id"`
	ParentID       uuid.UUID `json:"parent_id"`
	BillingCycle   string    `json:"billing_cycle"`
	SubtotalAmount int64     `json:"subtotal_amount"`
	DiscountAmount int64     `json:"discount_amount"`
	PlatformFee    int64     `json:"platform_fee"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Title          string    `json:"title"`
}

type InvoiceResponse struct {
	TransactionID      uuid.UUID `json:"transaction_id"`
	CheckoutSessionURL string    `json:"checkout_session_url"`
	PaymentIntentID    string    `json:"payment_intent_id"`
}

type CancelRequest struct {
	EnrollmentID uuid.UUID `json:"enrollment_id"`
}

// CancelResponse is the transaction that the billing service cancelled. Only the
// fields the academic service needs are read, so an unexpected billing payload
// cannot change the enrollment outcome silently.
type CancelResponse struct {
	TransactionID uuid.UUID `json:"id"`
	Status        string    `json:"status"`
}

type client struct {
	baseURL    string
	credential string
	httpClient *http.Client
}

func NewClient(baseURL, credential string) Client {
	return &client{baseURL: strings.TrimRight(baseURL, "/"), credential: credential, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (c *client) GenerateInvoice(ctx context.Context, request InvoiceRequest) (*InvoiceResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal billing request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/billing/transactions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create billing request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Service-Credential", c.credential)
	if request.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", request.IdempotencyKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call billing service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil || envelope.Message == "" {
			return nil, fmt.Errorf("billing service returned status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("billing service returned status %d: %s", resp.StatusCode, redactCredential(envelope.Message, c.credential))
	}
	var envelope struct {
		Data InvoiceResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode billing response: %w", err)
	}
	return &envelope.Data, nil
}

// Sentinel errors let the enrollment use case distinguish "this enrollment has no
// invoice to withdraw" from "the invoice can no longer be withdrawn". Both are
// terminal for the request, but only the second means money may already have moved,
// so the parent must not be told the seat was cancelled.
var (
	ErrTransactionNotFound       = errors.New("billing transaction not found")
	ErrTransactionNotCancellable = errors.New("billing transaction can no longer be cancelled")
)

// CancelEnrollmentPayment withdraws the unpaid invoice that belongs to an
// enrollment. Billing answers 404 when the enrollment has no transaction at all and
// 409 when the transaction already settled; both are surfaced as sentinel errors so
// the caller does not have to parse messages.
func (c *client) CancelEnrollmentPayment(ctx context.Context, enrollmentID uuid.UUID) (*CancelResponse, error) {
	body, err := json.Marshal(CancelRequest{EnrollmentID: enrollmentID})
	if err != nil {
		return nil, fmt.Errorf("marshal billing cancellation request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/billing/transactions/cancel", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create billing cancellation request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Service-Credential", c.credential)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call billing service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrTransactionNotFound
	}
	if resp.StatusCode == http.StatusConflict {
		return nil, ErrTransactionNotCancellable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil || envelope.Message == "" {
			return nil, fmt.Errorf("billing service returned status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("billing service returned status %d: %s", resp.StatusCode, redactCredential(envelope.Message, c.credential))
	}
	var envelope struct {
		Data CancelResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode billing cancellation response: %w", err)
	}
	return &envelope.Data, nil
}

func redactCredential(message, credential string) string {
	if credential == "" {
		return message
	}
	return strings.ReplaceAll(message, credential, "[redacted]")
}
