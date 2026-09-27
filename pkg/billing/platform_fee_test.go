package billing

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Billing's KEL-99 answer for a refused invoice, verbatim from
// kelolakelas-billing-service internal/delivery/http/handler/transaction_handler.go.
const platformFeeRejectionBody = `{"status":"error","code":"platform_fee_exceeds_gross","message":"Biaya platform melebihi jumlah pembayaran","data":null}`

func generateInvoiceAgainst(t *testing.T, status int, body string) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	_, err := NewClient(server.URL, "credential").GenerateInvoice(t.Context(), InvoiceRequest{IdempotencyKey: "key"})
	return err
}

func TestGenerateInvoicePlatformFeeRejection(t *testing.T) {
	err := generateInvoiceAgainst(t, http.StatusUnprocessableEntity, platformFeeRejectionBody)
	if !errors.Is(err, ErrPlatformFeeExceedsGross) {
		t.Fatalf("error = %v, want ErrPlatformFeeExceedsGross", err)
	}
}

// Only the exact status and code pair is the permanent rejection. Everything else
// keeps the previous generic error, so no other billing failure changes meaning.
func TestGenerateInvoiceOtherFailuresStayGeneric(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantSubstr string
	}{
		{name: "422 without code", status: http.StatusUnprocessableEntity, body: `{"status":"error","message":"invalid amount"}`, wantSubstr: "status 422: invalid amount"},
		{name: "422 with another code", status: http.StatusUnprocessableEntity, body: `{"status":"error","code":"other_rule","message":"other rule"}`, wantSubstr: "status 422: other rule"},
		{name: "422 without a JSON body", status: http.StatusUnprocessableEntity, body: `not json`, wantSubstr: "status 422"},
		{name: "same code on another status", status: http.StatusInternalServerError, body: platformFeeRejectionBody, wantSubstr: "status 500"},
		{name: "fee policy unavailable", status: http.StatusServiceUnavailable, body: `{"status":"error","code":"platform_fee_policy_unavailable","message":"Platform fee policy is unavailable"}`, wantSubstr: "status 503: Platform fee policy is unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := generateInvoiceAgainst(t, tt.status, tt.body)
			if err == nil || errors.Is(err, ErrPlatformFeeExceedsGross) || !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error = %v, want a generic error containing %q", err, tt.wantSubstr)
			}
		})
	}
}
