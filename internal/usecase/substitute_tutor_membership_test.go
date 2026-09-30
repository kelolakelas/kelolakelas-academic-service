package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// TestChangeTutorTemporaryValidatesActiveMembership is the KEL-135 proof that a
// substitute must be an active member of the calling tenant: cross-tenant,
// inactive, and unknown ids are validation errors (400-style), while an
// unreachable identity or a missing client fails closed (503-style) and an
// unknown session keeps answering ErrSessionNotFound.
func TestChangeTutorTemporaryValidatesActiveMembership(t *testing.T) {
	newReq := func(sessionID, tutorID uuid.UUID) *domain.SubstituteTutorRequest {
		return &domain.SubstituteTutorRequest{SessionID: sessionID, SubstituteTutorID: tutorID}
	}

	t.Run("inactive member is rejected with zero writes", func(t *testing.T) {
		f := newScopeFixture()
		f.membership.active = false
		substitute := uuid.New()
		res, err := f.usecase.ChangeTutorTemporary(context.Background(), f.ownTenant, newReq(f.session.ID, substitute))
		if !errors.Is(err, ErrSubstituteTutorNotEligible) || res != nil {
			t.Fatalf("res=%+v err=%v want ErrSubstituteTutorNotEligible", res, err)
		}
		if f.writes() != 0 {
			t.Fatalf("writes=%d want=0 for an ineligible tutor", f.writes())
		}
		if len(f.membership.members) != 1 || f.membership.members[0] != substitute.String() {
			t.Fatalf("membership probe members=%v want [%s]", f.membership.members, substitute)
		}
		if len(f.membership.tenants) != 1 || f.membership.tenants[0] != f.ownTenant.String() {
			t.Fatalf("membership probe tenants=%v want [%s]", f.membership.tenants, f.ownTenant)
		}
	})

	t.Run("identity error fails closed", func(t *testing.T) {
		f := newScopeFixture()
		f.membership.err = errors.New("identity unavailable")
		res, err := f.usecase.ChangeTutorTemporary(context.Background(), f.ownTenant, newReq(f.session.ID, uuid.New()))
		if !errors.Is(err, ErrSubstituteTutorUnavailable) || res != nil {
			t.Fatalf("res=%+v err=%v want ErrSubstituteTutorUnavailable", res, err)
		}
		if f.writes() != 0 {
			t.Fatalf("writes=%d want=0 when identity cannot be trusted", f.writes())
		}
	})

	t.Run("missing client fails closed", func(t *testing.T) {
		f := newScopeFixture()
		f.usecase = NewScheduleUsecase(f.tx, nil, f.schedules, f.sessions, f.enrollments)
		res, err := f.usecase.ChangeTutorTemporary(context.Background(), f.ownTenant, newReq(f.session.ID, uuid.New()))
		if !errors.Is(err, ErrSubstituteTutorUnavailable) || res != nil {
			t.Fatalf("res=%+v err=%v want ErrSubstituteTutorUnavailable", res, err)
		}
		if f.writes() != 0 {
			t.Fatalf("writes=%d want=0 without a membership client", f.writes())
		}
	})

	t.Run("unknown session stays not found without a membership verdict", func(t *testing.T) {
		f := newScopeFixture()
		res, err := f.usecase.ChangeTutorTemporary(context.Background(), f.ownTenant, newReq(uuid.New(), uuid.New()))
		if !errors.Is(err, ErrSessionNotFound) || res != nil {
			t.Fatalf("res=%+v err=%v want ErrSessionNotFound", res, err)
		}
		if f.membership.calls != 0 {
			t.Fatalf("membership calls=%d want=0 for an unknown session", f.membership.calls)
		}
		if f.writes() != 0 {
			t.Fatalf("writes=%d want=0", f.writes())
		}
	})

	t.Run("active member assigns with only the session row touched", func(t *testing.T) {
		f := newScopeFixture()
		substitute := uuid.New()
		res, err := f.usecase.ChangeTutorTemporary(context.Background(), f.ownTenant, newReq(f.session.ID, substitute))
		if err != nil {
			t.Fatalf("ChangeTutorTemporary error: %v", err)
		}
		if res.Session == nil || res.Session.TutorID != substitute {
			t.Fatalf("session=%+v want tutor_id=%s", res.Session, substitute)
		}
		if f.sessions.updates != 1 || f.schedules.updates != 0 {
			t.Fatalf("session updates=%d schedule updates=%d want=(1,0)", f.sessions.updates, f.schedules.updates)
		}
	})
}
