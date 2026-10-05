package billing

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestPreviewVoucherSendsExactBody(t *testing.T) {
	var gotPath string
	var gotHeaders map[string]string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeaders = map[string]string{"Content-Type": r.Header.Get("Content-Type"), "X-Internal-Service-Credential": r.Header.Get("X-Internal-Service-Credential")}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"discount_amount":25000,"gross_amount":75000}}`))
	}))
	defer server.Close()

	tenantID := uuid.New()
	res, err := NewClient(server.URL, "credential").(VoucherPreviewClient).PreviewVoucher(t.Context(), VoucherPreviewRequest{TenantID: tenantID, SubtotalAmount: 100000, VoucherCode: "HEMAT"})
	if err != nil {
		t.Fatalf("PreviewVoucher() error = %v", err)
	}
	if res.DiscountAmount != 25000 || res.GrossAmount != 75000 {
		t.Fatalf("preview = %+v", res)
	}
	if gotPath != "/internal/billing/vouchers/preview" {
		t.Fatalf("path = %s", gotPath)
	}
	if gotHeaders["X-Internal-Service-Credential"] != "credential" {
		t.Fatalf("credential header = %v", gotHeaders)
	}
	if gotBody["voucher_code"] != "HEMAT" || gotBody["subtotal_amount"] != float64(100000) || gotBody["tenant_id"] != tenantID.String() {
		t.Fatalf("body = %v", gotBody)
	}
	if len(gotBody) != 3 {
		t.Fatalf("body must have exactly tenant_id, subtotal_amount, voucher_code; got %v", gotBody)
	}
}

func TestPreviewVoucherInvalidEnvelope(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":null}`, `{"data":{"discount_amount":-1,"gross_amount":100}}`, `{"data":{"discount_amount":"bad","gross_amount":100}}`, `invalid`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			if _, err := NewClient(server.URL, "credential").(VoucherPreviewClient).PreviewVoucher(t.Context(), VoucherPreviewRequest{TenantID: uuid.New(), VoucherCode: "X"}); err == nil || errors.Is(err, ErrVoucherRejected) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestPreviewVoucherRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"status":"error","message":"voucher rejected","code":"voucher_rejected"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "credential").(VoucherPreviewClient).PreviewVoucher(t.Context(), VoucherPreviewRequest{TenantID: uuid.New(), SubtotalAmount: 100000, VoucherCode: "X"})
	if !errors.Is(err, ErrVoucherRejected) {
		t.Fatalf("error = %v, want ErrVoucherRejected", err)
	}
}

func TestPreviewVoucherOtherErrorKeepsGeneric(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "credential").(VoucherPreviewClient).PreviewVoucher(t.Context(), VoucherPreviewRequest{TenantID: uuid.New(), SubtotalAmount: 100000, VoucherCode: "X"})
	if err == nil || errors.Is(err, ErrVoucherRejected) {
		t.Fatalf("error = %v, want generic error", err)
	}
}

func TestGenerateInvoiceVoucherRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"status":"error","message":"voucher rejected","code":"voucher_rejected"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "credential").GenerateInvoice(t.Context(), InvoiceRequest{IdempotencyKey: "key"})
	if !errors.Is(err, ErrVoucherRejected) {
		t.Fatalf("error = %v, want ErrVoucherRejected", err)
	}
}

func TestGenerateInvoiceSendsVoucherCode(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"transaction_id":"` + uuid.NewString() + `","checkout_session_url":"https://x","payment_intent_id":"p1","gross_amount":75000,"discount_amount":25000}}`))
	}))
	defer server.Close()

	res, err := NewClient(server.URL, "credential").GenerateInvoice(t.Context(), InvoiceRequest{IdempotencyKey: "key", VoucherCode: "HEMAT"})
	if err != nil {
		t.Fatalf("GenerateInvoice() error = %v", err)
	}
	if res.GrossAmount != 75000 || res.DiscountAmount != 25000 {
		t.Fatalf("invoice = %+v", res)
	}
	want := map[string]any{"voucher_code": "HEMAT", "tenant_id": uuid.Nil.String(), "student_id": uuid.Nil.String(), "class_id": uuid.Nil.String(), "enrollment_id": uuid.Nil.String(), "parent_id": uuid.Nil.String(), "billing_cycle": "", "subtotal_amount": float64(0), "idempotency_key": "key", "title": ""}
	if !reflect.DeepEqual(gotBody, want) {
		t.Fatalf("invoice body=%v want=%v", gotBody, want)
	}
}
