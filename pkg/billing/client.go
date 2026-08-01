package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	Title          string    `json:"title"`
}

type InvoiceResponse struct {
	TransactionID      uuid.UUID `json:"transaction_id"`
	CheckoutSessionURL string    `json:"checkout_session_url"`
	PaymentIntentID    string    `json:"payment_intent_id"`
}

type client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) Client {
	return &client{baseURL: strings.TrimRight(baseURL, "/"), httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (c *client) GenerateInvoice(ctx context.Context, request InvoiceRequest) (*InvoiceResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal billing request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/billing/transactions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create billing request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call billing service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("billing service returned status %d", resp.StatusCode)
	}
	var envelope struct {
		Data InvoiceResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode billing response: %w", err)
	}
	return &envelope.Data, nil
}
