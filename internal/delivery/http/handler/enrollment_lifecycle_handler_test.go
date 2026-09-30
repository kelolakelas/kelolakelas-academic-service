package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

// lifecycleEnrollmentUsecaseStub answers the one lifecycle method under test and
// records it; every other method is unreachable from these internal endpoints.
type lifecycleEnrollmentUsecaseStub struct {
	method string // "suspend", "resume", or "end"; the method to answer
	err    error
	calls  int
	status string
}

func (s *lifecycleEnrollmentUsecaseStub) EnrollStudent(context.Context, uuid.UUID, *domain.EnrollStudentRequest) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *lifecycleEnrollmentUsecaseStub) EnrollPublic(context.Context, uuid.UUID, uuid.UUID, *domain.PublicEnrollmentRequest, string) (*domain.PublicEnrollmentResponse, error) {
	return nil, nil
}
func (s *lifecycleEnrollmentUsecaseStub) ActivateEnrollment(context.Context, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *lifecycleEnrollmentUsecaseStub) ReleaseEnrollment(context.Context, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *lifecycleEnrollmentUsecaseStub) SuspendEnrollment(_ context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error) {
	if s.method != "suspend" {
		return nil, nil
	}
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &domain.EnrollmentResponse{ID: enrollmentID, Status: s.status}, nil
}
func (s *lifecycleEnrollmentUsecaseStub) ResumeEnrollment(_ context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error) {
	if s.method != "resume" {
		return nil, nil
	}
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &domain.EnrollmentResponse{ID: enrollmentID, Status: s.status}, nil
}
func (s *lifecycleEnrollmentUsecaseStub) EndEnrollment(_ context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error) {
	if s.method != "end" {
		return nil, nil
	}
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &domain.EnrollmentResponse{ID: enrollmentID, Status: s.status}, nil
}
func (s *lifecycleEnrollmentUsecaseStub) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) (*domain.EnrollmentListResponse, error) {
	return nil, nil
}
func (s *lifecycleEnrollmentUsecaseStub) GetByID(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *lifecycleEnrollmentUsecaseStub) AssignSchedule(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *lifecycleEnrollmentUsecaseStub) CancelPendingEnrollment(context.Context, uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func callLifecycleInternal(t *testing.T, stub *lifecycleEnrollmentUsecaseStub, action, enrollmentID string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: enrollmentID}}
	c.Request = httptest.NewRequest(http.MethodPut, "/internal/enrollments/"+enrollmentID+"/"+action, nil)

	handler := NewEnrollmentHandler(stub)
	switch action {
	case "suspend":
		handler.SuspendInternal(c)
	case "resume":
		handler.ResumeInternal(c)
	case "end":
		handler.EndInternal(c)
	default:
		t.Fatalf("unknown action %q", action)
	}
	return recorder
}

func TestSuspendInternalAnswersSuspendedEnrollment(t *testing.T) {
	stub := &lifecycleEnrollmentUsecaseStub{method: "suspend", status: "suspended"}

	recorder := callLifecycleInternal(t, stub, "suspend", uuid.New().String())

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusOK)
	}
	if stub.calls != 1 {
		t.Fatalf("suspend calls=%d, want 1", stub.calls)
	}
	var body struct {
		Status string `json:"status"`
		Data   struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "success" || body.Data.Status != "suspended" {
		t.Fatalf("envelope=%+v, want a successful suspended enrollment", body)
	}
}

func TestSuspendInternalReturnsConflictForNonActiveEnrollment(t *testing.T) {
	stub := &lifecycleEnrollmentUsecaseStub{method: "suspend", err: domain.ErrInvalidEnrollmentTransition}

	recorder := callLifecycleInternal(t, stub, "suspend", uuid.New().String())

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusConflict)
	}
	if stub.calls != 1 {
		t.Fatalf("suspend calls=%d, want 1", stub.calls)
	}
}

func TestResumeInternalAnswersActiveEnrollment(t *testing.T) {
	stub := &lifecycleEnrollmentUsecaseStub{method: "resume", status: "active"}

	recorder := callLifecycleInternal(t, stub, "resume", uuid.New().String())

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusOK)
	}
	var body struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Status != "active" {
		t.Fatalf("enrollment status=%q, want active", body.Data.Status)
	}
}

// A full schedule is a conflict, not an error: billing's retry can succeed once a
// seat frees up, and the enrollment itself stays suspended.
func TestResumeInternalReturnsConflictWhenScheduleFull(t *testing.T) {
	stub := &lifecycleEnrollmentUsecaseStub{method: "resume", err: domain.ErrScheduleFull}

	recorder := callLifecycleInternal(t, stub, "resume", uuid.New().String())

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusConflict)
	}
}

// The student re-enrolled while this enrollment was suspended; reclaiming the old
// seat must also surface as a conflict for the caller.
func TestResumeInternalReturnsConflictForSuspendedDuplicate(t *testing.T) {
	stub := &lifecycleEnrollmentUsecaseStub{method: "resume", err: domain.ErrEnrollmentSuspendedConflict}

	recorder := callLifecycleInternal(t, stub, "resume", uuid.New().String())

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestEndInternalAnswersDroppedEnrollment(t *testing.T) {
	stub := &lifecycleEnrollmentUsecaseStub{method: "end", status: "dropped"}

	recorder := callLifecycleInternal(t, stub, "end", uuid.New().String())

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusOK)
	}
	var body struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Status != "dropped" {
		t.Fatalf("enrollment status=%q, want dropped", body.Data.Status)
	}
}

func TestEndInternalReturnsConflictForPendingEnrollment(t *testing.T) {
	stub := &lifecycleEnrollmentUsecaseStub{method: "end", err: domain.ErrInvalidEnrollmentTransition}

	recorder := callLifecycleInternal(t, stub, "end", uuid.New().String())

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusConflict)
	}
}

// Every lifecycle endpoint answers a malformed id with 400 before touching the
// usecase, so a routing mistake can never mutate a real enrollment.
func TestLifecycleInternalReturnsBadRequestForInvalidID(t *testing.T) {
	for _, action := range []string{"suspend", "resume", "end"} {
		stub := &lifecycleEnrollmentUsecaseStub{method: action}

		recorder := callLifecycleInternal(t, stub, action, "not-a-uuid")

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d, want %d", action, recorder.Code, http.StatusBadRequest)
		}
		if stub.calls != 0 {
			t.Fatalf("%s calls=%d, want 0 for a malformed id", action, stub.calls)
		}
	}
}

// An unknown enrollment is 404 on every lifecycle endpoint.
func TestLifecycleInternalReturnsNotFoundForUnknownEnrollment(t *testing.T) {
	for _, action := range []string{"suspend", "resume", "end"} {
		stub := &lifecycleEnrollmentUsecaseStub{method: action, err: usecase.ErrEnrollmentNotFound}

		recorder := callLifecycleInternal(t, stub, action, uuid.New().String())

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d, want %d", action, recorder.Code, http.StatusNotFound)
		}
	}
}
