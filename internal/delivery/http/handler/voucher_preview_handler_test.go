package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

type voucherHandlerUsecase struct {
	usecase.EnrollmentUsecase
	calls             int
	parentID, classID uuid.UUID
	code              string
	err               error
}

func (u *voucherHandlerUsecase) PreviewVoucher(_ context.Context, parentID, classID uuid.UUID, code string) (*billing.VoucherPreviewResponse, error) {
	u.calls++
	u.parentID, u.classID, u.code = parentID, classID, code
	return &billing.VoucherPreviewResponse{DiscountAmount: 25, GrossAmount: 75}, u.err
}

func TestVoucherPreviewHandlerAuthenticationValidationAndRejection(t *testing.T) {
	parentID, classID := uuid.New(), uuid.New()
	for _, tt := range []struct {
		name, token, class, body string
		err                      error
		status, calls            int
		code                     string
	}{
		{name: "anonymous", class: classID.String(), body: `{"voucher_code":"HEMAT"}`, status: 401},
		{name: "tenant", token: signToken(t, middleware.Claims{UserID: parentID.String(), TenantID: uuid.NewString()}), class: classID.String(), body: `{"voucher_code":"HEMAT"}`, status: 403},
		{name: "invalid parent", token: signToken(t, middleware.Claims{UserID: "not-a-uuid", IsParent: true}), class: classID.String(), body: `{"voucher_code":"HEMAT"}`, status: 403},
		{name: "invalid class", token: signToken(t, middleware.Claims{UserID: parentID.String(), IsParent: true}), class: "bad", body: `{"voucher_code":"HEMAT"}`, status: 400},
		{name: "missing code", token: signToken(t, middleware.Claims{UserID: parentID.String(), IsParent: true}), class: classID.String(), body: `{}`, status: 400},
		{name: "too long", token: signToken(t, middleware.Claims{UserID: parentID.String(), IsParent: true}), class: classID.String(), body: `{"voucher_code":"` + strings.Repeat("X", 256) + `"}`, status: 400},
		{name: "success ignores client amounts", token: signToken(t, middleware.Claims{UserID: parentID.String(), IsParent: true}), class: classID.String(), body: `{"voucher_code":"HEMAT","tenant_id":"spoof","subtotal_amount":1,"discount_amount":999}`, status: 200, calls: 1},
		{name: "rejected", token: signToken(t, middleware.Claims{UserID: parentID.String(), IsParent: true}), class: classID.String(), body: `{"voucher_code":"HEMAT"}`, err: domain.ErrVoucherRejected, status: 422, calls: 1, code: "voucher_rejected"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			u := &voucherHandlerUsecase{err: tt.err}
			r := gin.New()
			r.Use(middleware.AuthMiddleware(testJWTSecret))
			r.POST("/catalog/classes/:class_id/voucher-preview", NewEnrollmentHandler(u).PreviewVoucher)
			req := httptest.NewRequest(http.MethodPost, "/catalog/classes/"+tt.class+"/voucher-preview", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			if res.Code != tt.status || u.calls != tt.calls {
				t.Fatalf("status=%d calls=%d body=%s", res.Code, u.calls, res.Body.String())
			}
			if u.calls > 0 && (u.parentID != parentID || u.classID != classID || u.code != "HEMAT") {
				t.Fatalf("unexpected preview arguments: %+v", u)
			}
			var body map[string]any
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tt.code != "" && body["code"] != tt.code {
				t.Fatalf("body=%v", body)
			}
			if res.Code == 200 {
				data := body["data"].(map[string]any)
				if data["gross_amount"] != float64(75) || data["discount_amount"] != float64(25) {
					t.Fatalf("data=%v", data)
				}
			}
		})
	}
}
