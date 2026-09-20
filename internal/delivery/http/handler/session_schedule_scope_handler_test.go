package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

// scopePermissionClientStub is a minimal grpcclient.PermissionClient that grants
// access, so the route mirror below can attach the same RequirePermission
// middleware the production route table uses.
type scopePermissionClientStub struct {
	permissions []string
	allowed     bool
	denyAll     bool
}

func (s *scopePermissionClientStub) CheckPermission(_ context.Context, _ string, permission string) (bool, error) {
	s.permissions = append(s.permissions, permission)
	return !s.denyAll, nil
}

func (*scopePermissionClientStub) Close() error { return nil }

var _ grpcclient.PermissionClient = (*scopePermissionClientStub)(nil)

// scopeScheduleUsecase records the tenant and the resolved request every
// operation receives. It is the boundary that proves the handler forwards the
// signed JWT tenant claim unchanged instead of the client-controlled
// X-Tenant-ID header.
type scopeScheduleUsecase struct {
	recordingScheduleUsecase

	mutations   []string
	tenants     []uuid.UUID
	sessionIDs  []uuid.UUID
	scheduleIDs []uuid.UUID
	attendeesT  uuid.UUID
	attendeesID uuid.UUID
	err         error
}

func (m *scopeScheduleUsecase) note(name string, tenantID uuid.UUID) {
	m.mutations = append(m.mutations, name)
	m.tenants = append(m.tenants, tenantID)
}

func (m *scopeScheduleUsecase) RescheduleSession(_ context.Context, tenantID uuid.UUID, req *domain.RescheduleSessionRequest) (*domain.RescheduleSessionResponse, error) {
	m.note("reschedule", tenantID)
	m.sessionIDs = append(m.sessionIDs, req.SessionID)
	return &domain.RescheduleSessionResponse{}, m.err
}

func (m *scopeScheduleUsecase) ChangeSchedulePermanent(_ context.Context, tenantID uuid.UUID, req *domain.PermanentScheduleChangeRequest) (*domain.PermanentScheduleChangeResponse, error) {
	m.note("schedule-permanent", tenantID)
	m.scheduleIDs = append(m.scheduleIDs, req.OldScheduleID)
	return &domain.PermanentScheduleChangeResponse{}, m.err
}

func (m *scopeScheduleUsecase) ChangeTutorTemporary(_ context.Context, tenantID uuid.UUID, req *domain.SubstituteTutorRequest) (*domain.SubstituteTutorResponse, error) {
	m.note("substitute-tutor", tenantID)
	m.sessionIDs = append(m.sessionIDs, req.SessionID)
	return &domain.SubstituteTutorResponse{}, m.err
}

func (m *scopeScheduleUsecase) ChangeTutorPermanent(_ context.Context, tenantID uuid.UUID, req *domain.PermanentTutorChangeRequest) (*domain.PermanentTutorChangeResponse, error) {
	m.note("tutor-permanent", tenantID)
	m.scheduleIDs = append(m.scheduleIDs, req.ScheduleID)
	return &domain.PermanentTutorChangeResponse{}, m.err
}

func (m *scopeScheduleUsecase) GetSessionAttendees(_ context.Context, tenantID, sessionID uuid.UUID) ([]*domain.Enrollment, error) {
	m.note("attendees", tenantID)
	m.attendeesT, m.attendeesID = tenantID, sessionID
	if m.err != nil {
		return nil, m.err
	}
	return []*domain.Enrollment{}, nil
}

var _ usecase.ScheduleUsecase = (*scopeScheduleUsecase)(nil)

