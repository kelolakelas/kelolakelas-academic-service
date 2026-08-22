package billing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"transaction_id":       enrollmentID,
			"checkout_session_url": "https://checkout.test/session",
			"payment_intent_id":    "payment-1",
		}})
	}))
	defer server.Close()

	response, err := NewClient(server.URL, credential).GenerateInvoice(t.Context(), InvoiceRequest{EnrollmentID: enrollmentID, IdempotencyKey: "same-key"})
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
