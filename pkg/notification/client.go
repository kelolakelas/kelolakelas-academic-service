// Package notification sends immutable outbox snapshots to chat and Resend.
package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

var ErrUnavailable = errors.New("notification delivery unconfirmed")

type Client struct {
	chatURL, resendURL, credential, apiKey string
	http                                   *http.Client
}

func NewClient(chatURL, credential, apiKey string) *Client {
	return &Client{chatURL: strings.TrimRight(chatURL, "/"), resendURL: "https://api.resend.com/emails", credential: credential, apiKey: apiKey,
		http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) Send(ctx context.Context, n *repository.ParentNotification) error {
	key := "academic-notification/" + n.ID.String()
	endpoint := c.chatURL + "/internal/notifications"
	var body any
	switch n.Channel {
	case "chat":
		if c.chatURL == "" || c.credential == "" {
			return ErrUnavailable
		}
		body = map[string]any{"tenant_id": n.TenantID, "parent_user_id": n.ParentID, "body": n.Body, "idempotency_key": key}
	case "email":
		if c.apiKey == "" || n.SenderEmail == "" || n.RecipientEmail == "" {
			return ErrUnavailable
		}
		endpoint = c.resendURL
		body = map[string]any{"from": n.SenderEmail, "to": []string{n.RecipientEmail}, "subject": "Pembaruan sesi KelolaKelas", "text": n.Body}
	default:
		return ErrUnavailable
	}
	data, err := json.Marshal(body)
	if err != nil {
		return ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	if n.Channel == "chat" {
		req.Header.Set("X-Internal-Service-Credential", c.credential)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return ErrUnavailable
	}
	// A successful HTTP status without a provider message id is uncertain too.
	var result struct {
		ID   string `json:"id"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result) != nil {
		return ErrUnavailable
	}
	if n.Channel == "chat" {
		if result.Data.ID == "" {
			return ErrUnavailable
		}
	} else if result.ID == "" {
		return ErrUnavailable
	}
	return nil
}