// sessionScheduleScopeRouter mirrors the real route table in cmd/server/main.go
// for the five tenant-scoped session/schedule operations: every alias/path
// variant, the schedule:update permission middleware on the four mutations, and
// GET /sessions/:id/attendees registered *without* RequirePermission exactly as
// in production.
func sessionScheduleScopeRouter(schedule *scopeScheduleUsecase, permissions *scopePermissionClientStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	scheduleHandler := NewScheduleHandler(schedule)
	requireUpdate := middleware.RequirePermission(permissions, "schedule:update")

	apiV1 := router.Group("/api/v1")
	apiV1.Use(middleware.AuthMiddleware(testJWTSecret))
	apiV1.PUT("/schedules/permanent", requireUpdate, scheduleHandler.ChangeSchedulePermanent)
	apiV1.PUT("/schedules/:id/permanent", requireUpdate, scheduleHandler.ChangeSchedulePermanent)
	apiV1.PATCH("/schedules/tutor-permanent", requireUpdate, scheduleHandler.ChangeTutorPermanent)
	apiV1.PATCH("/schedules/:id/tutor-permanent", requireUpdate, scheduleHandler.ChangeTutorPermanent)
	apiV1.PUT("/schedules/tutor-permanent", requireUpdate, scheduleHandler.ChangeTutorPermanent)
	apiV1.PUT("/schedules/:id/tutor-permanent", requireUpdate, scheduleHandler.ChangeTutorPermanent)
	apiV1.POST("/sessions/reschedule", requireUpdate, scheduleHandler.RescheduleSession)
	apiV1.POST("/sessions/:id/reschedule", requireUpdate, scheduleHandler.RescheduleSession)
	apiV1.PATCH("/sessions/substitute-tutor", requireUpdate, scheduleHandler.ChangeTutorTemporary)
	apiV1.PATCH("/sessions/:id/substitute-tutor", requireUpdate, scheduleHandler.ChangeTutorTemporary)
	apiV1.GET("/sessions/:id/attendees", scheduleHandler.GetSessionAttendees)

	return router
}

func rescheduleBody(sessionID uuid.UUID) string {
	return `{"session_id":"` + sessionID.String() + `","new_session_date":"2026-09-21T16:00:00Z",` +
		`"new_start_time":"16:00:00","new_end_time":"17:00:00"}`
}

func permanentScheduleBody(scheduleID uuid.UUID) string {
	return `{"old_schedule_id":"` + scheduleID.String() + `","new_day_of_week":3,` +
		`"new_start_time":"16:00:00","new_end_time":"17:00:00","effective_date":"2026-09-14T00:00:00Z"}`
}

func substituteTutorBody(sessionID uuid.UUID) string {
	return `{"session_id":"` + sessionID.String() + `","substitute_tutor_id":"` + uuid.New().String() + `"}`
}

func permanentTutorBody(scheduleID uuid.UUID) string {
	return `{"schedule_id":"` + scheduleID.String() + `","new_tutor_id":"` + uuid.New().String() + `",` +
		`"effective_date":"2026-09-14T00:00:00Z"}`
}

