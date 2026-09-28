package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type chatContextReaderStub struct {
	calls    int
	err      error
	schedule *domain.ScheduleRequestChatContext
	report   *domain.ReportChatContext
}

func (s *chatContextReaderStub) ScheduleRequest(_ context.Context, _ uuid.UUID) (*domain.ScheduleRequestChatContext, error) {
	s.calls++
	return s.schedule, s.err
}
func (s *chatContextReaderStub) Report(_ context.Context, _ uuid.UUID) (*domain.ReportChatContext, error) {
	s.calls++
	return s.report, s.err
}

func TestChatContextRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id, tenant, parent, class, student, enrollment, reporter := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tests := []struct {
		name, path string
		data       any
		fields     []string
	}{
		{"schedule", "/internal/chat-context/schedule-requests/", &domain.ScheduleRequestChatContext{ID: id, TenantID: tenant, ParentID: parent, ClassID: class, ClassName: "unpublished", StudentID: student, StudentFirstName: "Ana", Status: "rejected"}, []string{"id", "tenant_id", "parent_id", "class_id", "class_name", "student_id", "student_first_name", "status"}},
		{"report", "/internal/chat-context/reports/", &domain.ReportChatContext{ID: id, TenantID: tenant, EnrollmentID: enrollment, StudentID: student, StudentFirstName: "Ana", ParentID: parent, ClassName: "Math", Title: "Progress", ReporterID: reporter}, []string{"id", "tenant_id", "enrollment_id", "student_id", "student_first_name", "parent_id", "class_name", "title", "reporter_id"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &chatContextReaderStub{}
			switch value := tc.data.(type) {
			case *domain.ScheduleRequestChatContext:
				stub.schedule = value
			case *domain.ReportChatContext:
				stub.report = value
			}
			r := gin.New()
			registerRoutes(r, routeHandlers{chatContext: handler.NewChatContextHandler(stub)}, "jwt", "secret", nil)
			registered := 0
			for _, route := range r.Routes() {
				if route.Path == tc.path+":id" {
					registered++
				}
				if route.Path == "/api/v1/chat-context/"+tc.name+"/:id" {
					t.Fatalf("public registration: %s", route.Path)
				}
			}
			if registered != 1 {
				t.Fatalf("registered routes=%d", registered)
			}
			request := func(path, credential string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, path, nil)
				if credential != "" {
					req.Header.Set("X-Internal-Service-Credential", credential)
				}
				r.ServeHTTP(w, req)
				return w
			}
			for _, credential := range []string{"", "wrong"} {
				w := request(tc.path+id.String(), credential)
				if w.Code != 401 || stub.calls != 0 {
					t.Fatalf("credential %q: code=%d calls=%d", credential, w.Code, stub.calls)
				}
			}
			for _, invalidID := range []string{"invalid", uuid.Nil.String()} {
				w := request(tc.path+invalidID, "secret")
				if w.Code != 400 || stub.calls != 0 {
					t.Fatalf("invalid %q: code=%d calls=%d", invalidID, w.Code, stub.calls)
				}
			}
			stub.err = domain.ErrChatContextNotFound
			w := request(tc.path+uuid.NewString(), "secret")
			if w.Code != 404 {
				t.Fatalf("missing: code=%d", w.Code)
			}
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || string(envelope["data"]) != "null" || string(envelope["status"]) != "\"error\"" {
				t.Fatalf("404 envelope: %s %v", w.Body.String(), err)
			}
			stub.err = errors.New("db failure")
			w = request(tc.path+id.String(), "secret")
			if w.Code != 500 {
				t.Fatalf("db failure: %d", w.Code)
			}
			stub.err = nil
			w = request(tc.path+id.String(), "secret")
			if w.Code != 200 {
				t.Fatalf("success: %d %s", w.Code, w.Body.String())
			}
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(envelope["data"], &payload); err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(payload))
			for key := range payload {
				got = append(got, key)
			}
			sort.Strings(got)
			sort.Strings(tc.fields)
			if len(got) != len(tc.fields) {
				t.Fatalf("fields=%v want=%v", got, tc.fields)
			}
			for i := range got {
				if got[i] != tc.fields[i] {
					t.Fatalf("fields=%v want=%v", got, tc.fields)
				}
			}
			if payload["id"] != id.String() || payload["student_first_name"] != "Ana" {
				t.Fatalf("wrong data: %v", payload)
			}
			for _, public := range []string{"/api/v1/chat-context/", "/chat-context/"} {
				w = request(public+tc.name+"/"+id.String(), "secret")
				if w.Code != 404 {
					t.Fatalf("unexpected public route: %s %d", public, w.Code)
				}
			}
		})
	}
}
