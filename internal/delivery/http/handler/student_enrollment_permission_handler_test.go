package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

// KEL-21: student and enrollment routes are shared by tenant members and
// parents. These tests mirror the production route table so the route-level
// contract is verified where it is actually enforced: a non-parent caller needs
// the matching student or enrollment permission, and a parent caller keeps using
// the existing ownership rules without an identity lookup.
type studentPermissionClientStub struct {
	permissions []string
	tenants     []string
	roles       []string
	allowed     bool
	err         error
}

func (s *studentPermissionClientStub) CheckPermission(_ context.Context, tenantID, roleID, permission string) (bool, error) {
	s.permissions = append(s.permissions, permission)
	s.tenants = append(s.tenants, tenantID)
	s.roles = append(s.roles, roleID)
	if s.err != nil {
		return false, s.err
	}
	return s.allowed, nil
}

func (*studentPermissionClientStub) Close() error { return nil }

var _ grpcclient.PermissionClient = (*studentPermissionClientStub)(nil)

// studentPermissionUsecase records every student operation the handler forwards
// so a denied caller can be proven to have caused no data change.
type studentPermissionUsecase struct {
	listCalls   int
	createCalls int
	getCalls    int
	updateCalls int
	deleteCalls int
	tenantID    *uuid.UUID
	parentID    *uuid.UUID
	err         error
}

func (m *studentPermissionUsecase) List(_ context.Context, tenantID, parentID *uuid.UUID, query domain.StudentQuery) (*domain.StudentListResponse, error) {
	m.listCalls++
	m.tenantID, m.parentID = tenantID, parentID
	if m.err != nil {
		return nil, m.err
	}
	return &domain.StudentListResponse{Items: []domain.Student{}, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize}}, nil
}

func (m *studentPermissionUsecase) Create(context.Context, *uuid.UUID, *uuid.UUID, *domain.CreateStudentRequest) (*domain.Student, error) {
	m.createCalls++
	if m.err != nil {
		return nil, m.err
	}
	return &domain.Student{ID: uuid.New()}, nil
}

func (m *studentPermissionUsecase) GetByID(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.Student, error) {
	m.getCalls++
	if m.err != nil {
		return nil, m.err
	}
	return &domain.Student{ID: uuid.New()}, nil
}

func (m *studentPermissionUsecase) Update(context.Context, *uuid.UUID, *uuid.UUID, *uuid.UUID, uuid.UUID, *domain.UpdateStudentRequest) (*domain.Student, error) {
	m.updateCalls++
	if m.err != nil {
		return nil, m.err
	}
	return &domain.Student{ID: uuid.New()}, nil
}

func (m *studentPermissionUsecase) Delete(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) error {
	m.deleteCalls++
	return m.err
}

// studentEnrollmentPermissionUsecase records enrollment operations the same way.
type studentEnrollmentPermissionUsecase struct {
	recordingEnrollmentUsecase
	listCalls int
	getCalls  int
	err       error
}

func (m *studentEnrollmentPermissionUsecase) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) (*domain.EnrollmentListResponse, error) {
	m.listCalls++
	if m.err != nil {
		return nil, m.err
	}
	return &domain.EnrollmentListResponse{Items: []*domain.EnrollmentResponse{}, Pagination: domain.Pagination{Page: 1, PageSize: 20}}, nil
}

func (m *studentEnrollmentPermissionUsecase) GetByID(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	m.getCalls++
	if m.err != nil {
		return nil, m.err
	}
	return &domain.EnrollmentResponse{ID: uuid.New()}, nil
}