// AC (b): the owning tenant's member keeps using every alias route and every path
// route, and the tenant handed to the use case is always the signed token's
// claim -- never the spoofed X-Tenant-ID header that every request carries.
func TestSessionScheduleMutationsResolveTenantFromToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenantID := uuid.New()
	token := signToken(t, middleware.Claims{
		UserID:   uuid.New().String(),
		TenantID: tenantID.String(),
		RoleID:   uuid.New().String(),
	})
	sessionID, scheduleID := uuid.New(), uuid.New()

	cases := []struct {
		name        string
		method      string
		path        string
		body        string
		wantName    string
		wantSession uuid.UUID
		wantSched   uuid.UUID
	}{
		{
			name: "reschedule alias route", method: http.MethodPost, path: "/api/v1/sessions/reschedule",
			body:     rescheduleBody(sessionID),
			wantName: "reschedule", wantSession: sessionID,
		},
		{
			name: "reschedule path route", method: http.MethodPost, path: "/api/v1/sessions/" + sessionID.String() + "/reschedule",
			body:     rescheduleBody(sessionID),
			wantName: "reschedule", wantSession: sessionID,
		},
		{
			// Edge case from the issue: a session_id in the body plus a different
			// id in the path. The body value is the authoritative one and must be
			// the id the use case is asked to mutate.
			name: "reschedule body id wins over a conflicting path id", method: http.MethodPost, path: "/api/v1/sessions/" + uuid.New().String() + "/reschedule",
			body:     rescheduleBody(sessionID),
			wantName: "reschedule", wantSession: sessionID,
		},
		{
			name: "permanent schedule alias route", method: http.MethodPut, path: "/api/v1/schedules/permanent",
			body:     permanentScheduleBody(scheduleID),
			wantName: "schedule-permanent", wantSched: scheduleID,
		},
		{
			name: "permanent schedule path route", method: http.MethodPut, path: "/api/v1/schedules/" + scheduleID.String() + "/permanent",
			body:     permanentScheduleBody(scheduleID),
			wantName: "schedule-permanent", wantSched: scheduleID,
		},
		{
			name: "substitute tutor alias route", method: http.MethodPatch, path: "/api/v1/sessions/substitute-tutor",
			body:     substituteTutorBody(sessionID),
			wantName: "substitute-tutor", wantSession: sessionID,
		},
		{
			name: "substitute tutor path route", method: http.MethodPatch, path: "/api/v1/sessions/" + sessionID.String() + "/substitute-tutor",
			body:     substituteTutorBody(sessionID),
			wantName: "substitute-tutor", wantSession: sessionID,
		},
		{
			name: "permanent tutor patch alias route", method: http.MethodPatch, path: "/api/v1/schedules/tutor-permanent",
			body:     permanentTutorBody(scheduleID),
			wantName: "tutor-permanent", wantSched: scheduleID,
		},
		{
			name: "permanent tutor put alias route", method: http.MethodPut, path: "/api/v1/schedules/tutor-permanent",
			body:     permanentTutorBody(scheduleID),
			wantName: "tutor-permanent", wantSched: scheduleID,
		},
		{
			name: "permanent tutor patch path route", method: http.MethodPatch, path: "/api/v1/schedules/" + scheduleID.String() + "/tutor-permanent",
			body:     permanentTutorBody(scheduleID),
			wantName: "tutor-permanent", wantSched: scheduleID,
		},
		{
			name: "permanent tutor put path route", method: http.MethodPut, path: "/api/v1/schedules/" + scheduleID.String() + "/tutor-permanent",
			body:     permanentTutorBody(scheduleID),
			wantName: "tutor-permanent", wantSched: scheduleID,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schedule := &scopeScheduleUsecase{}
			permissions := &scopePermissionClientStub{}
			res := doJSONRequest(sessionScheduleScopeRouter(schedule, permissions), tc.method, tc.path, token, tc.body)

			if res.Code != http.StatusOK {
				t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusOK, res.Body.String())
			}
			if len(schedule.mutations) != 1 || schedule.mutations[0] != tc.wantName {
				t.Fatalf("usecase calls=%v want exactly [%s]", schedule.mutations, tc.wantName)
			}
			// The token's tenant must arrive; the spoofed header must not.
			if schedule.tenants[0] != tenantID {
				t.Fatalf("tenant=%s want token claim %s", schedule.tenants[0], tenantID)
			}
			if schedule.tenants[0] == spoofedTenantHeader {
				t.Fatalf("the X-Tenant-ID header reached the use case: %s", schedule.tenants[0])
			}
			if tc.wantSession != uuid.Nil && (len(schedule.sessionIDs) != 1 || schedule.sessionIDs[0] != tc.wantSession) {
				t.Fatalf("session ids=%v want [%s]", schedule.sessionIDs, tc.wantSession)
			}
			if tc.wantSched != uuid.Nil && (len(schedule.scheduleIDs) != 1 || schedule.scheduleIDs[0] != tc.wantSched) {
				t.Fatalf("schedule ids=%v want [%s]", schedule.scheduleIDs, tc.wantSched)
			}
			// Requirement 4: schedule:update still runs for every mutation route.
			if len(permissions.permissions) != 1 || permissions.permissions[0] != "schedule:update" {
				t.Fatalf("permission checks=%v want exactly [schedule:update]", permissions.permissions)
			}
		})
	}
}

