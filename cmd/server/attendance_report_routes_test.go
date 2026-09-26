package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

const routeTestSecret = "attendance-report-route-test-secret"

type routePermissionClient struct {
	permissions                map[string]bool
	err                        error
	calls                      []string
	tenantID, roleID, memberID string
}

func (p *routePermissionClient) CheckPermission(_ context.Context, tenant, role, member, permission string) (bool, error) {
	p.calls = append(p.calls, permission)
	p.tenantID, p.roleID, p.memberID = tenant, role, member
	return p.permissions[permission], p.err
}
func (*routePermissionClient) Close() error { return nil }

type routeAttendanceUsecase struct {
	calls     int
	createErr error
}

func (u *routeAttendanceUsecase) List(context.Context, uuid.UUID, domain.AttendanceQuery) (*domain.AttendanceListResponse, error) {
	u.calls++
	return &domain.AttendanceListResponse{}, nil
}
func (u *routeAttendanceUsecase) Create(_ context.Context, _, _ uuid.UUID, _ *domain.CreateAttendanceRequest) (*domain.Attendance, error) {
	u.calls++
	if u.createErr != nil {
		return nil, u.createErr
	}
	return &domain.Attendance{}, nil
}
func (u *routeAttendanceUsecase) Get(context.Context, uuid.UUID, uuid.UUID) (*domain.Attendance, error) {
	u.calls++
	return &domain.Attendance{}, nil
}
func (u *routeAttendanceUsecase) Update(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, *domain.UpdateAttendanceRequest) (*domain.Attendance, error) {
	u.calls++
	return &domain.Attendance{}, nil
}

type routeReportUsecase struct {
	calls     int
	createErr error
}

func (u *routeReportUsecase) List(context.Context, uuid.UUID, domain.ReportQuery) (*domain.ReportListResponse, error) {
	u.calls++
	return &domain.ReportListResponse{}, nil
}
func (u *routeReportUsecase) Create(_ context.Context, _, _ uuid.UUID, _ *domain.CreateReportRequest) (*domain.Report, error) {
	u.calls++
	if u.createErr != nil {
		return nil, u.createErr
	}
	return &domain.Report{}, nil
}
func (u *routeReportUsecase) Get(context.Context, uuid.UUID, uuid.UUID) (*domain.Report, error) {
	u.calls++
	return &domain.Report{}, nil
}
func (u *routeReportUsecase) Update(context.Context, uuid.UUID, uuid.UUID, *domain.UpdateReportRequest) (*domain.Report, error) {
	u.calls++
	return &domain.Report{}, nil
}
func (u *routeReportUsecase) Delete(context.Context, uuid.UUID, uuid.UUID) error {
	u.calls++
	return nil
}