// studentPermissionRouter mirrors the production route table for the student and
// enrollment routes added by KEL-21, with AuthMiddleware in front so the route
// level permission decision is exercised exactly as in cmd/server/main.go.
func studentPermissionRouter(student *studentPermissionUsecase, enrollment *studentEnrollmentPermissionUsecase, client grpcclient.PermissionClient) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	studentHandler := NewStudentHandler(student)
	enrollmentHandler := NewEnrollmentHandler(enrollment)

	apiV1 := router.Group("/api/v1")
	apiV1.GET("/catalog/classes", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": nil})
	})
	apiV1.Use(middleware.AuthMiddleware(testJWTSecret))
	apiV1.GET("/students", middleware.RequirePermissionUnlessParent(client, "student:read"), studentHandler.List)
	apiV1.POST("/students", middleware.RequirePermissionUnlessParent(client, "student:create"), studentHandler.Create)
	apiV1.GET("/students/:id", middleware.RequirePermissionUnlessParent(client, "student:read"), studentHandler.Get)
	apiV1.PATCH("/students/:id", middleware.RequirePermissionUnlessParent(client, "student:update"), studentHandler.Update)
	apiV1.DELETE("/students/:id", middleware.RequirePermissionUnlessParent(client, "student:delete"), studentHandler.Delete)
	apiV1.POST("/tenants/:tenant_id/enrollments", middleware.RequirePermissionUnlessParent(client, "enrollment:create"), enrollmentHandler.Create)
	apiV1.POST("/catalog/classes/:class_id/enrollments", enrollmentHandler.CreateCatalogEnrollment)
	apiV1.GET("/enrollments", middleware.RequirePermissionUnlessParent(client, "enrollment:read"), enrollmentHandler.ListQuery)
	apiV1.GET("/enrollments/:id", middleware.RequirePermissionUnlessParent(client, "enrollment:read"), enrollmentHandler.GetQuery)
	return router
}

func studentBody() string {
	return `{"parent_id":"` + uuid.New().String() + `","first_name":"Anak","date_of_birth":"2015-04-01"}`
}

func enrollmentBody() string {
	return `{"student_id":"` + uuid.New().String() + `","class_id":"` + uuid.New().String() + `","billing_cycle":"monthly"}`
}

// publicEnrollmentBody matches the payload the parent catalogue route expects:
// the class is taken from the path, not the body.
func publicEnrollmentBody() string {
	return `{"student_id":"` + uuid.New().String() + `","billing_cycle":"monthly"}`
}

// AC1: a role without the student or enrollment permission is denied on every
// guarded route and no use-case call happens, so a denied request cannot change
// data. This is the role that lacks the privileges, e.g. a teacher.
func TestStudentAndEnrollmentPermissionDenied(t *testing.T) {
	tenantID, roleID := uuid.New(), uuid.New()
	token := signToken(t, middleware.Claims{UserID: uuid.New().String(), TenantID: tenantID.String(), RoleID: roleID.String()})
	studentID, enrollmentID := uuid.New(), uuid.New()

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		permission string
	}{
		{name: "list students", method: http.MethodGet, path: "/api/v1/students", permission: "student:read"},
		{name: "create student", method: http.MethodPost, path: "/api/v1/students", body: studentBody(), permission: "student:create"},
		{name: "get student", method: http.MethodGet, path: "/api/v1/students/" + studentID.String(), permission: "student:read"},
		{name: "update student", method: http.MethodPatch, path: "/api/v1/students/" + studentID.String(), body: `{"first_name":"Baru"}`, permission: "student:update"},
		{name: "delete student", method: http.MethodDelete, path: "/api/v1/students/" + studentID.String(), permission: "student:delete"},
		{name: "create enrollment", method: http.MethodPost, path: "/api/v1/tenants/" + tenantID.String() + "/enrollments", body: enrollmentBody(), permission: "enrollment:create"},
		{name: "list enrollments", method: http.MethodGet, path: "/api/v1/enrollments", permission: "enrollment:read"},
		{name: "get enrollment", method: http.MethodGet, path: "/api/v1/enrollments/" + enrollmentID.String(), permission: "enrollment:read"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &studentPermissionClientStub{allowed: false}
			student, enrollment := &studentPermissionUsecase{}, &studentEnrollmentPermissionUsecase{}
			router := studentPermissionRouter(student, enrollment, client)

			res := doJSONRequest(router, tc.method, tc.path, token, tc.body)

			if res.Code != http.StatusForbidden {
				t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusForbidden, res.Body.String())
			}
			// The permission question must be asked for the role and tenant of the
			// verified claim, never for a client-controlled value.
			if len(client.permissions) != 1 || client.permissions[0] != tc.permission {
				t.Fatalf("permission lookups=%v want=[%s]", client.permissions, tc.permission)
			}
			if client.tenants[0] != tenantID.String() || client.roles[0] != roleID.String() {
				t.Fatalf("lookup scope=(%s,%s) want=(%s,%s)", client.tenants[0], client.roles[0], tenantID, roleID)
			}
			if student.listCalls+student.createCalls+student.getCalls+student.updateCalls+student.deleteCalls != 0 {
				t.Fatalf("denied request still reached the student use case: %+v", student)
			}
			if enrollment.listCalls+enrollment.getCalls+enrollment.enrollCalls+enrollment.publicCalls != 0 {
				t.Fatalf("denied request still reached the enrollment use case: %+v", enrollment)
			}
		})
	}
}

