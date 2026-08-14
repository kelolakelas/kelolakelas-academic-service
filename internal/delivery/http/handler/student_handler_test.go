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

type studentHandlerUsecaseStub struct {
	listErr   error
	tenantID  *uuid.UUID
	parentID  *uuid.UUID
	listQuery domain.StudentQuery
}

func (s *studentHandlerUsecaseStub) List(_ context.Context, tenantID, parentID *uuid.UUID, query domain.StudentQuery) (*domain.StudentListResponse, error) {
	s.tenantID, s.parentID, s.listQuery = tenantID, parentID, query
	if s.listErr != nil {
		return nil, s.listErr
	}
	return &domain.StudentListResponse{Items: []domain.Student{}, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize}}, nil
}
func (*studentHandlerUsecaseStub) Create(context.Context, *uuid.UUID, *uuid.UUID, *domain.CreateStudentRequest) (*domain.Student, error) {
	return nil, nil
}
func (*studentHandlerUsecaseStub) GetByID(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.Student, error) {
	return nil, nil
}
func (*studentHandlerUsecaseStub) Update(context.Context, *uuid.UUID, *uuid.UUID, *uuid.UUID, uuid.UUID, *domain.UpdateStudentRequest) (*domain.Student, error) {
	return nil, nil
}
func (*studentHandlerUsecaseStub) Delete(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) error {
	return nil
}

func newStudentHandlerContext(userID, tenantID string, isParent *bool) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/students", nil)
	c.Set("user_id", userID)
	if tenantID != "" {
		c.Set("tenant_id", tenantID)
	}
	if isParent != nil {
		c.Set("is_parent", *isParent)
	}
	return c, recorder
}

func TestStudentScope(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	parent := true
	admin := false
	tests := []struct {
		name       string
		tenant     string
		isParent   *bool
		user       string
		wantTenant *uuid.UUID
		wantParent *uuid.UUID
		wantErr    bool
	}{
		{name: "parent without tenant claim", isParent: &parent, user: userID.String(), wantParent: &userID},
		{name: "parent with nil tenant claim", tenant: uuid.Nil.String(), isParent: &parent, user: userID.String(), wantParent: &userID},
		{name: "parent with tenant claim", tenant: tenantID.String(), isParent: &parent, user: userID.String(), wantParent: &userID},
		{name: "admin with valid tenant", tenant: tenantID.String(), isParent: &admin, user: userID.String(), wantTenant: &tenantID},
		{name: "invalid user id", tenant: tenantID.String(), isParent: &admin, user: "bad", wantErr: true},
		{name: "invalid tenant id", tenant: "bad", isParent: &admin, user: userID.String(), wantErr: true},
		{name: "admin with nil tenant claim", tenant: uuid.Nil.String(), isParent: &admin, user: userID.String(), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newStudentHandlerContext(test.user, test.tenant, test.isParent)
			gotTenant, gotParent, err := studentScope(c)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, test.wantErr)
			}
			if !sameUUIDPointer(gotTenant, test.wantTenant) || !sameUUIDPointer(gotParent, test.wantParent) {
				t.Fatalf("scope=(tenant %v, parent %v), want=(tenant %v, parent %v)", gotTenant, gotParent, test.wantTenant, test.wantParent)
			}
		})
	}
}

func TestStudentListUsesExpectedScopeAndHidesDatabaseError(t *testing.T) {
	userID, tenantID := uuid.New(), uuid.New()
	parent := true
	admin := false
	tests := []struct {
		name       string
		isParent   *bool
		tenant     string
		wantTenant *uuid.UUID
		wantParent *uuid.UUID
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "parent list", isParent: &parent, wantParent: &userID, wantStatus: http.StatusOK},
		{name: "tenant list", isParent: &admin, tenant: tenantID.String(), wantTenant: &tenantID, wantStatus: http.StatusOK},
		{name: "database error", isParent: &admin, tenant: tenantID.String(), wantTenant: &tenantID, err: errors.New("database connection failed"), wantStatus: http.StatusInternalServerError, wantBody: "Failed to fetch students"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &studentHandlerUsecaseStub{listErr: test.err}
			handler := NewStudentHandler(stub)
			c, recorder := newStudentHandlerContext(userID.String(), test.tenant, test.isParent)
			handler.List(c)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if !sameUUIDPointer(stub.tenantID, test.wantTenant) || !sameUUIDPointer(stub.parentID, test.wantParent) {
				t.Fatalf("scope=(tenant %v, parent %v)", stub.tenantID, stub.parentID)
			}
			if test.wantBody != "" && !strings.Contains(recorder.Body.String(), test.wantBody) {
				t.Fatalf("body=%s want message %q", recorder.Body.String(), test.wantBody)
			}
			if strings.Contains(recorder.Body.String(), "database connection failed") {
				t.Fatal("database details must not be returned to the client")
			}
		})
	}
}

func sameUUIDPointer(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
