package paymentlink

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExactPaymentLinkHTTPContracts(t *testing.T) {
	id := uuid.New()
	input := Input{InvoiceExpiresAt: time.Now().UTC().Add(time.Hour), RequestID: uuid.New(), TenantID: uuid.New(), ParentUserID: uuid.New(), TransactionID: id, CheckoutSessionURL: "https://checkout.example/payment"}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/api/v1/billing/transactions/" + id.String():
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer verified" || r.Header.Get("X-Internal-Service-Credential") != "" || r.ContentLength != 0 {
				t.Errorf("billing contract: %+v", r)
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": Transaction{ID: id}})
		case "/internal/schedule-request-payment-links":
			if r.Method != "POST" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Internal-Service-Credential") != "server-only" || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("chat headers: %v", r.Header)
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if len(body) != 6 {
				t.Errorf("unexpected fields: %v", body)
			}
			raw, _ := json.Marshal(body)
			var got Input
			json.Unmarshal(raw, &got)
			if got != input {
				t.Errorf("input: %+v", got)
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": Message{ID: id, ConversationID: input.RequestID, SenderKind: "system", Body: "link"}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, server.URL, "server-only")
	if tx, err := client.GetTransaction(context.Background(), id, "Bearer verified"); err != nil || tx.ID != id {
		t.Fatalf("transaction=%+v error=%v", tx, err)
	}
	if m, err := client.Send(context.Background(), input); err != nil || m.ConversationID != input.RequestID {
		t.Fatalf("message=%+v error=%v", m, err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestPaymentLinkClientFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		code int
		body string
	}{{403, `{"status":"error"}`}, {404, `{}`}, {503, `{}`}, {200, `broken`}, {200, `{"status":"success","data":null}`}, {200, `{"status":"error","data":{}}`}, {200, `{"status":"success","data":{}} {}`}, {200, `{"status":"success","data":{"id":"invalid"}}`}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); fmt.Fprint(w, tc.body) }))
			defer s.Close()
			c := NewClient(s.URL, s.URL, "credential")
			if _, err := c.GetTransaction(context.Background(), uuid.New(), "Bearer verified"); err != ErrUnavailable {
				t.Fatal(err)
			}
			if _, err := c.Send(context.Background(), Input{}); err != ErrUnavailable {
				t.Fatal(err)
			}
		})
	}
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	c := NewClient(redirect.URL, redirect.URL, "credential")
	c.GetTransaction(context.Background(), uuid.New(), "Bearer verified")
	c.Send(context.Background(), Input{})
	if targetCalls != 0 {
		t.Fatal("credentials followed redirect")
	}
	if _, err := c.GetTransaction(context.Background(), uuid.New(), ""); err != ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := NewClient(target.URL, target.URL, "").Send(context.Background(), Input{}); err != ErrUnavailable || targetCalls != 0 {
		t.Fatal("missing credential not fail closed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GetTransaction(ctx, uuid.New(), "Bearer verified"); err != ErrUnavailable {
		t.Fatal(err)
	}
}
