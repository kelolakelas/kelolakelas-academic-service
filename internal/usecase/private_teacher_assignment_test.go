package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type assignmentSessionFailure struct{ *scopeSessionRepo }

func (s *assignmentSessionFailure) UpdateFutureSessionsTutor(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) error {
	return errors.New("session write failed")
}

type assignmentRollbackTx struct {
	repo       *assignmentScheduleStore
	rolledBack bool
}

func (tx *assignmentRollbackTx) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	previous := tx.repo.schedule
	err := fn(ctx)
	if err != nil {
		tx.repo.schedule = previous
		tx.rolledBack = true
	}
	return err
}

type assignmentScheduleStore struct{ *scopeScheduleRepo }

func (s *assignmentScheduleStore) Update(_ context.Context, row *domain.ClassSchedule) error {
	s.schedule = row
	return nil
}
func TestPrivateTeacherAssignmentRollbackAndRetry(t *testing.T) {
	tenant, teacher, enrollment := uuid.New(), uuid.New(), uuid.New()
	date := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	row := &domain.ClassSchedule{ID: uuid.New(), ClassID: uuid.New(), EnrollmentID: &enrollment, ValidFrom: &date}
	repo := &assignmentScheduleStore{&scopeScheduleRepo{ownerTenant: tenant, schedule: row}}
	tx := &assignmentRollbackTx{repo: repo}
	sessions := newScopeSessionRepo(tenant)
	membership := &scopeMembershipStub{active: true}
	req := &domain.PermanentTutorChangeRequest{ScheduleID: row.ID, NewTutorID: teacher, EffectiveDate: date}
	u := NewScheduleUsecase(tx, nil, repo, &assignmentSessionFailure{sessions}, nil, membership)
	if result, err := u.ChangeTutorPermanent(context.Background(), tenant, req); err == nil || result != nil || !tx.rolledBack || repo.schedule.TutorID != nil {
		t.Fatal("partial assignment must roll back")
	}
	u = NewScheduleUsecase(tx, nil, repo, sessions, nil, membership)
	for i := 0; i < 2; i++ {
		if _, err := u.ChangeTutorPermanent(context.Background(), tenant, req); err != nil {
			t.Fatal(err)
		}
	}
	if repo.creates != 0 || sessions.tutorUpdates != 1 || repo.schedule.ID != row.ID {
		t.Fatal("retry created resources or rewrote session substitutions")
	}
}

func TestPrivateTeacherAssignment(t *testing.T) {
	for _, tc := range []struct {
		name    string
		active  bool
		failure error
		want    error
	}{
		{name: "active", active: true},
		{name: "foreign or inactive", want: ErrSubstituteTutorNotEligible},
		{name: "identity failure", failure: errors.New("unavailable"), want: ErrSubstituteTutorUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant, teacher, enrollment := uuid.New(), uuid.New(), uuid.New()
			date := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
			schedule := &domain.ClassSchedule{ID: uuid.New(), ClassID: uuid.New(), EnrollmentID: &enrollment, ValidFrom: &date}
			repo := &scopeScheduleRepo{ownerTenant: tenant, schedule: schedule}
			sessions := newScopeSessionRepo(tenant)
			tx := &scopeTxStub{}
			membership := &scopeMembershipStub{active: tc.active, err: tc.failure}
			u := NewScheduleUsecase(tx, nil, repo, sessions, nil, membership)
			result, err := u.ChangeTutorPermanent(context.Background(), tenant, &domain.PermanentTutorChangeRequest{ScheduleID: schedule.ID, NewTutorID: teacher, EffectiveDate: date})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if tx.calls != 0 || repo.updates != 0 || sessions.tutorUpdates != 0 {
					t.Fatal("mutated before verification")
				}
				return
			}
			if result.NewSchedule.ID != schedule.ID || result.NewSchedule.EnrollmentID == nil || *result.NewSchedule.EnrollmentID != enrollment || *result.NewSchedule.TutorID != teacher || result.NewSchedule.ValidUntil != nil {
				t.Fatalf("unexpected assignment: %+v", result.NewSchedule)
			}
			if repo.creates != 0 || repo.updates != 1 || sessions.tutorUpdates != 1 || tx.calls != 1 {
				t.Fatal("assignment must update in one transaction, never create")
			}
			if membership.tenants[0] != tenant.String() || membership.members[0] != teacher.String() {
				t.Fatal("wrong membership scope")
			}
		})
	}
}