// AC2: a role that holds the permission keeps its existing behaviour on every
// guarded route.
func TestStudentAndEnrollmentPermissionAllowed(t *testing.T) {
	tenantID, roleID := uuid.New(), uuid.New()
	token := signToken(t, middleware.Claims{UserID: uuid.New().String(), TenantID: tenantID.String(), RoleID: roleID.String()})
	studentID, enrollmentID := uuid.New(), uuid.New()

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		permission string
		wantStatus int
	}{
		{name: "list students", method: http.MethodGet, path: "/api/v1/students", permission: "student:read", wantStatus: http.StatusOK},
		{name: "create student", method: http.MethodPost, path: "/api/v1/students", body: studentBody(), permission: "student:create", wantStatus: http.StatusCreated},
		{name: "get student", method: http.MethodGet, path: "/api/v1/students/" + studentID.String(), permission: "student:read", wantStatus: http.StatusOK},
		{name: "update student", method: http.MethodPatch, path: "/api/v1/students/" + studentID.String(), body: `{"first_name":"Baru","date_of_birth":"2015-04-01"}`, permission: "student:update", wantStatus: http.StatusOK},
		{name: "delete student", method: http.MethodDelete, path: "/api/v1/students/" + studentID.String(), permission: "student:delete", wantStatus: http.StatusOK},
		{name: "create enrollment", method: http.MethodPost, path: "/api/v1/tenants/" + tenantID.String() + "/enrollments", body: enrollmentBody(), permission: "enrollment:create", wantStatus: http.StatusCreated},
		{name: "list enrollments", method: http.MethodGet, path: "/api/v1/enrollments", permission: "enrollment:read", wantStatus: http.StatusOK},
		{name: "get enrollment", method: http.MethodGet, path: "/api/v1/enrollments/" + enrollmentID.String(), permission: "enrollment:read", wantStatus: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &studentPermissionClientStub{allowed: true}
			student, enrollment := &studentPermissionUsecase{}, &studentEnrollmentPermissionUsecase{}
			router := studentPermissionRouter(student, enrollment, client)

			res := doJSONRequest(router, tc.method, tc.path, token, tc.body)

			if res.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", res.Code, tc.wantStatus, res.Body.String())
			}
			if len(client.permissions) != 1 || client.permissions[0] != tc.permission {
				t.Fatalf("permission lookups=%v want=[%s]", client.permissions, tc.permission)
			}
		})
	}
}

// AC3: a parent token carries no role, so it must pass the permission gate and
// keep using the existing ownership rules even when identity cannot be reached.
func TestParentBypassesStudentAndEnrollmentPermission(t *testing.T) {
	parentID := uuid.New()
	token := signToken(t, middleware.Claims{UserID: parentID.String(), IsParent: true})
	studentID := uuid.New()

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "list students", method: http.MethodGet, path: "/api/v1/students", wantStatus: http.StatusOK},
		{name: "get student", method: http.MethodGet, path: "/api/v1/students/" + studentID.String(), wantStatus: http.StatusOK},
		{name: "list enrollments", method: http.MethodGet, path: "/api/v1/enrollments", wantStatus: http.StatusOK},
		{name: "get enrollment", method: http.MethodGet, path: "/api/v1/enrollments/" + uuid.New().String(), wantStatus: http.StatusOK},
		{name: "create enrollment", method: http.MethodPost, path: "/api/v1/tenants/" + uuid.New().String() + "/enrollments", body: enrollmentBody(), wantStatus: http.StatusCreated},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The identity stub fails every call: a parent must never trigger one.
			client := &studentPermissionClientStub{err: errors.New("identity unavailable")}
			student, enrollment := &studentPermissionUsecase{}, &studentEnrollmentPermissionUsecase{}
			router := studentPermissionRouter(student, enrollment, client)

			res := doJSONRequest(router, tc.method, tc.path, token, tc.body)

			if res.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", res.Code, tc.wantStatus, res.Body.String())
			}
			if len(client.permissions) != 0 {
				t.Fatalf("parent triggered identity lookups %v", client.permissions)
			}
		})
	}

	// The parent scope must still be the authenticated parent, not a tenant.
	student := &studentPermissionUsecase{}
	router := studentPermissionRouter(student, &studentEnrollmentPermissionUsecase{}, &studentPermissionClientStub{err: errors.New("identity unavailable")})
	res := doJSONRequest(router, http.MethodGet, "/api/v1/students", token, "")
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if student.tenantID != nil || student.parentID == nil || *student.parentID != parentID {
		t.Fatalf("parent scope=(tenant=%v parent=%v) want parent=%v", student.tenantID, student.parentID, parentID)
	}
}

