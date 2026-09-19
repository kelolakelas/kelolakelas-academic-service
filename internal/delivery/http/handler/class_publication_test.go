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

type publicationUsecaseMock struct {
	response *domain.ClassResponse
	err      error
}

func (m *publicationUsecaseMock) CreateClass(context.Context, uuid.UUID, *domain.CreateClassRequest) (*domain.ClassResponse, error) {
	return nil, nil
}
func (m *publicationUsecaseMock) ListClasses(context.Context, uuid.UUID, domain.ListQuery) (*domain.ClassListResponse, error) {
	return nil, nil
}
func (m *publicationUsecaseMock) DeleteClass(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (m *publicationUsecaseMock) UpdateClassPublication(context.Context, uuid.UUID, uuid.UUID, *domain.UpdateClassPublicationRequest) (*domain.ClassResponse, error) {
	return m.response, m.err
}

type publicationCreationUsecaseMock struct{}

func (publicationCreationUsecaseMock) CreateClassWithCategory(context.Context, uuid.UUID, *domain.CreateClassWithCategoryRequest) (*domain.CreateClassWithCategoryResponse, error) {
	return nil, nil
}

func TestUpdatePublicationHandler(t *testing.T) {
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
		{name: "publish", pathID: classID.String(), tenant: tenantID.String(), body: `{"is_published":true}`, wantStatus: http.StatusOK},
		{name: "unpublish", pathID: classID.String(), tenant: tenantID.String(), body: `{"is_published":false}`, wantStatus: http.StatusOK},
		{name: "missing tenant", pathID: classID.String(), body: `{"is_published":true}`, wantStatus: http.StatusForbidden},
		{name: "invalid UUID", pathID: "not-a-uuid", tenant: tenantID.String(), body: `{"is_published":true}`, wantStatus: http.StatusBadRequest},
		{name: "malformed request body", pathID: classID.String(), tenant: tenantID.String(), body: `{"is_published":`, wantStatus: http.StatusBadRequest},
		{name: "class not found", pathID: classID.String(), tenant: tenantID.String(), body: `{"is_published":true}`, usecaseErr: domain.ErrClassNotFound, wantStatus: http.StatusNotFound},
		{name: "repository error", pathID: classID.String(), tenant: tenantID.String(), body: `{"is_published":true}`, usecaseErr: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usecase := &publicationUsecaseMock{
				response: &domain.ClassResponse{ID: classID, IsPublished: strings.Contains(tc.body, "true")},
				err:      tc.usecaseErr,
			}
			router := gin.New()
			handler := NewClassHandler(usecase, publicationCreationUsecaseMock{})
			router.PATCH("/classes/:id/published", func(c *gin.Context) {
				// Mirrors the verified JWT claim set that AuthMiddleware installs.
				// The header is intentionally ignored: tenant context must never
				// come from a client-controlled value.
				if tc.tenant != "" {
					c.Set("tenant_id", tc.tenant)
				}
				handler.UpdatePublication(c)
			})
			req := httptest.NewRequest(http.MethodPatch, "/classes/"+tc.pathID+"/published", strings.NewReader(tc.body))
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
