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

// KEL-19: tenant context must come from the verified JWT claim only. These
// tests reproduce the exact spoofing scenario: a client sends X-Tenant-ID while
// the signed token carries a different (or no) tenant. The header must never be
// trusted, and no tenant-scoped data may be read or written.

const testJWTSecret = "kel19-tenant-claim-only"

// spoofedTenantHeader is deliberately a different, valid-looking UUID so a
// handler that still honoured the header would visibly succeed.
var spoofedTenantHeader = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func signToken(t *testing.T, claims middleware.Claims) string {
	t.Helper()
	claims.RegisteredClaims = jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

type recordingCategoryUsecase struct {
	calls  int
	tenant uuid.UUID
}

func (m *recordingCategoryUsecase) CreateCategory(context.Context, uuid.UUID, *domain.CreateCategoryRequest) (*domain.CategoryResponse, error) {
	return nil, nil
}

func (m *recordingCategoryUsecase) ListCategories(_ context.Context, tenantID uuid.UUID, _ domain.ListQuery) (*domain.CategoryListResponse, error) {
	m.calls++
	m.tenant = tenantID
	return &domain.CategoryListResponse{Items: []domain.CategoryResponse{}}, nil
}

func (m *recordingCategoryUsecase) DeleteCategory(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

type recordingClassUsecase struct {
	calls  int
	tenant uuid.UUID
}

func (m *recordingClassUsecase) CreateClass(context.Context, uuid.UUID, *domain.CreateClassRequest) (*domain.ClassResponse, error) {
	return nil, nil
}

func (m *recordingClassUsecase) ListClasses(_ context.Context, tenantID uuid.UUID, _ domain.ListQuery) (*domain.ClassListResponse, error) {
	m.calls++
	m.tenant = tenantID
	return &domain.ClassListResponse{Items: []domain.ClassResponse{}}, nil
}

func (m *recordingClassUsecase) DeleteClass(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (m *recordingClassUsecase) UpdateClassPublication(context.Context, uuid.UUID, uuid.UUID, *domain.UpdateClassPublicationRequest) (*domain.ClassResponse, error) {
	return nil, nil
}

func (m *recordingClassUsecase) UpdateClass(context.Context, uuid.UUID, uuid.UUID, *domain.UpdateClassRequest) (*domain.ClassResponse, error) {
	return nil, nil
}

type recordingScheduleUsecase struct {
	calls  int
	tenant uuid.UUID
}

func (m *recordingScheduleUsecase) ListSchedules(_ context.Context, tenantID uuid.UUID, _ domain.ListQuery) (*domain.ScheduleListResponse, error) {
	m.calls++
	m.tenant = tenantID
	return &domain.ScheduleListResponse{Items: []domain.ClassSchedule{}}, nil
}

func (m *recordingScheduleUsecase) DeleteSchedule(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (m *recordingScheduleUsecase) ListSessions(context.Context, uuid.UUID, domain.SessionQuery) (*domain.SessionListResponse, error) {
	return nil, nil
}

func (m *recordingScheduleUsecase) GetSession(context.Context, uuid.UUID, uuid.UUID) (*domain.ClassSession, error) {
	return nil, nil
}

func (m *recordingScheduleUsecase) DeleteSession(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (m *recordingScheduleUsecase) CreateInitialSchedules(context.Context, uuid.UUID, *domain.CreateInitialSchedulesRequest) (*domain.CreateInitialSchedulesResponse, error) {
	return nil, nil
}

func (m *recordingScheduleUsecase) RescheduleSession(context.Context, uuid.UUID, *domain.RescheduleSessionRequest) (*domain.RescheduleSessionResponse, error) {
	return nil, nil
}

func (m *recordingScheduleUsecase) ChangeSchedulePermanent(context.Context, uuid.UUID, *domain.PermanentScheduleChangeRequest) (*domain.PermanentScheduleChangeResponse, error) {
	return nil, nil
}

func (m *recordingScheduleUsecase) ChangeTutorTemporary(context.Context, uuid.UUID, *domain.SubstituteTutorRequest) (*domain.SubstituteTutorResponse, error) {
	return nil, nil
}

func (m *recordingScheduleUsecase) ChangeTutorPermanent(context.Context, uuid.UUID, *domain.PermanentTutorChangeRequest) (*domain.PermanentTutorChangeResponse, error) {
	return nil, nil
}

func (m *recordingScheduleUsecase) GetSessionAttendees(context.Context, uuid.UUID, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, nil
}

type recordingEnrollmentUsecase struct {
	enrollCalls  int
	enrollTenant uuid.UUID
	publicCalls  int
}

func (m *recordingEnrollmentUsecase) EnrollStudent(_ context.Context, tenantID uuid.UUID, _ *domain.EnrollStudentRequest) (*domain.EnrollmentResponse, error) {
	m.enrollCalls++
	m.enrollTenant = tenantID
	return &domain.EnrollmentResponse{ID: uuid.New(), TenantID: tenantID}, nil
}

func (m *recordingEnrollmentUsecase) EnrollPublic(context.Context, uuid.UUID, uuid.UUID, *domain.PublicEnrollmentRequest, string) (*domain.PublicEnrollmentResponse, error) {
	m.publicCalls++
	return &domain.PublicEnrollmentResponse{}, nil
}

func (m *recordingEnrollmentUsecase) ActivateEnrollment(context.Context, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func (m *recordingEnrollmentUsecase) ReleaseEnrollment(context.Context, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func (m *recordingEnrollmentUsecase) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) (*domain.EnrollmentListResponse, error) {
	return nil, nil
}

func (m *recordingEnrollmentUsecase) GetByID(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func (m *recordingEnrollmentUsecase) AssignSchedule(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func (m *recordingEnrollmentUsecase) CancelPendingEnrollment(context.Context, uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

// tenantScopedRouter mirrors the real route table in cmd/server/main.go: the
// authenticated group is built from AuthMiddleware and carries the list and
// enrollment handlers that used to fall back to the X-Tenant-ID header.
func tenantScopedRouter(category *recordingCategoryUsecase, class *recordingClassUsecase, schedule *recordingScheduleUsecase, enrollment *recordingEnrollmentUsecase) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	listHandler := NewListHandler(category, class, schedule)
	enrollmentHandler := NewEnrollmentHandler(enrollment)

	apiV1 := router.Group("/api/v1")
	apiV1.GET("/catalog/classes", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": nil})
	})
	apiV1.Use(middleware.AuthMiddleware(testJWTSecret))
	apiV1.GET("/categories", listHandler.ListCategories)
	apiV1.GET("/classes", listHandler.ListClasses)
	apiV1.GET("/schedules", listHandler.ListSchedules)
	apiV1.POST("/tenants/:tenant_id/enrollments", enrollmentHandler.Create)
	return router
}

func doJSONRequest(router *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// The spoofing attempt: every request carries a client-controlled tenant
	// header that must be ignored in favour of the verified claim.
	req.Header.Set("X-Tenant-ID", spoofedTenantHeader.String())
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

// AC1: a parent token (which legitimately carries no tenant claim) must get 403
// on every tenant-scoped read endpoint, with no tenant data read.
func TestParentTokenWithSpoofedTenantHeaderIsForbidden(t *testing.T) {
	parentToken := signToken(t, middleware.Claims{UserID: uuid.New().String(), IsParent: true})
	category, class, schedule := &recordingCategoryUsecase{}, &recordingClassUsecase{}, &recordingScheduleUsecase{}
	enrollment := &recordingEnrollmentUsecase{}
	router := tenantScopedRouter(category, class, schedule, enrollment)

	for _, path := range []string{"/api/v1/categories", "/api/v1/classes", "/api/v1/schedules"} {
		t.Run(path, func(t *testing.T) {
			res := doJSONRequest(router, http.MethodGet, path, parentToken, "")
			if res.Code != http.StatusForbidden {
				t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusForbidden, res.Body.String())
			}
			if strings.Contains(res.Body.String(), spoofedTenantHeader.String()) {
				t.Fatalf("response leaked the spoofed tenant: %s", res.Body.String())
			}
		})
	}

	if category.calls != 0 || class.calls != 0 || schedule.calls != 0 {
		t.Fatalf("spoofed header reached the use case: categories=%d classes=%d schedules=%d",
			category.calls, class.calls, schedule.calls)
	}
}

// The parent catalogue flow is public and must keep working unchanged.
func TestParentTokenCanStillUsePublicCatalog(t *testing.T) {
	parentToken := signToken(t, middleware.Claims{UserID: uuid.New().String(), IsParent: true})
	router := tenantScopedRouter(&recordingCategoryUsecase{}, &recordingClassUsecase{}, &recordingScheduleUsecase{}, &recordingEnrollmentUsecase{})

	res := doJSONRequest(router, http.MethodGet, "/api/v1/catalog/classes", parentToken, "")
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusOK, res.Body.String())
	}
}

// AC2: a member whose claim names tenant A must be rejected with 403 when the
// path asks for tenant B, and the enrollment use case must never run.
func TestCrossTenantEnrollmentIsForbidden(t *testing.T) {
	ownTenant, otherTenant := uuid.New(), uuid.New()
	token := signToken(t, middleware.Claims{UserID: uuid.New().String(), TenantID: ownTenant.String()})
	enrollment := &recordingEnrollmentUsecase{}
	router := tenantScopedRouter(&recordingCategoryUsecase{}, &recordingClassUsecase{}, &recordingScheduleUsecase{}, enrollment)

	body := `{"student_id":"` + uuid.New().String() + `","class_id":"` + uuid.New().String() + `","billing_cycle":"monthly"}`
	res := doJSONRequest(router, http.MethodPost, "/api/v1/tenants/"+otherTenant.String()+"/enrollments", token, body)

	if res.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusForbidden, res.Body.String())
	}
	if enrollment.enrollCalls != 0 {
		t.Fatalf("enrollment was created for another tenant (calls=%d)", enrollment.enrollCalls)
	}
}

// The spoofed header must not be able to talk a tenant into its own path either.
func TestSpoofedHeaderCannotAuthoriseCrossTenantEnrollment(t *testing.T) {
	ownTenant, otherTenant := uuid.New(), uuid.New()
	// Claim says tenant A while the header claims the spoofed tenant C; the path
	// asks for tenant B. Nothing may match, so the call must fail closed.
	token := signToken(t, middleware.Claims{UserID: uuid.New().String(), TenantID: ownTenant.String()})
	enrollment := &recordingEnrollmentUsecase{}
	router := tenantScopedRouter(&recordingCategoryUsecase{}, &recordingClassUsecase{}, &recordingScheduleUsecase{}, enrollment)

	body := `{"student_id":"` + uuid.New().String() + `","class_id":"` + uuid.New().String() + `","billing_cycle":"monthly"}`
	res := doJSONRequest(router, http.MethodPost, "/api/v1/tenants/"+otherTenant.String()+"/enrollments", token, body)

	if res.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusForbidden, res.Body.String())
	}
	if enrollment.enrollCalls != 0 || enrollment.publicCalls != 0 {
		t.Fatalf("use case ran despite tenant mismatch: enroll=%d public=%d", enrollment.enrollCalls, enrollment.publicCalls)
	}
}

// AC3: a member with a valid tenant claim keeps full catalogue and enrollment
// access on its own tenant, even while sending the spoofed header.
func TestValidTenantClaimStillHasFullAccess(t *testing.T) {
	ownTenant := uuid.New()
	token := signToken(t, middleware.Claims{UserID: uuid.New().String(), TenantID: ownTenant.String()})
	category, class, schedule := &recordingCategoryUsecase{}, &recordingClassUsecase{}, &recordingScheduleUsecase{}
	enrollment := &recordingEnrollmentUsecase{}
	router := tenantScopedRouter(category, class, schedule, enrollment)

	for _, path := range []string{"/api/v1/categories", "/api/v1/classes", "/api/v1/schedules"} {
		res := doJSONRequest(router, http.MethodGet, path, token, "")
		if res.Code != http.StatusOK {
			t.Fatalf("%s status=%d want=%d body=%s", path, res.Code, http.StatusOK, res.Body.String())
		}
	}
	if category.calls != 1 || class.calls != 1 || schedule.calls != 1 {
		t.Fatalf("use case calls=(%d,%d,%d) want=(1,1,1)", category.calls, class.calls, schedule.calls)
	}
	// The claim wins over the header for every scoped read.
	if category.tenant != ownTenant || class.tenant != ownTenant || schedule.tenant != ownTenant {
		t.Fatalf("resolved tenant=(%v,%v,%v) want=%v", category.tenant, class.tenant, schedule.tenant, ownTenant)
	}

	body := `{"student_id":"` + uuid.New().String() + `","class_id":"` + uuid.New().String() + `","billing_cycle":"monthly"}`
	res := doJSONRequest(router, http.MethodPost, "/api/v1/tenants/"+ownTenant.String()+"/enrollments", token, body)
	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusCreated, res.Body.String())
	}
	if enrollment.enrollCalls != 1 || enrollment.enrollTenant != ownTenant {
		t.Fatalf("enroll calls=%d tenant=%v want tenant=%v", enrollment.enrollCalls, enrollment.enrollTenant, ownTenant)
	}
}

