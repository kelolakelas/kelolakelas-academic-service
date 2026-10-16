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
)

type updateUsecaseMock struct {
	response *domain.ClassResponse
	err      error
	tenantID uuid.UUID
	classID  uuid.UUID
	request  *domain.UpdateClassRequest
}

func (m *updateUsecaseMock) CreateClass(context.Context, uuid.UUID, *domain.CreateClassRequest) (*domain.ClassResponse, error) {
	return nil, nil
}
func (m *updateUsecaseMock) ListClasses(context.Context, uuid.UUID, domain.ListQuery) (*domain.ClassListResponse, error) {
	return nil, nil
}
func (m *updateUsecaseMock) DeleteClass(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (m *updateUsecaseMock) UpdateClassPublication(context.Context, uuid.UUID, uuid.UUID, *domain.UpdateClassPublicationRequest) (*domain.ClassResponse, error) {
	return nil, nil
}
func (m *updateUsecaseMock) UpdateClass(_ context.Context, tenantID, id uuid.UUID, req *domain.UpdateClassRequest) (*domain.ClassResponse, error) {
	m.tenantID = tenantID
	m.classID = id
	m.request = req
	return m.response, m.err
}

type updateCreationUsecaseMock struct{}

func (updateCreationUsecaseMock) CreateClassWithCategory(context.Context, uuid.UUID, *domain.CreateClassWithCategoryRequest) (*domain.CreateClassWithCategoryResponse, error) {
	return nil, nil
}

func TestUpdateClassHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	classID, tenantID := uuid.New(), uuid.New()

	cases := []struct {
		name       string
		pathID     string
		tenant     string
		body       string
		usecaseErr error
		wantStatus int
	}{
		{name: "update name", pathID: classID.String(), tenant: tenantID.String(), body: `{"name":"Aljabar Dasar"}`, wantStatus: http.StatusOK},
		{name: "update price", pathID: classID.String(), tenant: tenantID.String(), body: `{"price":275000}`, wantStatus: http.StatusOK},
		{name: "price zero is accepted", pathID: classID.String(), tenant: tenantID.String(), body: `{"price":0}`, wantStatus: http.StatusOK},
		{name: "negative price rejected by binding", pathID: classID.String(), tenant: tenantID.String(), body: `{"price":-1}`, wantStatus: http.StatusBadRequest},
		{name: "unknown class type rejected by binding", pathID: classID.String(), tenant: tenantID.String(), body: `{"type":"hybrid"}`, wantStatus: http.StatusBadRequest},
		{name: "empty name rejected by binding", pathID: classID.String(), tenant: tenantID.String(), body: `{"name":""}`, wantStatus: http.StatusBadRequest},
		{name: "missing tenant", pathID: classID.String(), body: `{"name":"X"}`, wantStatus: http.StatusForbidden},
		{name: "invalid UUID", pathID: "not-a-uuid", tenant: tenantID.String(), body: `{"name":"X"}`, wantStatus: http.StatusBadRequest},
		{name: "malformed request body", pathID: classID.String(), tenant: tenantID.String(), body: `{"name":`, wantStatus: http.StatusBadRequest},
		{name: "class not found", pathID: classID.String(), tenant: tenantID.String(), body: `{"name":"X"}`, usecaseErr: domain.ErrClassNotFound, wantStatus: http.StatusNotFound},
		{name: "foreign tenant class is not found", pathID: classID.String(), tenant: tenantID.String(), body: `{"name":"X"}`, usecaseErr: domain.ErrClassForbidden, wantStatus: http.StatusForbidden},
		{name: "category not found", pathID: classID.String(), tenant: tenantID.String(), body: `{"category_id":"` + uuid.New().String() + `"}`, usecaseErr: domain.ErrCategoryNotFound, wantStatus: http.StatusUnprocessableEntity},
		{name: "category of another tenant", pathID: classID.String(), tenant: tenantID.String(), body: `{"category_id":"` + uuid.New().String() + `"}`, usecaseErr: domain.ErrCategoryForbidden, wantStatus: http.StatusUnprocessableEntity},
		{name: "class type immutable", pathID: classID.String(), tenant: tenantID.String(), body: `{"type":"private"}`, usecaseErr: domain.ErrClassTypeImmutable, wantStatus: http.StatusUnprocessableEntity},
		{name: "blank name", pathID: classID.String(), tenant: tenantID.String(), body: `{"name":"   "}`, usecaseErr: domain.ErrClassNameRequired, wantStatus: http.StatusUnprocessableEntity},
		{name: "invalid price", pathID: classID.String(), tenant: tenantID.String(), body: `{"name":"X"}`, usecaseErr: domain.ErrInvalidClassPrice, wantStatus: http.StatusUnprocessableEntity},
		{name: "repository error", pathID: classID.String(), tenant: tenantID.String(), body: `{"name":"X"}`, usecaseErr: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usecase := &updateUsecaseMock{response: &domain.ClassResponse{ID: classID, TenantID: tenantID}}
			usecase.err = tc.usecaseErr
			router := gin.New()
			handler := NewClassHandler(usecase, updateCreationUsecaseMock{})
			router.PATCH("/classes/:id", func(c *gin.Context) {
				// Mirrors the verified JWT claim that AuthMiddleware installs.
				// X-Tenant-ID is deliberately present but must be ignored.
				if tc.tenant != "" {
					c.Set("tenant_id", tc.tenant)
				}
				handler.Update(c)
			})
			req := httptest.NewRequest(http.MethodPatch, "/classes/"+tc.pathID, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Tenant-ID", "11111111-1111-1111-1111-111111111111")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			if res.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", res.Code, tc.wantStatus, res.Body.String())
			}
			if tc.wantStatus == http.StatusOK {
				if !strings.Contains(res.Body.String(), `"status":"success"`) {
					t.Fatalf("unexpected body: %s", res.Body.String())
				}
				if usecase.tenantID != tenantID || usecase.classID != classID {
					t.Fatalf("usecase got tenant=%s class=%s", usecase.tenantID, usecase.classID)
				}
			}
			if usecase.tenantID != uuid.Nil && usecase.tenantID.String() == "11111111-1111-1111-1111-111111111111" {
				t.Fatal("tenant must come from the verified claim, not the header")
			}
		})
	}
}