// Documents a pre-existing limitation that tenant scoping did not introduce and
// must not be assumed away: every mutation DTO marks the resource id
// binding:"required", and ShouldBindJSON validates *before* the ":id" path
// fallback runs. A path route whose body omits the id is therefore answered with
// 400 and never reaches the alias. This test pins the current behaviour so that
// changing it stays a deliberate, separately reviewed decision.
func TestPathAliasCannotSupplyAMissingBodyID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenantID := uuid.New()
	token := signToken(t, middleware.Claims{
		UserID:   uuid.New().String(),
		TenantID: tenantID.String(),
		RoleID:   uuid.New().String(),
	})
	sessionID, scheduleID := uuid.New(), uuid.New()

	cases := []struct {
		name string
		verb string
		path string
		body string
	}{
		{
			name: "reschedule", verb: http.MethodPost, path: "/api/v1/sessions/" + sessionID.String() + "/reschedule",
			body: `{"new_session_date":"2026-09-21T16:00:00Z","new_start_time":"16:00:00","new_end_time":"17:00:00"}`,
		},
		{
			name: "permanent schedule", verb: http.MethodPut, path: "/api/v1/schedules/" + scheduleID.String() + "/permanent",
			body: `{"new_day_of_week":3,"new_start_time":"16:00:00","new_end_time":"17:00:00","effective_date":"2026-09-14T00:00:00Z"}`,
		},
		{
			name: "substitute tutor", verb: http.MethodPatch, path: "/api/v1/sessions/" + sessionID.String() + "/substitute-tutor",
			body: `{"substitute_tutor_id":"` + uuid.New().String() + `"}`,
		},
		{
			name: "permanent tutor", verb: http.MethodPatch, path: "/api/v1/schedules/" + scheduleID.String() + "/tutor-permanent",
			body: `{"new_tutor_id":"` + uuid.New().String() + `","effective_date":"2026-09-14T00:00:00Z"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schedule := &scopeScheduleUsecase{}
			res := doJSONRequest(sessionScheduleScopeRouter(schedule, &scopePermissionClientStub{}), tc.verb, tc.path, token, tc.body)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want=%d (pre-existing binding:required behaviour) body=%s",
					res.Code, http.StatusBadRequest, res.Body.String())
			}
			if len(schedule.mutations) != 0 {
				t.Fatalf("use case ran on an invalid request: %v", schedule.mutations)
			}
		})
	}
}

// A token without a tenant claim must be rejected before the use case runs and
// before any client-supplied id is trusted. The permission middleware is
// satisfied here so the rejection is provably writeTenantError's doing.
func TestSessionScheduleMutationsRejectMissingTenantClaim(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// A parent token legitimately carries no tenant claim.
	parentToken := signToken(t, middleware.Claims{
		UserID:   uuid.New().String(),
		RoleID:   uuid.New().String(),
		IsParent: true,
	})
	sessionID, scheduleID := uuid.New(), uuid.New()

	cases := []struct {
		name string
		verb string
		path string
		body string
	}{
		{"reschedule", http.MethodPost, "/api/v1/sessions/" + sessionID.String() + "/reschedule", rescheduleBody(sessionID)},
		{"permanent schedule", http.MethodPut, "/api/v1/schedules/" + scheduleID.String() + "/permanent", permanentScheduleBody(scheduleID)},
		{"substitute tutor", http.MethodPatch, "/api/v1/sessions/" + sessionID.String() + "/substitute-tutor", substituteTutorBody(sessionID)},
		{"permanent tutor", http.MethodPatch, "/api/v1/schedules/" + scheduleID.String() + "/tutor-permanent", permanentTutorBody(scheduleID)},
		{"attendees", http.MethodGet, "/api/v1/sessions/" + sessionID.String() + "/attendees", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schedule := &scopeScheduleUsecase{}
			res := doJSONRequest(sessionScheduleScopeRouter(schedule, &scopePermissionClientStub{}), tc.verb, tc.path, parentToken, tc.body)
			if res.Code != http.StatusForbidden {
				t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusForbidden, res.Body.String())
			}
			if len(schedule.mutations) != 0 {
				t.Fatalf("use case ran without a tenant claim: %v", schedule.mutations)
			}
		})
	}
}

// AC (b): attendees are read for the token's tenant using the path id.
func TestSessionAttendeesUseTheTokenTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenantID, sessionID := uuid.New(), uuid.New()
	token := signToken(t, middleware.Claims{UserID: uuid.New().String(), TenantID: tenantID.String()})
	schedule := &scopeScheduleUsecase{}

	res := doJSONRequest(sessionScheduleScopeRouter(schedule, &scopePermissionClientStub{}),
		http.MethodGet, "/api/v1/sessions/"+sessionID.String()+"/attendees", token, "")

	if res.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if schedule.attendeesT != tenantID || schedule.attendeesID != sessionID {
		t.Fatalf("attendees called with tenant=%s session=%s want tenant=%s session=%s",
			schedule.attendeesT, schedule.attendeesID, tenantID, sessionID)
	}
	if schedule.attendeesT == spoofedTenantHeader {
		t.Fatalf("the X-Tenant-ID header reached the use case: %s", schedule.attendeesT)
	}
}

// AC (c) and (e): another tenant's or an unknown id is a 404 whose message must
// not reveal whether the row exists; a malformed path id is a 400; and an
// unexpected failure stays a 500 instead of being laundered into a 404.
func TestSessionScheduleMutationsMapErrorsToStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenantID := uuid.New()
	token := signToken(t, middleware.Claims{
		UserID:   uuid.New().String(),
		TenantID: tenantID.String(),
		RoleID:   uuid.New().String(),
	})
	id := uuid.New()

	cases := []struct {
		name        string
		verb        string
		path        string
		body        string
		usecaseErr  error
		wantStatus  int
		wantMessage string
	}{
		{
			name: "session not found", verb: http.MethodPost, path: "/api/v1/sessions/" + id.String() + "/reschedule",
			body: rescheduleBody(id), usecaseErr: usecase.ErrSessionNotFound,
			wantStatus: http.StatusNotFound, wantMessage: "class session not found",
		},
		{
			name: "schedule not found", verb: http.MethodPut, path: "/api/v1/schedules/" + id.String() + "/permanent",
			body: permanentScheduleBody(id), usecaseErr: usecase.ErrScheduleNotFound,
			wantStatus: http.StatusNotFound, wantMessage: "class schedule not found",
		},
		{
			name: "session not found on substitute tutor", verb: http.MethodPatch, path: "/api/v1/sessions/" + id.String() + "/substitute-tutor",
			body: substituteTutorBody(id), usecaseErr: usecase.ErrSessionNotFound,
			wantStatus: http.StatusNotFound, wantMessage: "class session not found",
		},
		{
			name: "schedule not found on permanent tutor", verb: http.MethodPatch, path: "/api/v1/schedules/" + id.String() + "/tutor-permanent",
			body: permanentTutorBody(id), usecaseErr: usecase.ErrScheduleNotFound,
			wantStatus: http.StatusNotFound, wantMessage: "class schedule not found",
		},
		{
			name: "attendees session not found", verb: http.MethodGet, path: "/api/v1/sessions/" + id.String() + "/attendees",
			usecaseErr: usecase.ErrSessionNotFound, wantStatus: http.StatusNotFound,
		},
		{
			name: "attendees malformed id", verb: http.MethodGet, path: "/api/v1/sessions/not-a-uuid/attendees",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "repository failure is not a 404", verb: http.MethodPost, path: "/api/v1/sessions/" + id.String() + "/reschedule",
			body: rescheduleBody(id), usecaseErr: errors.New("connection reset"),
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "repository failure on permanent change is not a 404", verb: http.MethodPut, path: "/api/v1/schedules/" + id.String() + "/permanent",
			body: permanentScheduleBody(id), usecaseErr: errors.New("connection reset"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schedule := &scopeScheduleUsecase{err: tc.usecaseErr}
			res := doJSONRequest(sessionScheduleScopeRouter(schedule, &scopePermissionClientStub{}), tc.verb, tc.path, token, tc.body)
			if res.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", res.Code, tc.wantStatus, res.Body.String())
			}
			if tc.wantMessage != "" && !strings.Contains(res.Body.String(), tc.wantMessage) {
				t.Fatalf("body=%s want message %q", res.Body.String(), tc.wantMessage)
			}
			if tc.wantStatus == http.StatusNotFound && strings.Contains(res.Body.String(), "permission") {
				t.Fatalf("not-found response leaked a permission hint: %s", res.Body.String())
			}
		})
	}
}

// Requirement 4: RequirePermission runs before the handler, so a denied check
// can never reach the tenant comparison or the use case.
func TestScheduleUpdatePermissionRunsBeforeTenantCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenantID := uuid.New()
	token := signToken(t, middleware.Claims{
		UserID:   uuid.New().String(),
		TenantID: tenantID.String(),
		RoleID:   uuid.New().String(),
	})

	cases := []struct {
		name string
		verb string
		path string
		body string
	}{
		{"reschedule", http.MethodPost, "/api/v1/sessions/reschedule", rescheduleBody(uuid.New())},
		{"permanent schedule", http.MethodPut, "/api/v1/schedules/permanent", permanentScheduleBody(uuid.New())},
		{"substitute tutor", http.MethodPatch, "/api/v1/sessions/substitute-tutor", substituteTutorBody(uuid.New())},
		{"permanent tutor", http.MethodPatch, "/api/v1/schedules/tutor-permanent", permanentTutorBody(uuid.New())},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schedule := &scopeScheduleUsecase{}
			denied := &scopePermissionClientStub{denyAll: true}
			res := doJSONRequest(sessionScheduleScopeRouter(schedule, denied), tc.verb, tc.path, token, tc.body)

			if res.Code != http.StatusForbidden {
				t.Fatalf("status=%d want=%d body=%s", res.Code, http.StatusForbidden, res.Body.String())
			}
			if len(schedule.mutations) != 0 {
				t.Fatalf("handler ran despite a denied permission check: %v", schedule.mutations)
			}
		})
	}
}