func routeToken(t *testing.T, claims middleware.Claims) string {
	t.Helper()
	claims.RegisteredClaims = jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(routeTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func routeFixture(t *testing.T, p *routePermissionClient, a *routeAttendanceUsecase, r *routeReportUsecase) (*gin.Engine, middleware.Claims) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	claims := middleware.Claims{UserID: uuid.NewString(), TenantID: uuid.NewString(), RoleID: uuid.NewString(), MemberID: uuid.NewString()}
	engine := gin.New()
	api := engine.Group("/api/v1")
	api.Use(middleware.AuthMiddleware(routeTestSecret))
	registerAttendanceReportRoutes(api, p, handler.NewAttendanceHandler(a), handler.NewReportHandler(r))
	return engine, claims
}

func routeRequest(engine *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	return response
}

func TestAttendanceReportRoutesPermissionMatrix(t *testing.T) {
	id := uuid.NewString()
	enrollment := uuid.NewString()
	schedule := uuid.NewString()
	attendanceBody := fmt.Sprintf(`{"enrollment_id":%q,"schedule_id":%q,"date":"2026-09-26","status":"present"}`, enrollment, schedule)
	reportBody := fmt.Sprintf(`{"enrollment_id":%q,"title":"Progress"}`, enrollment)
	routes := []struct {
		method, path, body, permission string
		success                        int
		attendance                     bool
	}{
		{http.MethodGet, "/api/v1/attendance", "", "attendance:read", 200, true},
		{http.MethodPost, "/api/v1/attendance", attendanceBody, "attendance:create", 201, true},
		{http.MethodGet, "/api/v1/attendance/" + id, "", "attendance:read", 200, true},
		{http.MethodPatch, "/api/v1/attendance/" + id, `{"status":"late"}`, "attendance:update", 200, true},
		{http.MethodGet, "/api/v1/reports", "", "report:read", 200, false},
		{http.MethodPost, "/api/v1/reports", reportBody, "report:create", 201, false},
		{http.MethodGet, "/api/v1/reports/" + id, "", "report:read", 200, false},
		{http.MethodPatch, "/api/v1/reports/" + id, `{"title":"Updated"}`, "report:update", 200, false},
		{http.MethodDelete, "/api/v1/reports/" + id, "", "report:delete", 200, false},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			for _, scenario := range []struct {
				name        string
				permissions map[string]bool
				err         error
				status      int
				calls       int
			}{
				{"denied", nil, nil, 403, 0},
				{"allowed only corresponding permission", map[string]bool{route.permission: true}, nil, route.success, 1},
				{"identity unavailable", map[string]bool{route.permission: true}, errors.New("down"), 503, 0},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					p := &routePermissionClient{permissions: scenario.permissions, err: scenario.err}
					a, r := &routeAttendanceUsecase{}, &routeReportUsecase{}
					engine, claims := routeFixture(t, p, a, r)
					response := routeRequest(engine, route.method, route.path, route.body, routeToken(t, claims))
					if response.Code != scenario.status {
						t.Fatalf("status=%d want=%d body=%s", response.Code, scenario.status, response.Body.String())
					}
					if len(p.calls) != 1 || p.calls[0] != route.permission || p.tenantID != claims.TenantID || p.roleID != claims.RoleID || p.memberID != claims.MemberID {
						t.Fatalf("permission lookup=%v (%s,%s,%s), want %s and verified claims", p.calls, p.tenantID, p.roleID, p.memberID, route.permission)
					}
					actualCalls := r.calls
					otherCalls := a.calls
					if route.attendance {
						actualCalls, otherCalls = a.calls, r.calls
					}
					if actualCalls != scenario.calls || otherCalls != 0 {
						t.Fatalf("usecase calls=%d other=%d, want %d and 0", actualCalls, otherCalls, scenario.calls)
					}
				})
			}
		})
	}
}

func TestAttendanceReportRoutesParentTenantCompatibility(t *testing.T) {
	id := uuid.NewString()
	for _, route := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/attendance"},
		{http.MethodPost, "/api/v1/attendance"},
		{http.MethodGet, "/api/v1/attendance/" + id},
		{http.MethodPatch, "/api/v1/attendance/" + id},
		{http.MethodGet, "/api/v1/reports"},
		{http.MethodPost, "/api/v1/reports"},
		{http.MethodGet, "/api/v1/reports/" + id},
		{http.MethodPatch, "/api/v1/reports/" + id},
		{http.MethodDelete, "/api/v1/reports/" + id},
	} {
		for _, tenant := range []string{"", "not-a-uuid"} {
			t.Run(route.method+" "+route.path+" tenant="+tenant, func(t *testing.T) {
				p := &routePermissionClient{err: errors.New("unavailable")}
				a, r := &routeAttendanceUsecase{}, &routeReportUsecase{}
				engine, claims := routeFixture(t, p, a, r)
				claims.IsParent, claims.TenantID = true, tenant
				response := routeRequest(engine, route.method, route.path, `{}`, routeToken(t, claims))
				if response.Code != http.StatusUnauthorized || response.Body.String() != "{\"data\":null,\"message\":\"Invalid tenant context\",\"status\":\"error\"}" {
					t.Fatalf("status=%d body=%s, want original tenant error", response.Code, response.Body.String())
				}
				if len(p.calls) != 0 || a.calls != 0 || r.calls != 0 {
					t.Fatalf("invalid tenant reached permission/usecase: %v %d %d", p.calls, a.calls, r.calls)
				}
			})
		}
	}
	// Before permission gating, a parent token carrying a tenant could reach the
	// handler without a member role. Preserve that route rather than defining a
	// new parent policy here.
	p := &routePermissionClient{err: errors.New("unavailable")}
	a, r := &routeAttendanceUsecase{}, &routeReportUsecase{}
	engine, claims := routeFixture(t, p, a, r)
	claims.IsParent, claims.RoleID, claims.MemberID = true, "", ""
	response := routeRequest(engine, http.MethodGet, "/api/v1/reports", "", routeToken(t, claims))
	if response.Code != http.StatusOK || r.calls != 1 || a.calls != 0 || len(p.calls) != 0 {
		t.Fatalf("parent tenant response=%d permission=%v usecases=%d,%d body=%s", response.Code, p.calls, a.calls, r.calls, response.Body.String())
	}
}

