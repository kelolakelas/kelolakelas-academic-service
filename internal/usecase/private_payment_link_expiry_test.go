package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/paymentlink"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPaymentLinkExpiryDuringDelivery(t *testing.T) {
	u, fixture, r, _ := linkFixture()
	expiry := time.Now().Add(50 * time.Millisecond)
	fixture.transaction.InvoiceExpiresAt = &expiry
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": fixture.transaction})
	}))
	defer billing.Close()
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var input paymentlink.Input
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if !input.InvoiceExpiresAt.Equal(expiry) || input.RequestID != r.ID || input.CheckoutSessionURL != fixture.transaction.CheckoutSessionURL {
			t.Error("trusted payload mismatch")
		}
		time.Sleep(time.Until(expiry.Add(20 * time.Millisecond)))
		w.WriteHeader(http.StatusConflict)
	}))
	defer chat.Close()
	u.client = paymentlink.NewClient(billing.URL, chat.URL, "shared")
	if _, err := u.SendPaymentLink(context.Background(), r.TenantID, r.ID, "Bearer verified"); !errors.Is(err, ErrPaymentLinkInactive) {
		t.Fatalf("delayed expiry: %v", err)
	}
}