// The parent catalogue flow is public and must keep working unchanged.
// Edge case: a parent token that also carries a tenant membership and a role
// still counts as a parent, so the ownership rules must stay authoritative and
// identity must not be consulted for those routes.
func TestParentWithTenantMembershipBypassesPermission(t *testing.T) {
	parentID := uuid.New()
	token := signToken(t, middleware.Claims{
		UserID:   parentID.String(),
		IsParent: true,
		TenantID: uuid.New().String(),
		RoleID:   uuid.New().String(),
	})
	studentID := uuid.New()

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "list students", method: http.MethodGet, path: "/api/v1/students", wantStatus: http.StatusOK},
		{name: "get student", method: http.MethodGet, path: "/api/v1/students/" + studentID.String(), wantStatus: http.StatusOK},
		{name: "list enrollments", method: http.MethodGet, path: "/api/v1/enrollments", wantStatus: http.StatusOK},
		{name: "get enrollment", method: http.MethodGet, path: "/api/v1/enrollments/" + uuid.New().String(), wantStatus: http.StatusOK},
		{name: "create enrollment", method: http.MethodPost, path: "/api/v1/tenants/" + uuid.New().String() + "/enrollments", body: enrollmentBody(), wantStatus: http.StatusCreated},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &studentPermissionClientStub{allowed: false}
			student, enrollment := &studentPermissionUsecase{}, &studentEnrollmentPermissionUsecase{}
			router := studentPermissionRouter(student, enrollment, client)

			res := doJSONRequest(router, tc.method, tc.path, token, tc.body)

			if res.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", res.Code, tc.wantStatus, res.Body.String())
			}
			if len(client.permissions) != 0 {
				t.Fatalf("parent with a tenant membership triggered identity lookups %v", client.permissions)
			}
		})
	}

	// The parent branch must win over the tenant branch, so the operation is
	// scoped to the parent and not to the tenant claim.
	student := &studentPermissionUsecase{}
	enrollment := &studentEnrollmentPermissionUsecase{}
	router := studentPermissionRouter(student, enrollment, &studentPermissionClientStub{allowed: false})
	res := doJSONRequest(router, http.MethodGet, "/api/v1/students", token, "")
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if student.tenantID != nil || student.parentID == nil || *student.parentID != parentID {
		t.Fatalf("parent scope=(tenant=%v parent=%v) want parent=%v", student.tenantID, student.parentID, parentID)
	}

	res = doJSONRequest(router, http.MethodPost, "/api/v1/tenants/"+uuid.New().String()+"/enrollments", token, enrollmentBody())
	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusCreated, res.Body.String())
	}
	if enrollment.publicCalls != 1 || enrollment.enrollCalls != 0 {
		t.Fatalf("enrollment routing public=%d tenant=%d want public=1 tenant=0", enrollment.publicCalls, enrollment.enrollCalls)
	}
}

func TestParentCatalogAccessUnaffectedByPermission(t *testing.T) {
	token := signToken(t, middleware.Claims{UserID: uuid.New().String(), IsParent: true})
	client := &studentPermissionClientStub{err: errors.New("identity unavailable")}
	router := studentPermissionRouter(&studentPermissionUsecase{}, &studentEnrollmentPermissionUsecase{}, client)

	res := doJSONRequest(router, http.MethodGet, "/api/v1/catalog/classes", token, "")
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if len(client.permissions) != 0 {
		t.Fatalf("public catalogue route consulted identity: %v", client.permissions)
	}
}

