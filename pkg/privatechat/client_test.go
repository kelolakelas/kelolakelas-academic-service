package privatechat

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestInvoiceReadUsesCredentialAndValidatesExactScope(t *testing.T) {
	scope := InvoiceScope{TransactionID: uuid.New(), EnrollmentID: uuid.New(), TenantID: uuid.New(), ParentID: uuid.New(), StudentID: uuid.New()}
	var bad atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/billing/private-request-invoice" || r.Method != "POST" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Internal-Service-Credential") != "internal" {
			t.Error("credential contract")
		}
		var received InvoiceScope
		if json.NewDecoder(r.Body).Decode(&received) != nil || received != scope {
			t.Error("scope contract")
		}
		id := scope.TransactionID
		if bad.Load() {
			id = uuid.New()
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": Invoice{ID: id, EnrollmentID: scope.EnrollmentID, TenantID: scope.TenantID, ParentID: scope.ParentID, StudentID: scope.StudentID, Status: "pending"}})
	}))
	defer server.Close()
	c := New(server.URL, "", "internal")
	if _, err := c.Invoice(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	bad.Store(true)
	if _, err := c.Invoice(context.Background(), scope); err == nil {
		t.Fatal("mis-scoped invoice accepted")
	}
}
func TestPrivateChatTransportRefusesRedirectsAndMalformedResponses(t *testing.T) {
	for _, status := range []int{302, 404, 503, 200} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://should-not-follow.invalid")
				w.WriteHeader(status)
				w.Write([]byte(`{"status":"success","data":null}`))
			}))
			defer server.Close()
			c := New(server.URL, server.URL, "internal")
			c.HTTP.Timeout = time.Second
			if _, err := c.Invoice(context.Background(), InvoiceScope{}); err == nil {
				t.Fatal("invalid response accepted")
			}
			if err := c.Deliver(context.Background(), Delivery{}); err == nil {
				t.Fatal("invalid delivery accepted")
			}
		})
	}
}
