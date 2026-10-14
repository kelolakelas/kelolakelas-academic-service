// Package paymentlink implements the read-only billing and internal chat contracts.
package paymentlink

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

var ErrUnavailable = errors.New("Payment services are unavailable")
var ErrInactive = errors.New("Payment link is inactive")

type Transaction struct {
	ID                 uuid.UUID  `json:"id"`
	TenantID           uuid.UUID  `json:"tenant_id"`
	ParentID           uuid.UUID  `json:"parent_id"`
	StudentID          uuid.UUID  `json:"student_id"`
	EnrollmentID       uuid.UUID  `json:"enrollment_id"`
	Status             string     `json:"status"`
	CheckoutSessionURL string     `json:"checkout_session_url"`
	InvoiceExpiresAt   *time.Time `json:"invoice_expires_at"`
}
type Input struct {
	InvoiceExpiresAt   time.Time `json:"invoice_expires_at"`
	RequestID          uuid.UUID `json:"request_id"`
	TenantID           uuid.UUID `json:"tenant_id"`
	ParentUserID       uuid.UUID `json:"parent_user_id"`
	TransactionID      uuid.UUID `json:"transaction_id"`
	CheckoutSessionURL string    `json:"checkout_session_url"`
}
type Message struct {
	ID              uuid.UUID  `json:"id"`
	ConversationID  uuid.UUID  `json:"conversation_id"`
	SenderUserID    *uuid.UUID `json:"sender_user_id"`
	SenderKind      string     `json:"sender_kind"`
	Body            string     `json:"body"`
	ClientMessageID string     `json:"client_message_id"`
	CreatedAt       time.Time  `json:"created_at"`
}
type Client interface {
	GetTransaction(context.Context, uuid.UUID, string) (*Transaction, error)
	Send(context.Context, Input) (*Message, error)
}
type client struct {
	billingURL, chatURL, credential string
	http                            *http.Client
}

func NewClient(billingURL, chatURL, credential string) Client {
	return &client{strings.TrimRight(billingURL, "/"), strings.TrimRight(chatURL, "/"), credential, &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *client) GetTransaction(ctx context.Context, id uuid.UUID, authorization string) (*Transaction, error) {
	if !strings.HasPrefix(authorization, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")) == "" {
		return nil, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.billingURL+"/api/v1/billing/transactions/"+id.String(), nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Authorization", authorization)
	var result Transaction
	if err = c.call(req, &result, false); err != nil {
		return nil, err
	}
	return &result, nil
}
func (c *client) Send(ctx context.Context, input Input) (*Message, error) {
	if strings.TrimSpace(c.credential) == "" {
		return nil, ErrUnavailable
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL+"/internal/schedule-request-payment-links", bytes.NewReader(body))
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("X-Internal-Service-Credential", c.credential)
	req.Header.Set("Content-Type", "application/json")
	var result Message
	if err = c.call(req, &result, true); err != nil {
		return nil, err
	}
	if result.ID == uuid.Nil || result.ConversationID == uuid.Nil || result.SenderKind != "system" {
		return nil, ErrUnavailable
	}
	return &result, nil
}
func (c *client) call(req *http.Request, out any, created bool) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if created && resp.StatusCode == http.StatusConflict {
		return ErrInactive
	}
	if resp.StatusCode != http.StatusOK && !(created && resp.StatusCode == http.StatusCreated) {
		return ErrUnavailable
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if decoder.Decode(&envelope) != nil || envelope.Status != "success" || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return ErrUnavailable
	}
	if decoder.Decode(new(any)) != io.EOF {
		return ErrUnavailable
	}
	if json.Unmarshal(envelope.Data, out) != nil {
		return ErrUnavailable
	}
	return nil
}