// Edge case: a tenant member whose token carries no role_id cannot be
// authorized, so the route must deny it before identity is consulted and no
// data may change.
func TestTenantMemberWithoutRoleIDIsDenied(t *testing.T) {
	tenantID := uuid.New()
	token := signToken(t, middleware.Claims{UserID: uuid.New().String(), TenantID: tenantID.String()})

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list students", method: http.MethodGet, path: "/api/v1/students"},
		{name: "create student", method: http.MethodPost, path: "/api/v1/students", body: studentBody()},
		{name: "update student", method: http.MethodPatch, path: "/api/v1/students/" + uuid.New().String(), body: `{"first_name":"Baru","date_of_birth":"2015-04-01"}`},
		{name: "delete student", method: http.MethodDelete, path: "/api/v1/students/" + uuid.New().String()},
		{name: "list enrollments", method: http.MethodGet, path: "/api/v1/enrollments"},
		{name: "create enrollment", method: http.MethodPost, path: "/api/v1/tenants/" + tenantID.String() + "/enrollments", body: enrollmentBody()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &studentPermissionClientStub{allowed: true}
			student, enrollment := &studentPermissionUsecase{}, &studentEnrollmentPermissionUsecase{}
			router := studentPermissionRouter(student, enrollment, client)

			res := doJSONRequest(router, tc.method, tc.path, token, tc.body)

			if res.Code != http.StatusForbidden {
				t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusForbidden, res.Body.String())
			}
			if len(client.permissions) != 0 {
				t.Fatalf("member without role_id reached identity: %v", client.permissions)
			}
			if student.listCalls+student.createCalls+student.getCalls+student.updateCalls+student.deleteCalls != 0 {
				t.Fatalf("member without role_id reached the student use case: %+v", student)
			}
			if enrollment.listCalls+enrollment.getCalls+enrollment.enrollCalls+enrollment.publicCalls != 0 {
				t.Fatalf("member without role_id reached the enrollment use case: %+v", enrollment)
			}
		})
	}
}

// AC4: an unusable permission decision fails closed with 503, matching the
// catalogue mutations, and no data is touched.
func TestStudentAndEnrollmentPermissionIdentityUnavailable(t *testing.T) {
	tenantID, roleID := uuid.New(), uuid.New()
	token := signToken(t, middleware.Claims{UserID: uuid.New().String(), TenantID: tenantID.String(), RoleID: roleID.String()})

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list students", method: http.MethodGet, path: "/api/v1/students"},
		{name: "delete student", method: http.MethodDelete, path: "/api/v1/students/" + uuid.New().String()},
		{name: "create enrollment", method: http.MethodPost, path: "/api/v1/tenants/" + tenantID.String() + "/enrollments", body: enrollmentBody()},
		{name: "list enrollments", method: http.MethodGet, path: "/api/v1/enrollments"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &studentPermissionClientStub{err: errors.New("identity unavailable")}
			student, enrollment := &studentPermissionUsecase{}, &studentEnrollmentPermissionUsecase{}
			router := studentPermissionRouter(student, enrollment, client)

			res := doJSONRequest(router, tc.method, tc.path, token, tc.body)

			if res.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusServiceUnavailable, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), "Authorization service unavailable") {
				t.Fatalf("unexpected body=%s", res.Body.String())
			}
			if student.listCalls+student.createCalls+student.getCalls+student.updateCalls+student.deleteCalls != 0 {
				t.Fatalf("student use case ran while identity was unavailable: %+v", student)
			}
			if enrollment.listCalls+enrollment.getCalls+enrollment.enrollCalls+enrollment.publicCalls != 0 {
				t.Fatalf("enrollment use case ran while identity was unavailable: %+v", enrollment)
			}
		})
	}
}

// The routes that were already parent-only stay untouched: a parent still uses
// the public flow and a tenant member is still rejected by the handler itself.
func TestCatalogEnrollmentRouteStaysParentOnly(t *testing.T) {
	classID := uuid.New()
	parentID := uuid.New()
	parentToken := signToken(t, middleware.Claims{UserID: parentID.String(), IsParent: true})
	client := &studentPermissionClientStub{err: errors.New("identity unavailable")}
	enrollment := &studentEnrollmentPermissionUsecase{}
	router := studentPermissionRouter(&studentPermissionUsecase{}, enrollment, client)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/catalog/classes/"+classID.String()+"/enrollments", strings.NewReader(publicEnrollmentBody()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+parentToken)
	req.Header.Set("Idempotency-Key", uuid.New().String())
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusCreated, res.Body.String())
	}
	if enrollment.publicCalls != 1 {
		t.Fatalf("public enrollment calls=%d want=1", enrollment.publicCalls)
	}
	if len(client.permissions) != 0 {
		t.Fatalf("parent-only route consulted identity: %v", client.permissions)
	}
}