// TestUpdateClassHandlerIgnoresTenantHeader is a focused regression guard: a
// client-supplied X-Tenant-ID must never become the tenant scope of an update.
func TestUpdateClassHandlerIgnoresTenantHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	classID, tenantID := uuid.New(), uuid.New()
	otherTenantID := uuid.New()

	usecase := &updateUsecaseMock{response: &domain.ClassResponse{ID: classID, TenantID: tenantID}}
	router := gin.New()
	handler := NewClassHandler(usecase, updateCreationUsecaseMock{})
	router.PATCH("/classes/:id", func(c *gin.Context) {
		c.Set("tenant_id", tenantID.String())
		handler.Update(c)
	})

	req := httptest.NewRequest(http.MethodPatch, "/classes/"+classID.String(), strings.NewReader(`{"name":"X"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", otherTenantID.String())
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status=%d want=200 body=%s", res.Code, res.Body.String())
	}
	if usecase.tenantID != tenantID {
		t.Fatalf("tenant=%s want=%s", usecase.tenantID, tenantID)
	}
}

type editorPermission struct{ denied string }

func (p editorPermission) CheckPermission(_ context.Context, _, _, _, permission string) (bool, error) {
	return permission != p.denied, nil
}
func (editorPermission) Close() error { return nil }

func TestUpdateClassInlineCategoryPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, denied := range []string{"category:create", "class:update", ""} {
		t.Run(denied, func(t *testing.T) {
			tenant, class := uuid.New(), uuid.New()
			uc := &updateUsecaseMock{response: &domain.ClassResponse{ID: class}}
			permission := editorPermission{denied: denied}
			h := NewClassHandler(uc, updateCreationUsecaseMock{}).WithCreationPermissions(permission)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("tenant_id", tenant.String())
				c.Set("role_id", uuid.NewString())
				c.Set("member_id", uuid.NewString())
			})
			router.PATCH("/classes/:id", middleware.RequirePermission(permission, "class:update"), h.Update)
			req := httptest.NewRequest(http.MethodPatch, "/classes/"+class.String(), strings.NewReader(`{"category":{"name":"New"}}`))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if denied != "" {
				if res.Code != http.StatusForbidden || uc.request != nil {
					t.Fatalf("denied operation reached usecase: %d", res.Code)
				}
			} else if res.Code != http.StatusOK || uc.request == nil || uc.request.Category.Name != "New" {
				t.Fatalf("authorized operation: %d", res.Code)
			}
		})
	}
}
