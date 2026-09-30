package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

// mineFilterScheduleStub captures the SessionQuery ListSessions receives.
type mineFilterScheduleStub struct {
	usecase.ScheduleUsecase
	calls     int
	gotTenant uuid.UUID
	gotQuery  domain.SessionQuery
}

func (s *mineFilterScheduleStub) ListSessions(_ context.Context, tenantID uuid.UUID, q domain.SessionQuery) (*domain.SessionListResponse, error) {
	s.calls++
	s.gotTenant = tenantID
	s.gotQuery = q
	return &domain.SessionListResponse{}, nil
}

func mineFilterContext(tenant, member, rawQuery string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/sessions?"+rawQuery, nil)
	c.Set("tenant_id", tenant)
	if member != "" {
		c.Set("member_id", member)
	}
	return c, recorder
}

// TestListSessionsMineDerivesTutorFromJWT is the KEL-135 proof that
// mine=true limits the list to the caller's own sessions: the TutorID filter
// comes from the verified JWT member claim, and any client-supplied tutor_id
// (even a malformed one) is ignored instead of leaking another tutor's
// sessions or faulting the request.
func TestListSessionsMineDerivesTutorFromJWT(t *testing.T) {
	tenant, member, other := uuid.New(), uuid.New(), uuid.New()

	t.Run("mine forces tutor to caller despite client tutor_id", func(t *testing.T) {
		stub := &mineFilterScheduleStub{}
		c, recorder := mineFilterContext(tenant.String(), member.String(), "mine=true&tutor_id="+other.String())
		NewSessionHandler(stub).ListSessions(c)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
		}
		if stub.calls != 1 || stub.gotTenant != tenant {
			t.Fatalf("calls=%d tenant=%v want=(1,%v)", stub.calls, stub.gotTenant, tenant)
		}
		if stub.gotQuery.TutorID == nil || *stub.gotQuery.TutorID != member {
			t.Fatalf("tutor filter=%v want caller %v", stub.gotQuery.TutorID, member)
		}
	})

	t.Run("mine ignores malformed client tutor_id", func(t *testing.T) {
		stub := &mineFilterScheduleStub{}
		c, recorder := mineFilterContext(tenant.String(), member.String(), "mine=true&tutor_id=not-a-uuid")
		NewSessionHandler(stub).ListSessions(c)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
		}
		if stub.calls != 1 || stub.gotQuery.TutorID == nil || *stub.gotQuery.TutorID != member {
			t.Fatalf("calls=%d tutor=%v want caller %v", stub.calls, stub.gotQuery.TutorID, member)
		}
	})

	t.Run("mine without member claim is unauthorized", func(t *testing.T) {
		stub := &mineFilterScheduleStub{}
		c, recorder := mineFilterContext(tenant.String(), "", "mine=true")
		NewSessionHandler(stub).ListSessions(c)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
		}
		if stub.calls != 0 {
			t.Fatalf("usecase calls=%d want=0", stub.calls)
		}
	})

	t.Run("explicit tutor filter still passes through without mine", func(t *testing.T) {
		stub := &mineFilterScheduleStub{}
		c, recorder := mineFilterContext(tenant.String(), member.String(), "tutor_id="+other.String())
		NewSessionHandler(stub).ListSessions(c)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
		}
		if stub.gotQuery.TutorID == nil || *stub.gotQuery.TutorID != other {
			t.Fatalf("tutor filter=%v want %v", stub.gotQuery.TutorID, other)
		}
	})
}
