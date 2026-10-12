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

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// KEL-188: the category PATCH handler binds optional fields, rejects malformed
// ids/tenants, and maps usecase errors to their status codes.

type updateCategoryUsecaseMock struct {
	response *domain.CategoryResponse
	err      error
	tenantID uuid.UUID
	catID    uuid.UUID
	request  *domain.UpdateCategoryRequest
}

func (m *updateCategoryUsecaseMock) CreateCategory(context.Context, uuid.UUID, *domain.CreateCategoryRequest) (*domain.CategoryResponse, error) {
	return nil, nil
}
func (m *updateCategoryUsecaseMock) ListCategories(context.Context, uuid.UUID, domain.ListQuery) (*domain.CategoryListResponse, error) {
	return nil, nil
}
func (m *updateCategoryUsecaseMock) DeleteCategory(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (m *updateCategoryUsecaseMock) UpdateCategory(_ context.Context, tenantID, id uuid.UUID, req *domain.UpdateCategoryRequest) (*domain.CategoryResponse, error) {
	m.tenantID = tenantID
	m.catID = id
	m.request = req
	return m.response, m.err
}

func TestUpdateCategoryHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	categoryID, tenantID := uuid.New(), uuid.New()

	cases := []struct {
		name       string
		pathID     string
		tenant     string
		body       string
		usecaseErr error
		wantStatus int
	}{
		{name: "rename", pathID: categoryID.String(), tenant: tenantID.String(), body: `{"name":"Fisika Dasar"}`, wantStatus: http.StatusOK},
		{name: "description only", pathID: categoryID.String(), tenant: tenantID.String(), body: `{"description":{"text":"baru"}}`, wantStatus: http.StatusOK},
		{name: "empty name rejected by binding", pathID: categoryID.String(), tenant: tenantID.String(), body: `{"name":""}`, wantStatus: http.StatusBadRequest},
		{name: "missing tenant", pathID: categoryID.String(), body: `{"name":"X"}`, wantStatus: http.StatusForbidden},
		{name: "invalid UUID", pathID: "not-a-uuid", tenant: tenantID.String(), body: `{"name":"X"}`, wantStatus: http.StatusBadRequest},
		{name: "malformed request body", pathID: categoryID.String(), tenant: tenantID.String(), body: `{"name":`, wantStatus: http.StatusBadRequest},
		{name: "category not found", pathID: categoryID.String(), tenant: tenantID.String(), body: `{"name":"X"}`, usecaseErr: domain.ErrCategoryNotFound, wantStatus: http.StatusNotFound},
		{name: "foreign tenant category is forbidden", pathID: categoryID.String(), tenant: tenantID.String(), body: `{"name":"X"}`, usecaseErr: domain.ErrCategoryForbidden, wantStatus: http.StatusForbidden},
		{name: "blank name", pathID: categoryID.String(), tenant: tenantID.String(), body: `{"name":"   "}`, usecaseErr: domain.ErrCategoryNameRequired, wantStatus: http.StatusUnprocessableEntity},
		{name: "repository error", pathID: categoryID.String(), tenant: tenantID.String(), body: `{"name":"X"}`, usecaseErr: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usecase := &updateCategoryUsecaseMock{response: &domain.CategoryResponse{ID: categoryID, TenantID: tenantID}}
			usecase.err = tc.usecaseErr
			router := gin.New()
			handler := NewCategoryHandler(usecase)
			router.PATCH("/categories/:id", func(c *gin.Context) {
				// Mirrors the verified JWT claim that AuthMiddleware installs.
				// X-Tenant-ID is deliberately present but must be ignored.
				if tc.tenant != "" {
					c.Set("tenant_id", tc.tenant)
				}
				handler.Update(c)
			})
			req := httptest.NewRequest(http.MethodPatch, "/categories/"+tc.pathID, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Tenant-ID", "11111111-1111-1111-1111-111111111111")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			if res.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", res.Code, tc.wantStatus, res.Body.String())
			}
		})
	}
}
