package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

// cancelEnrollmentUsecaseStub only needs to answer CancelPendingEnrollment; the
// handler tests assert the status mapping, not the cancellation semantics.
type cancelEnrollmentUsecaseStub struct {
	response     *domain.EnrollmentResponse
	err          error
	parentID     uuid.UUID
	enrollmentID uuid.UUID
	calls        int
}

func (s *cancelEnrollmentUsecaseStub) EnrollStudent(context.Context, uuid.UUID, *domain.EnrollStudentRequest) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *cancelEnrollmentUsecaseStub) EnrollPublic(context.Context, uuid.UUID, uuid.UUID, *domain.PublicEnrollmentRequest, string) (*domain.PublicEnrollmentResponse, error) {
	return nil, nil
}
func (s *cancelEnrollmentUsecaseStub) ActivateEnrollment(context.Context, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *cancelEnrollmentUsecaseStub) ReleaseEnrollment(context.Context, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *cancelEnrollmentUsecaseStub) CancelPendingEnrollment(_ context.Context, parentID, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error) {
	s.calls++
	s.parentID, s.enrollmentID = parentID, enrollmentID
	if s.err != nil {
		return nil, s.err
	}
	if s.response != nil {
		return s.response, nil
	}
	return &domain.EnrollmentResponse{ID: enrollmentID, Status: "dropped"}, nil
}
func (s *cancelEnrollmentUsecaseStub) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) (*domain.EnrollmentListResponse, error) {
	return nil, nil
}
func (s *cancelEnrollmentUsecaseStub) GetByID(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}
func (s *cancelEnrollmentUsecaseStub) AssignSchedule(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func callCancel(t *testing.T, stub *cancelEnrollmentUsecaseStub, parentUserID string, isParent bool, enrollmentID string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: enrollmentID}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/enrollments/"+enrollmentID+"/cancel", nil)
	if parentUserID != "" {
		c.Set("user_id", parentUserID)
	}
	c.Set("is_parent", isParent)

	NewEnrollmentHandler(stub).Cancel(c)
	return recorder
}

func TestCancelReturnsCancelledEnrollment(t *testing.T) {
	parentID := uuid.New()
	enrollmentID := uuid.New()
	stub := &cancelEnrollmentUsecaseStub{}

	recorder := callCancel(t, stub, parentID.String(), true, enrollmentID.String())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want %d", recorder.Code, recorder.Body.String(), http.StatusOK)
	}
	if stub.calls != 1 || stub.parentID != parentID || stub.enrollmentID != enrollmentID {
		t.Fatalf("usecase called %d times with parent=%s enrollment=%s", stub.calls, stub.parentID, stub.enrollmentID)
	}
}

// The cancellation endpoint is parent-only, so a tenant-scoped caller is refused
// before the usecase runs.
func TestCancelRequiresParentAuthentication(t *testing.T) {
	stub := &cancelEnrollmentUsecaseStub{}

	recorder := callCancel(t, stub, uuid.New().String(), false, uuid.New().String())
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusForbidden)
	}
	if stub.calls != 0 {
		t.Fatalf("usecase called %d times, want 0", stub.calls)
	}
}

func TestCancelRejectsMissingParentIdentity(t *testing.T) {
	stub := &cancelEnrollmentUsecaseStub{}

	recorder := callCancel(t, stub, "", true, uuid.New().String())
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusForbidden)
	}
	if stub.calls != 0 {
		t.Fatalf("usecase called %d times, want 0", stub.calls)
	}
}

func TestCancelRejectsMalformedEnrollmentID(t *testing.T) {
	stub := &cancelEnrollmentUsecaseStub{}

	recorder := callCancel(t, stub, uuid.New().String(), true, "not-a-uuid")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if stub.calls != 0 {
		t.Fatalf("usecase called %d times, want 0", stub.calls)
	}
}

// An unknown enrollment and another parent's enrollment are answered identically, so
// the response cannot be used to probe which enrollments exist.
func TestCancelMapsUnknownEnrollmentToNotFound(t *testing.T) {
	stub := &cancelEnrollmentUsecaseStub{err: usecase.ErrEnrollmentNotFound}

	recorder := callCancel(t, stub, uuid.New().String(), true, uuid.New().String())
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want %d", recorder.Code, recorder.Body.String(), http.StatusNotFound)
	}
}

// An enrollment that already started, or whose invoice settled first, is reported as
// a conflict so the parent learns the cancellation did not happen.
func TestCancelMapsInvalidTransitionToConflict(t *testing.T) {
	for _, err := range []error{
		domain.ErrInvalidEnrollmentTransition,
	} {
		stub := &cancelEnrollmentUsecaseStub{err: err}

		recorder := callCancel(t, stub, uuid.New().String(), true, uuid.New().String())
		if recorder.Code != http.StatusConflict {
			t.Fatalf("error=%v status=%d, want %d", err, recorder.Code, http.StatusConflict)
		}
	}
}

func TestCancelMapsUnexpectedFailureToServerError(t *testing.T) {
	stub := &cancelEnrollmentUsecaseStub{err: errors.New("withdraw enrollment payment: connection refused")}

	recorder := callCancel(t, stub, uuid.New().String(), true, uuid.New().String())
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}
