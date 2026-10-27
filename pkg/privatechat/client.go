package privatechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("private chat dependency unavailable")
var ErrMissingInvoice = errors.New("private invoice not found")

type InvoiceScope struct {
	TransactionID uuid.UUID `json:"transaction_id"`
	EnrollmentID  uuid.UUID `json:"enrollment_id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	ParentID      uuid.UUID `json:"parent_id"`
	StudentID     uuid.UUID `json:"student_id"`
}
type Invoice struct {
	ID                 uuid.UUID  `json:"id"`
	EnrollmentID       uuid.UUID  `json:"enrollment_id"`
	TenantID           uuid.UUID  `json:"tenant_id"`
	ParentID           uuid.UUID  `json:"parent_id"`
	StudentID          uuid.UUID  `json:"student_id"`
	Status             string     `json:"status"`
	CheckoutSessionURL string     `json:"checkout_session_url"`
	InvoiceExpiresAt   *time.Time `json:"invoice_expires_at"`
}
type Delivery struct {
	EventID   uuid.UUID `json:"event_id"`
	RequestID uuid.UUID `json:"request_id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	ParentID  uuid.UUID `json:"parent_id"`
}
type Client struct {
	BillingURL, ChatURL, Credential string
	HTTP                            *http.Client
}

func New(billingURL, chatURL, credential string) *Client {
	return &Client{strings.TrimRight(billingURL, "/"), strings.TrimRight(chatURL, "/"), credential, &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) post(ctx context.Context, url string, body, out any) error {
	if strings.TrimSpace(c.Credential) == "" {
		return ErrUnavailable
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Service-Credential", c.Credential)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 && out != nil {
		return ErrMissingInvoice
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return ErrUnavailable
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 65536))
	if decoder.Decode(&envelope) != nil || envelope.Status != "success" || len(envelope.Data) == 0 || string(envelope.Data) == "null" || decoder.Decode(new(any)) != io.EOF {
		return ErrUnavailable
	}
	if out != nil && json.Unmarshal(envelope.Data, out) != nil {
		return ErrUnavailable
	}
	return nil
}
func (c *Client) Invoice(ctx context.Context, scope InvoiceScope) (*Invoice, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result Invoice
	if err := c.post(ctx, c.BillingURL+"/internal/billing/private-request-invoice", scope, &result); err != nil {
		return nil, err
	}
	if result.ID != scope.TransactionID || result.EnrollmentID != scope.EnrollmentID || result.TenantID != scope.TenantID || result.ParentID != scope.ParentID || result.StudentID != scope.StudentID {
		return nil, ErrUnavailable
	}
	return &result, nil
}
func (c *Client) Deliver(ctx context.Context, d Delivery) error {
	var result struct {
		ID         uuid.UUID `json:"id"`
		SenderKind string    `json:"sender_kind"`
	}
	if err := c.post(ctx, c.ChatURL+"/internal/private-request-events", d, &result); err != nil {
		return err
	}
	if result.ID == uuid.Nil || (result.SenderKind != "parent" && result.SenderKind != "member") {
		return ErrUnavailable
	}
	return nil
}