func TestAttendanceReportRoutesIndependentAndAssignment(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, permission string
		attendance                   bool
	}{
		{"attendance", "/api/v1/attendance", fmt.Sprintf(`{"enrollment_id":%q,"schedule_id":%q,"date":"2026-09-26","status":"present"}`, uuid.NewString(), uuid.NewString()), "attendance:create", true},
		{"report", "/api/v1/reports", fmt.Sprintf(`{"enrollment_id":%q,"title":"Progress"}`, uuid.NewString()), "report:create", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &routePermissionClient{permissions: map[string]bool{tc.permission: true}}
			a, r := &routeAttendanceUsecase{}, &routeReportUsecase{}
			engine, claims := routeFixture(t, p, a, r)
			token := routeToken(t, claims)
			// A role with read but not create may list, but cannot create.
			p.permissions = map[string]bool{strings.TrimSuffix(tc.permission, ":create") + ":read": true}
			if result := routeRequest(engine, http.MethodGet, tc.path, "", token); result.Code != 200 {
				t.Fatalf("read status=%d body=%s", result.Code, result.Body.String())
			}
			if result := routeRequest(engine, http.MethodPost, tc.path, tc.body, token); result.Code != 403 {
				t.Fatalf("create without permission status=%d", result.Code)
			}
			p.permissions = map[string]bool{tc.permission: true}
			if tc.attendance {
				a.createErr = domain.ErrAttendanceForbidden
			} else {
				r.createErr = domain.ErrReportForbidden
			}
			if result := routeRequest(engine, http.MethodPost, tc.path, tc.body, token); result.Code != 403 || !strings.Contains(result.Body.String(), "Tutor is not assigned") {
				t.Fatalf("unassigned status=%d body=%s", result.Code, result.Body.String())
			}
			if tc.attendance {
				a.createErr = nil
			} else {
				r.createErr = nil
			}
			if result := routeRequest(engine, http.MethodPost, tc.path, tc.body, token); result.Code != 201 {
				t.Fatalf("assigned status=%d body=%s", result.Code, result.Body.String())
			}
		})
	}
}

func TestAttendanceReportRoutesRejectMissingMembershipAndValidateAfterPermission(t *testing.T) {
	p := &routePermissionClient{permissions: map[string]bool{"attendance:create": true, "report:create": true}}
	a, r := &routeAttendanceUsecase{}, &routeReportUsecase{}
	engine, claims := routeFixture(t, p, a, r)
	claims.MemberID = ""
	if response := routeRequest(engine, http.MethodPost, "/api/v1/attendance", `{}`, routeToken(t, claims)); response.Code != 403 || len(p.calls) != 0 || a.calls != 0 {
		t.Fatalf("missing member status=%d identity=%v usecase=%d", response.Code, p.calls, a.calls)
	}
	claims.MemberID = uuid.NewString()
	for _, path := range []string{"/api/v1/attendance", "/api/v1/reports"} {
		response := routeRequest(engine, http.MethodPost, path, `{}`, routeToken(t, claims))
		if response.Code != 400 {
			t.Fatalf("%s validation status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	if a.calls != 0 || r.calls != 0 {
		t.Fatalf("invalid input invoked usecases: %d,%d", a.calls, r.calls)
	}
}
