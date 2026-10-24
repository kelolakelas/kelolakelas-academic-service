package billing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGenerateInvoiceUsesInternalCredentialAndIdempotencyKey(t *testing.T) {
	credential := "academic-billing-secret"
	enrollmentID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/billing/transactions" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content type=%q", got)
		}
		if got := r.Header.Get("X-Internal-Service-Credential"); got != credential {
			t.Fatalf("credential=%q", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "same-key" {
			t.Fatalf("idempotency key=%q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["payment_method"] != "NQ" {
			t.Fatalf("invoice body=%v decode error=%v", body, err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"transaction_id":       enrollmentID,
			"checkout_session_url": "https://checkout.test/session",
			"payment_intent_id":    "payment-1",
		}})
	}))
	defer server.Close()

	response, err := NewClient(server.URL, credential).GenerateInvoice(t.Context(), InvoiceRequest{EnrollmentID: enrollmentID, IdempotencyKey: "same-key", PaymentMethod: "NQ"})
	if err != nil {
		t.Fatal(err)
	}
	if response.TransactionID != enrollmentID {
		t.Fatalf("transaction id=%s", response.TransactionID)
	}
}

func TestGenerateInvoiceRedactsCredentialFromError(t *testing.T) {
	credential := "secret-not-for-errors"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid ` + credential + `"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, credential).GenerateInvoice(t.Context(), InvoiceRequest{})
	if err == nil || strings.Contains(err.Error(), credential) || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("unexpected error=%v", err)
	}
}

// The old ten-second client cancelled a valid provider result before billing
// could persist/return it. This delay intentionally crosses that old boundary.
func TestGenerateInvoiceAcceptsResponseAfterFormerDeadline(t *testing.T) {
	id := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(11 * time.Second):
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"transaction_id":       id,
			"checkout_session_url": "https://checkout.test/slow-qris",
		}})
	}))
	defer server.Close()
	response, err := NewClient(server.URL, "test-credential").GenerateInvoice(t.Context(), InvoiceRequest{EnrollmentID: id, IdempotencyKey: "same-order", PaymentMethod: "NQ"})
	if err != nil || response.TransactionID != id {
		t.Fatalf("slow invoice response=%+v error=%v", response, err)
	}
}

func TestGenerateInvoiceHonorsShorterCallerDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := NewClient(server.URL, "test-credential").GenerateInvoice(ctx, InvoiceRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want caller deadline", err)
	}
}