// A parent token must keep using the existing EnrollPublic flow: the tenant path
// segment is irrelevant for parents and the tenant-scoped branch must not run.
func TestParentEnrollmentStillUsesPublicFlow(t *testing.T) {
	parentID := uuid.New()
	token := signToken(t, middleware.Claims{UserID: parentID.String(), IsParent: true})
	enrollment := &recordingEnrollmentUsecase{}
	router := tenantScopedRouter(&recordingCategoryUsecase{}, &recordingClassUsecase{}, &recordingScheduleUsecase{}, enrollment)

	body := `{"student_id":"` + uuid.New().String() + `","class_id":"` + uuid.New().String() + `","billing_cycle":"monthly"}`
	res := doJSONRequest(router, http.MethodPost, "/api/v1/tenants/"+uuid.New().String()+"/enrollments", token, body)

	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusCreated, res.Body.String())
	}
	if enrollment.publicCalls != 1 || enrollment.enrollCalls != 0 {
		t.Fatalf("public=%d enroll=%d want public=1 enroll=0", enrollment.publicCalls, enrollment.enrollCalls)
	}
}

func TestTenantIDFromContextIgnoresHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	claimTenant := uuid.New()
	tests := []struct {
		name       string
		claim      string
		wantErr    error
		wantTenant uuid.UUID
	}{
		{name: "valid claim wins over header", claim: claimTenant.String(), wantTenant: claimTenant},
		{name: "missing claim is forbidden even with header", wantErr: errTenantContextMissing},
		{name: "zero uuid claim is invalid", claim: uuid.Nil.String(), wantErr: errTenantContextInvalid},
		{name: "malformed claim is invalid", claim: "not-a-uuid", wantErr: errTenantContextInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/categories", nil)
			c.Request.Header.Set("X-Tenant-ID", spoofedTenantHeader.String())
			if tc.claim != "" {
				c.Set("tenant_id", tc.claim)
			}
			got, err := tenantIDFromContext(c)
			if err != tc.wantErr {
				t.Fatalf("err=%v want=%v", err, tc.wantErr)
			}
			if tc.wantErr == nil && got != tc.wantTenant {
				t.Fatalf("tenant=%v want=%v", got, tc.wantTenant)
			}
			if tc.wantErr == nil && got == spoofedTenantHeader {
				t.Fatalf("header value leaked into tenant context")
			}
		})
	}
}

func TestWriteTenantErrorStatusMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "missing context is forbidden", err: errTenantContextMissing, status: http.StatusForbidden},
		{name: "invalid context is unauthorized", err: errTenantContextInvalid, status: http.StatusUnauthorized},
		{name: "unknown error fails closed", err: jwt.ErrTokenMalformed, status: http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			writeTenantError(c, tc.err)
			if recorder.Code != tc.status {
				t.Fatalf("status=%d want=%d", recorder.Code, tc.status)
			}
		})
	}
}
