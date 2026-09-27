package handler

import (
	"context"
	"encoding/json"
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
	createErr error
	tenantID  *uuid.UUID
	parentID  *uuid.UUID
	listQuery domain.StudentQuery
	items     []domain.Student
	student   *domain.Student
}

func (s *studentHandlerUsecaseStub) List(_ context.Context, tenantID, parentID *uuid.UUID, query domain.StudentQuery) (*domain.StudentListResponse, error) {
	s.tenantID, s.parentID, s.listQuery = tenantID, parentID, query
	if s.listErr != nil {
		return nil, s.listErr
	}
	items := s.items
	if items == nil {
		items = []domain.Student{}
	}
	return &domain.StudentListResponse{Items: items, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize}}, nil
}
func (s *studentHandlerUsecaseStub) Create(context.Context, *uuid.UUID, *uuid.UUID, *domain.CreateStudentRequest) (*domain.Student, error) {
	return s.student, s.createErr
}
func (s *studentHandlerUsecaseStub) GetByID(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.Student, error) {
	return s.student, nil
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
		{name: "tenant user with valid tenant", tenant: tenantID.String(), isParent: &admin, user: userID.String(), wantTenant: &tenantID},
		{name: "invalid user id", tenant: tenantID.String(), isParent: &admin, user: "bad", wantErr: true},
		{name: "tenant user without tenant claim", isParent: &admin, user: userID.String(), wantErr: true},
		{name: "tenant user with invalid tenant claim", tenant: "bad", isParent: &admin, user: userID.String(), wantErr: true},
		{name: "tenant user with nil tenant claim", tenant: uuid.Nil.String(), isParent: &admin, user: userID.String(), wantErr: true},
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
		{name: "parent list with zero tenant", isParent: &parent, tenant: uuid.Nil.String(), wantParent: &userID, wantStatus: http.StatusOK},
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

func TestStudentCreateParentScopeAndOwnership(t *testing.T) {
	userID := uuid.New()
	parent := true
	tests := []struct {
		name       string
		createErr  error
		wantStatus int
	}{
		{name: "parent with zero tenant", wantStatus: http.StatusCreated},
		{name: "parent does not own student", createErr: domain.ErrStudentForbidden, wantStatus: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &studentHandlerUsecaseStub{createErr: test.createErr}
			handler := NewStudentHandler(stub)
			c, recorder := newStudentHandlerContext(userID.String(), uuid.Nil.String(), &parent)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/students", strings.NewReader(`{"parent_id":"`+userID.String()+`","first_name":"A","date_of_birth":"2015-01-01"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			handler.Create(c)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
		})
	}
}

// KEL-43: every student response spells the surname key `last_name`; the legacy
// `lastå_name` key must never come back, and a missing surname stays an explicit null.
func TestStudentResponsesUseLastNameKey(t *testing.T) {
	userID := uuid.New()
	parent := true
	surname := "Putri"
	named := domain.Student{ID: uuid.New(), ParentID: userID, FirstName: "Ayu", LastName: &surname}
	unnamed := domain.Student{ID: uuid.New(), ParentID: userID, FirstName: "Budi"}
	stub := &studentHandlerUsecaseStub{items: []domain.Student{named, unnamed}, student: &named}
	handler := NewStudentHandler(stub)

	decode := func(t *testing.T, recorder *httptest.ResponseRecorder) json.RawMessage {
		t.Helper()
		if recorder.Code != http.StatusOK && recorder.Code != http.StatusCreated {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "last\u00e5_name") {
			t.Fatalf("response still carries the legacy key: %s", recorder.Body.String())
		}
		var body struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Data
	}
	lastName := func(t *testing.T, raw json.RawMessage) any {
		t.Helper()
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		value, ok := fields["last_name"]
		if !ok {
			t.Fatalf("student has no last_name key: %s", raw)
		}
		return value
	}

	t.Run("list", func(t *testing.T) {
		c, recorder := newStudentHandlerContext(userID.String(), "", &parent)
		handler.List(c)
		var list struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(decode(t, recorder), &list); err != nil || len(list.Items) != 2 {
			t.Fatalf("items=%d err=%v", len(list.Items), err)
		}
		if got := lastName(t, list.Items[0]); got != surname {
			t.Fatalf("last_name=%v want %q", got, surname)
		}
		if got := lastName(t, list.Items[1]); got != nil {
			t.Fatalf("last_name=%v want null", got)
		}
	})
	t.Run("get", func(t *testing.T) {
		c, recorder := newStudentHandlerContext(userID.String(), "", &parent)
		c.Params = gin.Params{{Key: "id", Value: named.ID.String()}}
		handler.Get(c)
		if got := lastName(t, decode(t, recorder)); got != surname {
			t.Fatalf("last_name=%v want %q", got, surname)
		}
	})
	t.Run("create", func(t *testing.T) {
		c, recorder := newStudentHandlerContext(userID.String(), uuid.Nil.String(), &parent)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/students", strings.NewReader(`{"parent_id":"`+userID.String()+`","first_name":"Ayu","last_name":"Putri","date_of_birth":"2015-01-01"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		handler.Create(c)
		if got := lastName(t, decode(t, recorder)); got != surname {
			t.Fatalf("last_name=%v want %q", got, surname)
		}
	})
}

func sameUUIDPointer(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
