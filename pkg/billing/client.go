package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Client interface {
	GenerateInvoice(ctx context.Context, request InvoiceRequest) (*InvoiceResponse, error)
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

func redactCredential(message, credential string) string {
	if credential == "" {
		return message
	}
	return strings.ReplaceAll(message, credential, "[redacted]")
}
