package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type privateRequestUsecaseRecorder struct {
	called         string
	email          string
	tenant, parent *uuid.UUID
	approveErr     error
}

func (u *privateRequestUsecaseRecorder) Create(_ context.Context, parent, class uuid.UUID, req *domain.CreatePrivateScheduleRequest) (*domain.PrivateScheduleRequest, error) {
	u.called = "create"
	u.email = req.ParentEmail
	return &domain.PrivateScheduleRequest{ID: uuid.New(), Status: "pending", ParentID: parent, ClassID: class}, nil
}
func (u *privateRequestUsecaseRecorder) Get(_ context.Context, _ uuid.UUID, tenant, parent *uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	u.called = "get"
	u.tenant, u.parent = tenant, parent
	return &domain.PrivateScheduleRequest{}, nil
}
func (u *privateRequestUsecaseRecorder) List(_ context.Context, tenant, parent *uuid.UUID, _ string) ([]domain.PrivateScheduleRequest, error) {
	u.called = "list"
	u.tenant, u.parent = tenant, parent
	return []domain.PrivateScheduleRequest{}, nil
}
func (u *privateRequestUsecaseRecorder) Reject(_ context.Context, tenant, id uuid.UUID, _ *string) (*domain.PrivateScheduleRequest, error) {
	u.called = "reject"
	return &domain.PrivateScheduleRequest{ID: id, TenantID: tenant, Status: "rejected"}, nil
}
func (u *privateRequestUsecaseRecorder) Cancel(_ context.Context, parent, id uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	u.called = "cancel"
	return &domain.PrivateScheduleRequest{ID: id, ParentID: parent, Status: "cancelled"}, nil
}
func (u *privateRequestUsecaseRecorder) Approve(_ context.Context, tenant, id uuid.UUID) (*domain.PublicEnrollmentResponse, error) {
	u.called = "approve"
	u.tenant = &tenant
	if u.approveErr != nil {
		return nil, u.approveErr
	}
	return &domain.PublicEnrollmentResponse{Enrollment: &domain.EnrollmentResponse{ID: id}, Payment: &domain.PaymentResponse{CheckoutSessionURL: "https://pay.example.test"}}, nil
}

func TestPrivateRequestHandlerPermissionAndParent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	u := &privateRequestUsecaseRecorder{}
	h := NewPrivateScheduleRequestHandler(u)
	p := &studentPermissionClientStub{}
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(middleware.AuthMiddleware(testJWTSecret))
	api.GET("/schedule-requests", middleware.RequirePermissionUnlessParent(p, "enrollment:read"), h.List)
	api.GET("/schedule-requests/:id", middleware.RequirePermissionUnlessParent(p, "enrollment:read"), h.Get)
	api.POST("/schedule-requests/:id/approve", middleware.RequirePermission(p, "enrollment:update"), h.Approve)
	api.POST("/schedule-requests/:id/reject", middleware.RequirePermission(p, "enrollment:update"), h.Reject)
	api.POST("/schedule-requests/:id/cancel", h.Cancel)
	api.POST("/catalog/classes/:class_id/schedule-requests", h.Create)
	token := func(parent bool) string {
		claims := jwt.MapClaims{"user_id": uuid.NewString(), "tenant_id": uuid.NewString(), "role_id": uuid.NewString(), "member_id": uuid.NewString(), "email": "verified@example.test", "is_parent": parent, "exp": time.Now().Add(time.Hour).Unix()}
		raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testJWTSecret))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	call := func(method, path, body, auth string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	tenantToken, parentToken := token(false), token(true)
	id := uuid.NewString()
	createBody := `{"student_id":"` + id + `","billing_cycle":"monthly","slots":[{"day_of_week":1,"start_time":"10:00:00","end_time":"11:00:00"}],"parent_email":"forged@example.test"}`
	if code := call(http.MethodPost, "/api/v1/catalog/classes/"+id+"/schedule-requests", createBody, parentToken); code != 201 || u.email != "verified@example.test" {
		t.Fatalf("parent create=%d email=%s", code, u.email)
	}
	if code := call(http.MethodPost, "/api/v1/catalog/classes/"+id+"/schedule-requests", createBody, tenantToken); code != 403 {
		t.Fatalf("tenant create=%d", code)
	}
	if code := call(http.MethodGet, "/api/v1/schedule-requests", "", parentToken); code != 200 || u.parent == nil || u.tenant != nil {
		t.Fatalf("parent list=%d", code)
	}
	if code := call(http.MethodPost, "/api/v1/schedule-requests/"+id+"/cancel", "", tenantToken); code != 403 {
		t.Fatalf("tenant cancel=%d", code)
	}
	if code := call(http.MethodPost, "/api/v1/schedule-requests/"+id+"/approve", "", parentToken); code != 403 {
		t.Fatalf("parent approve=%d", code)
	}
	if code := call(http.MethodPost, "/api/v1/schedule-requests/"+id+"/reject", "", parentToken); code != 403 {
		t.Fatalf("parent reject=%d", code)
	}
	// With no permission client authorizing, tenant reads and writes stop before the usecase.
	u.called = ""
	if code := call(http.MethodGet, "/api/v1/schedule-requests/"+id, "", tenantToken); code != 403 || u.called != "" {
		t.Fatalf("tenant denied read=%d called=%s", code, u.called)
	}
	if code := call(http.MethodPost, "/api/v1/schedule-requests/"+id+"/reject", "{}", tenantToken); code != 403 || u.called != "" {
		t.Fatalf("tenant denied reject=%d called=%s", code, u.called)
	}
	if code := call(http.MethodPost, "/api/v1/schedule-requests/"+id+"/approve", "", tenantToken); code != 403 || u.called != "" {
		t.Fatalf("tenant denied approve=%d called=%s", code, u.called)
	}
	p.allowed = true
	if code := call(http.MethodPost, "/api/v1/schedule-requests/"+id+"/approve", "", tenantToken); code != 200 || u.called != "approve" || u.tenant == nil {
		t.Fatalf("tenant approve=%d called=%s", code, u.called)
	}
	for _, tc := range []struct {
		err    error
		status int
	}{
		{domain.ErrPrivateRequestNotFound, 404}, {domain.ErrPrivateRequestTransition, 409}, {domain.ErrPlatformFeeExceedsGross, 422},
	} {
		u.approveErr = tc.err
		if code := call(http.MethodPost, "/api/v1/schedule-requests/"+id+"/approve", "", tenantToken); code != tc.status {
			t.Fatalf("approve error %v: got %d want %d", tc.err, code, tc.status)
		}
	}
	u.approveErr = nil
	if code := call(http.MethodGet, "/api/v1/schedule-requests/"+id, "", tenantToken); code != 200 || u.called != "get" || u.tenant == nil || u.parent != nil {
		t.Fatalf("tenant permitted read=%d called=%s", code, u.called)
	}
	if code := call(http.MethodPost, "/api/v1/schedule-requests/"+id+"/reject", "{}", tenantToken); code != 200 || u.called != "reject" {
		t.Fatalf("tenant permitted reject=%d called=%s", code, u.called)
	}
	if got := p.permissions[len(p.permissions)-2:]; got[0] != "enrollment:read" || got[1] != "enrollment:update" {
		t.Fatalf("permissions=%v", got)
	}
}
