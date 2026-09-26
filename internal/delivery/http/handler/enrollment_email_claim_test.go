package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// KEL-75: the parent checkout must forward the verified JWT email claim to the
// billing invoice request. The claim reaches the handler through the auth
// middleware context, never through the request body or headers.
type emailRecordingEnrollmentUsecase struct {
	lastRequest *domain.PublicEnrollmentRequest
	calls       int
}

func (m *emailRecordingEnrollmentUsecase) EnrollStudent(context.Context, uuid.UUID, *domain.EnrollStudentRequest) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func (m *emailRecordingEnrollmentUsecase) EnrollPublic(_ context.Context, _, _ uuid.UUID, req *domain.PublicEnrollmentRequest, _ string) (*domain.PublicEnrollmentResponse, error) {
	m.calls++
	copied := *req
	m.lastRequest = &copied
	return &domain.PublicEnrollmentResponse{}, nil
}

func (m *emailRecordingEnrollmentUsecase) ActivateEnrollment(context.Context, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func (m *emailRecordingEnrollmentUsecase) ReleaseEnrollment(context.Context, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func (m *emailRecordingEnrollmentUsecase) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) (*domain.EnrollmentListResponse, error) {
	return nil, nil
}

func (m *emailRecordingEnrollmentUsecase) GetByID(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func (m *emailRecordingEnrollmentUsecase) AssignSchedule(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func (m *emailRecordingEnrollmentUsecase) CancelPendingEnrollment(context.Context, uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, nil
}

func emailClaimRouter(enrollment *emailRecordingEnrollmentUsecase) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	apiV1 := router.Group("/api/v1")
	apiV1.Use(middleware.AuthMiddleware(testJWTSecret))
	apiV1.POST("/tenants/:tenant_id/enrollments", NewEnrollmentHandler(enrollment).Create)
	apiV1.POST("/catalog/classes/:class_id/enrollments", NewEnrollmentHandler(enrollment).CreateCatalogEnrollment)
	return router
}

func doEmailClaimEnroll(router *gin.Engine, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/catalog/classes/"+uuid.New().String()+"/enrollments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", "kel75-email-claim-key")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

func TestParentCheckoutForwardsEmailClaimFromToken(t *testing.T) {
	parentID := uuid.New()
	token := signToken(t, middleware.Claims{UserID: parentID.String(), Email: "parent@example.com", IsParent: true})
	enrollment := &emailRecordingEnrollmentUsecase{}
	router := emailClaimRouter(enrollment)

	res := doEmailClaimEnroll(router, token, `{"student_id":"`+uuid.New().String()+`","billing_cycle":"monthly"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s, want 201", res.Code, res.Body.String())
	}
	if enrollment.calls != 1 {
		t.Fatalf("EnrollPublic calls=%d, want 1", enrollment.calls)
	}
	if enrollment.lastRequest == nil || enrollment.lastRequest.SenderEmail != "parent@example.com" {
		t.Fatalf("sender_email=%+v, want the token's email claim", enrollment.lastRequest)
	}
}

// The email claim must survive the same idempotent replay path the use case takes
// for a pending enrollment: the handler forwards it on every EnrollPublic call.
func TestParentCheckoutReplayStillForwardsEmailClaim(t *testing.T) {
	parentID := uuid.New()
	token := signToken(t, middleware.Claims{UserID: parentID.String(), Email: " replay@example.com ", IsParent: true})
	enrollment := &emailRecordingEnrollmentUsecase{}
	router := emailClaimRouter(enrollment)

	body := `{"student_id":"` + uuid.New().String() + `","billing_cycle":"monthly"}`
	first := doEmailClaimEnroll(router, token, body)
	second := doEmailClaimEnroll(router, token, body)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("statuses=%d/%d, want 201/201", first.Code, second.Code)
	}
	if enrollment.calls != 2 {
		t.Fatalf("EnrollPublic calls=%d, want 2 (original + replay)", enrollment.calls)
	}
	if enrollment.lastRequest == nil || enrollment.lastRequest.SenderEmail != "replay@example.com" {
		t.Fatalf("sender_email=%+v, want the trimmed email claim on the replay too", enrollment.lastRequest)
	}
}

// Anti-spoofing: a client that stuffs sender_email into the JSON body must not be
// able to choose the billing contact. The handler overwrites it from the token.
func TestParentCheckoutIgnoresSenderEmailFromBody(t *testing.T) {
	parentID := uuid.New()
	token := signToken(t, middleware.Claims{UserID: parentID.String(), Email: "parent@example.com", IsParent: true})
	enrollment := &emailRecordingEnrollmentUsecase{}
	router := emailClaimRouter(enrollment)

	body := `{"student_id":"` + uuid.New().String() + `","billing_cycle":"monthly","sender_email":"attacker@example.com"}`
	res := doEmailClaimEnroll(router, token, body)
	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s, want 201", res.Code, res.Body.String())
	}
	if enrollment.lastRequest == nil || enrollment.lastRequest.SenderEmail == "attacker@example.com" {
		t.Fatalf("sender_email=%+v, want the token claim to win over the body", enrollment.lastRequest)
	}
}

// A parent whose token predates the email claim still checks out; the forwarded
// email is empty and billing keeps accepting the request.
func TestParentCheckoutWithoutEmailClaimStaysAccepted(t *testing.T) {
	parentID := uuid.New()
	token := signToken(t, middleware.Claims{UserID: parentID.String(), IsParent: true})
	enrollment := &emailRecordingEnrollmentUsecase{}
	router := emailClaimRouter(enrollment)

	res := doEmailClaimEnroll(router, token, `{"student_id":"`+uuid.New().String()+`","billing_cycle":"monthly"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s, want 201 (rollout compatibility)", res.Code, res.Body.String())
	}
	if enrollment.lastRequest == nil || enrollment.lastRequest.SenderEmail != "" {
		t.Fatalf("sender_email=%+v, want empty", enrollment.lastRequest)
	}
}
