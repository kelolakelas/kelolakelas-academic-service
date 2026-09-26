package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

func TestCatalogEnrollmentErrorStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "schedule full is conflict", err: domain.ErrScheduleFull, want: http.StatusConflict},
		{name: "idempotency conflict", err: domain.ErrIdempotencyConflict, want: http.StatusConflict},
		{name: "duplicate enrollment is conflict", err: domain.ErrDuplicateEnrollment, want: http.StatusConflict},
		{name: "wrapped duplicate enrollment is conflict", err: fmt.Errorf("create enrollment: %w", domain.ErrDuplicateEnrollment), want: http.StatusConflict},
		{name: "ownership is unprocessable", err: domain.ErrStudentOwnership, want: http.StatusUnprocessableEntity},
		{name: "schedule required is unprocessable", err: domain.ErrScheduleRequired, want: http.StatusUnprocessableEntity},
		{name: "ended schedule is unprocessable", err: domain.ErrScheduleEnded, want: http.StatusUnprocessableEntity},
		{name: "class not found", err: domain.ErrClassNotFound, want: http.StatusNotFound},
		{name: "billing failure is server error", err: errors.New("generate enrollment invoice: provider unavailable"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := catalogEnrollmentErrorStatus(tt.err); got != tt.want {
				t.Fatalf("catalogEnrollmentErrorStatus() = %d, want %d", got, tt.want)
			}
		})
	}
}

// The duplicate enrollment 409 carries code=duplicate_enrollment; a full schedule
// and an idempotency conflict keep their previous 409 body with no code, so a
// client can tell them apart without reading the message.
func TestCatalogEnrollmentConflictBodies(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{name: "duplicate enrollment", err: domain.ErrDuplicateEnrollment, wantStatus: http.StatusConflict, wantCode: "duplicate_enrollment", wantMessage: domain.ErrDuplicateEnrollment.Error()},
		{name: "schedule full", err: domain.ErrScheduleFull, wantStatus: http.StatusConflict, wantMessage: domain.ErrScheduleFull.Error()},
		{name: "idempotency conflict", err: domain.ErrIdempotencyConflict, wantStatus: http.StatusConflict, wantMessage: domain.ErrIdempotencyConflict.Error()},
		{name: "unexpected failure", err: errors.New("database down"), wantStatus: http.StatusInternalServerError, wantMessage: "Failed to create enrollment"},
	}
	token := signToken(t, middleware.Claims{UserID: uuid.NewString(), Email: "parent@example.com", IsParent: true})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(middleware.AuthMiddleware(testJWTSecret))
			router.POST("/api/v1/catalog/classes/:class_id/enrollments", NewEnrollmentHandler(&failingEnrollmentUsecase{err: tt.err}).CreateCatalogEnrollment)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/catalog/classes/"+uuid.NewString()+"/enrollments", strings.NewReader(`{"student_id":"`+uuid.NewString()+`","billing_cycle":"monthly"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Idempotency-Key", uuid.NewString())
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", res.Code, res.Body.String(), tt.wantStatus)
			}
			var body map[string]any
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			code, hasCode := body["code"]
			if tt.wantCode == "" && hasCode {
				t.Fatalf("code = %v, want no code field", code)
			}
			if tt.wantCode != "" && code != tt.wantCode {
				t.Fatalf("code = %v, want %q", code, tt.wantCode)
			}
			if body["status"] != "error" || body["message"] != tt.wantMessage {
				t.Fatalf("body = %v, want status=error message=%q", body, tt.wantMessage)
			}
		})
	}
}

func TestTenantEnrollmentDuplicateIsConflict(t *testing.T) {
	tenantID := uuid.New()
	token := signToken(t, middleware.Claims{UserID: uuid.NewString(), TenantID: tenantID.String(), RoleID: uuid.NewString()})
	router := gin.New()
	router.Use(middleware.AuthMiddleware(testJWTSecret))
	router.POST("/api/v1/tenants/:tenant_id/enrollments", NewEnrollmentHandler(&failingEnrollmentUsecase{err: fmt.Errorf("create enrollment: %w", domain.ErrDuplicateEnrollment)}).Create)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+tenantID.String()+"/enrollments", strings.NewReader(`{"student_id":"`+uuid.NewString()+`","class_id":"`+uuid.NewString()+`","billing_cycle":"monthly"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), `"code":"duplicate_enrollment"`) {
		t.Fatalf("status=%d body=%s, want 409 with code duplicate_enrollment", res.Code, res.Body.String())
	}
}
