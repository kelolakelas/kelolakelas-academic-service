package billing

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCancelEnrollmentPaymentPostsEnrollmentIDWithInternalCredential(t *testing.T) {
	credential := "academic-billing-secret"
	enrollmentID := uuid.New()
	transactionID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/billing/transactions/cancel" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content type=%q", got)
		}
		if got := r.Header.Get("X-Internal-Service-Credential"); got != credential {
			t.Fatalf("credential=%q", got)
		}
		var body CancelRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.EnrollmentID != enrollmentID {
			t.Fatalf("enrollment id=%s, want %s", body.EnrollmentID, enrollmentID)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"id":     transactionID,
			"status": "cancelled",
		}})
	}))
	defer server.Close()

	response, err := NewClient(server.URL, credential).CancelEnrollmentPayment(t.Context(), enrollmentID)
	if err != nil {
		t.Fatal(err)
	}
	if response.TransactionID != transactionID || response.Status != "cancelled" {
		t.Fatalf("response=%+v", response)
	}
}

// Not found means the enrollment never got an invoice, which the usecase treats as
// "nothing to pay". It must be a sentinel so the caller does not parse message text.
func TestCancelEnrollmentPaymentMapsNotFoundToSentinel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"error","message":"Transaction not found"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "credential").CancelEnrollmentPayment(t.Context(), uuid.New())
	if !errors.Is(err, ErrTransactionNotFound) {
		t.Fatalf("error=%v, want ErrTransactionNotFound", err)
	}
}

// A settled transaction must be distinguishable from a missing one: only the first
// means money moved and the seat must stay reserved.
func TestCancelEnrollmentPaymentMapsConflictToSentinel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"status":"error","message":"Transaction can no longer be cancelled"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "credential").CancelEnrollmentPayment(t.Context(), uuid.New())
	if !errors.Is(err, ErrTransactionNotCancellable) {
		t.Fatalf("error=%v, want ErrTransactionNotCancellable", err)
	}
	if errors.Is(err, ErrTransactionNotFound) {
		t.Fatal("a settled transaction must not be reported as missing")
	}
}

// Any other failure stays an unclassified error so the call is retried instead of
// silently freeing the seat.
func TestCancelEnrollmentPaymentKeepsUnexpectedStatusAsRetryableFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"database is unavailable"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "credential").CancelEnrollmentPayment(t.Context(), uuid.New())
	if err == nil {
		t.Fatal("error=nil, want a failure")
	}
	if errors.Is(err, ErrTransactionNotFound) || errors.Is(err, ErrTransactionNotCancellable) {
		t.Fatalf("error=%v must not be classified as terminal", err)
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("error=%v, want the status in the message", err)
	}
}

func TestCancelEnrollmentPaymentRedactsCredentialFromError(t *testing.T) {
	credential := "secret-not-for-errors"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid ` + credential + `"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, credential).CancelEnrollmentPayment(t.Context(), uuid.New())
	if err == nil || strings.Contains(err.Error(), credential) || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("unexpected error=%v", err)
	}
}
