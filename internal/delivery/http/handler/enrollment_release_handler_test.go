package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

// releaseEnrollmentUsecaseStub only needs to answer ReleaseEnrollment for these
// tests; every other method is unreachable from the internal release endpoint.
type releaseEnrollmentUsecaseStub struct {
	err    error
	calls  int
	status string
}

func (s *releaseEnrollmentUsecaseStub) EnrollStudent(context.Context, uuid.UUID, *domain.EnrollStudentRequest) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *releaseEnrollmentUsecaseStub) EnrollPublic(context.Context, uuid.UUID, uuid.UUID, *domain.PublicEnrollmentRequest, string) (*domain.PublicEnrollmentResponse, error) {
	return nil, nil
}
func (s *releaseEnrollmentUsecaseStub) ActivateEnrollment(context.Context, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *releaseEnrollmentUsecaseStub) ReleaseEnrollment(_ context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	status := s.status
	if status == "" {
		status = "dropped"
	}
	return &domain.EnrollmentResponse{ID: enrollmentID, Status: status}, nil
}
func (s *releaseEnrollmentUsecaseStub) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) (*domain.EnrollmentListResponse, error) {
	return nil, nil
}
func (s *releaseEnrollmentUsecaseStub) GetByID(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *releaseEnrollmentUsecaseStub) AssignSchedule(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func callReleaseInternal(t *testing.T, stub *releaseEnrollmentUsecaseStub, enrollmentID string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: enrollmentID}}
	c.Request = httptest.NewRequest(http.MethodPut, "/internal/enrollments/"+enrollmentID+"/release", nil)

	NewEnrollmentHandler(stub).ReleaseInternal(c)
	return recorder
}

func TestReleaseInternalReturnsConflictForNonPendingEnrollment(t *testing.T) {
	// A paid enrollment that is already active cannot be released, and the payment
	// provider must learn that its release request was rejected rather than assume
	// the seat is free.
	stub := &releaseEnrollmentUsecaseStub{err: domain.ErrInvalidEnrollmentTransition}

	recorder := callReleaseInternal(t, stub, uuid.New().String())

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusConflict)
	}
	if stub.calls != 1 {
		t.Fatalf("release calls=%d, want 1", stub.calls)
	}
}

func TestReleaseInternalIsIdempotentForRepeatedNotifications(t *testing.T) {
	// The durable retry loop replays the notification; a second call must succeed
	// with the enrollment still dropped instead of failing.
	stub := &releaseEnrollmentUsecaseStub{status: "dropped"}

	recorder := callReleaseInternal(t, stub, uuid.New().String())

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusOK)
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
	if body.Status != "success" || body.Data.Status != "dropped" {
		t.Fatalf("envelope=%+v, want a successful dropped enrollment", body)
	}
}

func TestReleaseInternalReturnsNotFoundForUnknownEnrollment(t *testing.T) {
	stub := &releaseEnrollmentUsecaseStub{err: usecase.ErrEnrollmentNotFound}

	recorder := callReleaseInternal(t, stub, uuid.New().String())

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestReleaseInternalRejectsMalformedID(t *testing.T) {
	stub := &releaseEnrollmentUsecaseStub{}

	recorder := callReleaseInternal(t, stub, "not-a-uuid")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if stub.calls != 0 {
		t.Fatalf("release calls=%d, want 0 for a malformed id", stub.calls)
	}
}

func TestReleaseInternalMapsUnexpectedFailureToServerError(t *testing.T) {
	// An outage must be retryable, so it is reported as a server error rather than
	// as a permanent client-side rejection.
	stub := &releaseEnrollmentUsecaseStub{err: errors.New("database unavailable")}

	recorder := callReleaseInternal(t, stub, uuid.New().String())

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}
