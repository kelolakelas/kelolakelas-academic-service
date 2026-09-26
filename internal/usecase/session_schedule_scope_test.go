package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/gorm"
)

// KEL-17: every session/schedule mutation and the attendee read must be scoped
// to the calling tenant, and an id owned by another tenant has to be reported
// exactly like a missing one - with zero rows touched.
//
// The stubs below therefore model the ownership filter of the real SQL instead
// of trusting the caller: a scoped read answers gorm.ErrRecordNotFound when the
// tenant does not own the row, which is what the tenant predicate in the
// repository queries does in production. A use case that forgot to forward the
// caller's tenant would get a row back and fail these tests, and a use case that
// forgot the scoped read entirely would be caught by the write counters.

type scopeTxStub struct{ calls int }

func (m *scopeTxStub) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.calls++
	return fn(ctx)
}

// scopeScheduleRepo filters by tenant the same way GetByIDForTenant does: a
// schedule is only visible to the tenant that owns its class.
type scopeScheduleRepo struct {
	ownerTenant uuid.UUID
	schedule    *domain.ClassSchedule

	// readErr injects a transient failure and nilResult models a repository that
	// reports "no row" without an error.
	readErr   error
	nilResult bool

	updates int
	creates int
	reads   int
}

func (m *scopeScheduleRepo) Create(_ context.Context, schedule *domain.ClassSchedule) error {
	m.creates++
	m.schedule = schedule
	return nil
}

func (m *scopeScheduleRepo) ListByTenant(context.Context, uuid.UUID, domain.ListQuery) ([]domain.ClassSchedule, int64, error) {
	return nil, 0, nil
}

func (m *scopeScheduleRepo) BatchCreate(context.Context, []*domain.ClassSchedule) error { return nil }

func (m *scopeScheduleRepo) GetByID(context.Context, uuid.UUID) (*domain.ClassSchedule, error) {
	return m.schedule, nil
}

func (m *scopeScheduleRepo) GetByIDForTenant(_ context.Context, tenantID, id uuid.UUID) (*domain.ClassSchedule, error) {
	m.reads++
	if m.readErr != nil {
		return nil, m.readErr
	}
	if m.nilResult {
		return nil, nil
	}
	if m.schedule == nil || id != m.schedule.ID || tenantID != m.ownerTenant {
		return nil, gorm.ErrRecordNotFound
	}
	stored := *m.schedule
	return &stored, nil
}

func (m *scopeScheduleRepo) GetByIDForTenantForUpdate(ctx context.Context, tenantID, id uuid.UUID) (*domain.ClassSchedule, error) {
	return m.GetByIDForTenant(ctx, tenantID, id)
}

func (m *scopeScheduleRepo) Update(context.Context, *domain.ClassSchedule) error {
	m.updates++
	return nil
}

func (m *scopeScheduleRepo) DeleteByTenant(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (m *scopeScheduleRepo) DeleteByClass(context.Context, uuid.UUID, uuid.UUID) error  { return nil }

// scopeSessionRepo behaves like the scoped session queries: reads only resolve
// for the owning tenant and the bulk writes only affect rows of that tenant.
type scopeSessionRepo struct {
	ownerTenant uuid.UUID
	classTenant map[uuid.UUID]uuid.UUID
	sessions    map[uuid.UUID]*domain.ClassSession

	// readErr lets a test inject a transient failure; nilResult models a
	// repository that reports "no row" without an error.
	readErr   error
	nilResult bool

	updates          int
	creates          int
	batchCreates     int
	cancelBySchedule int
	cancelByClass    int
	tutorUpdates     int
	lastScopedTenant uuid.UUID
}

func newScopeSessionRepo(ownerTenant uuid.UUID, sessions ...*domain.ClassSession) *scopeSessionRepo {
	repo := &scopeSessionRepo{
		ownerTenant: ownerTenant,
		classTenant: map[uuid.UUID]uuid.UUID{},
		sessions:    map[uuid.UUID]*domain.ClassSession{},
	}
	for _, session := range sessions {
		repo.sessions[session.ID] = session
		repo.classTenant[session.ClassID] = ownerTenant
	}
	return repo
}

func (m *scopeSessionRepo) Create(_ context.Context, session *domain.ClassSession) error {
	m.creates++
	m.sessions[session.ID] = session
	m.classTenant[session.ClassID] = m.ownerTenant
	return nil
}

func (m *scopeSessionRepo) BatchCreate(_ context.Context, sessions []*domain.ClassSession) error {
	m.batchCreates++
	for _, session := range sessions {
		m.sessions[session.ID] = session
	}
	return nil
}

func (m *scopeSessionRepo) GetByID(context.Context, uuid.UUID) (*domain.ClassSession, error) {
	return nil, gorm.ErrRecordNotFound
}

func (m *scopeSessionRepo) GetByIDForTenant(_ context.Context, tenantID, id uuid.UUID) (*domain.ClassSession, error) {
	m.lastScopedTenant = tenantID
	if m.readErr != nil {
		return nil, m.readErr
	}
	if m.nilResult {
		return nil, nil
	}
	session, ok := m.sessions[id]
	if !ok || session == nil || m.classTenant[session.ClassID] != tenantID {
		return nil, gorm.ErrRecordNotFound
	}
	stored := *session
	return &stored, nil
}

func (m *scopeSessionRepo) DeleteByTenant(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (m *scopeSessionRepo) FindForAttendance(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (*domain.ClassSession, error) {
	return nil, gorm.ErrRecordNotFound
}

func (m *scopeSessionRepo) IsTutorForSession(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (m *scopeSessionRepo) ListByTenant(context.Context, uuid.UUID, domain.SessionQuery) ([]domain.ClassSession, int64, error) {
	return nil, 0, nil
}

func (m *scopeSessionRepo) Update(context.Context, *domain.ClassSession) error {
	m.updates++
	return nil
}

// CancelFutureSessionsBySchedule mirrors the SQL predicate: a foreign tenant
// matches zero rows, so nothing is written.
func (m *scopeSessionRepo) CancelFutureSessionsBySchedule(_ context.Context, tenantID, _ uuid.UUID, _ time.Time) error {
	if tenantID != m.ownerTenant {
		return nil
	}
	m.cancelBySchedule++
	return nil
}

func (m *scopeSessionRepo) CancelFutureSessionsByClass(_ context.Context, tenantID, _ uuid.UUID, _ time.Time) error {
	if tenantID != m.ownerTenant {
		return nil
	}
	m.cancelByClass++
	return nil
}

func (m *scopeSessionRepo) UpdateFutureSessionsTutor(_ context.Context, tenantID, _, _, _ uuid.UUID, _ time.Time) error {
	if tenantID != m.ownerTenant {
		return nil
	}
	m.tutorUpdates++
	return nil
}

// scopeEnrollmentRepo answers only for the owning tenant in both attendance
// branches, so a leak would have to show up as a non-zero call counter.
type scopeEnrollmentRepo struct {
	ownerTenant uuid.UUID
	byID        map[uuid.UUID]*domain.Enrollment
	bySchedule  map[uuid.UUID][]*domain.Enrollment

	accessCalls   int
	scheduleCalls int
}

func newScopeEnrollmentRepo(ownerTenant uuid.UUID) *scopeEnrollmentRepo {
	return &scopeEnrollmentRepo{
		ownerTenant: ownerTenant,
		byID:        map[uuid.UUID]*domain.Enrollment{},
		bySchedule:  map[uuid.UUID][]*domain.Enrollment{},
	}
}

func (m *scopeEnrollmentRepo) Create(context.Context, *domain.Enrollment) error { return nil }

func (m *scopeEnrollmentRepo) GetByID(context.Context, uuid.UUID) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}

func (m *scopeEnrollmentRepo) GetByIDForAccess(_ context.Context, tenantID, _ *uuid.UUID, id uuid.UUID) (*domain.Enrollment, error) {
	m.accessCalls++
	enrollment, ok := m.byID[id]
	if !ok || tenantID == nil || *tenantID != enrollment.TenantID {
		return nil, gorm.ErrRecordNotFound
	}
	return enrollment, nil
}

func (m *scopeEnrollmentRepo) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) ([]*domain.Enrollment, int64, error) {
	return nil, 0, nil
}

func (m *scopeEnrollmentRepo) ExistsActive(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (m *scopeEnrollmentRepo) GetByIdempotencyKey(context.Context, uuid.UUID, string) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}

func (m *scopeEnrollmentRepo) CreateIfCapacityAvailable(context.Context, *domain.Enrollment) error {
	return nil
}

func (m *scopeEnrollmentRepo) IsTutorForEnrollment(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (m *scopeEnrollmentRepo) GetActiveByClassID(context.Context, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, nil
}

func (m *scopeEnrollmentRepo) GetActiveByScheduleID(_ context.Context, tenantID, scheduleID uuid.UUID) ([]*domain.Enrollment, error) {
	m.scheduleCalls++
	if tenantID != m.ownerTenant {
		return nil, nil
	}
	return m.bySchedule[scheduleID], nil
}

func (m *scopeEnrollmentRepo) TransferSchedule(_ context.Context, tenantID, _, oldID, newID uuid.UUID) error {
	if tenantID != m.ownerTenant {
		return nil
	}
	for _, enrollment := range m.bySchedule[oldID] {
		if enrollment.Status == "active" || enrollment.Status == "pending" {
			enrollment.ScheduleID = &newID
			m.bySchedule[newID] = append(m.bySchedule[newID], enrollment)
		}
	}
	return nil
}
func (m *scopeEnrollmentRepo) AssignSchedule(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (m *scopeEnrollmentRepo) Update(context.Context, *domain.Enrollment) error           { return nil }
func (m *scopeEnrollmentRepo) Delete(context.Context, uuid.UUID) error                    { return nil }

type scopeFixture struct {
	ownTenant   uuid.UUID
	otherTenant uuid.UUID
	classID     uuid.UUID
	schedule    *domain.ClassSchedule
	session     *domain.ClassSession

	tx          *scopeTxStub
	schedules   *scopeScheduleRepo
	sessions    *scopeSessionRepo
	enrollments *scopeEnrollmentRepo
	usecase     ScheduleUsecase
}

func newScopeFixture() *scopeFixture {
	ownTenant, otherTenant := uuid.New(), uuid.New()
	classID := uuid.New()

	schedule := &domain.ClassSchedule{
		ID:        uuid.New(),
		ClassID:   classID,
		TutorID:   ptrToUUID(uuid.New()),
		Capacity:  10,
		DayOfWeek: 1,
		StartTime: "16:00:00",
		EndTime:   "17:00:00",
	}
	// The session belongs to the same class, so the same tenant owns both.
	session := &domain.ClassSession{
		ID:          uuid.New(),
		ClassID:     classID,
		ScheduleID:  &schedule.ID,
		TutorID:     *schedule.TutorID,
		SessionDate: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
		StartTime:   "16:00:00",
		EndTime:     "17:00:00",
		Status:      "scheduled",
	}

	tx := &scopeTxStub{}
	schedules := &scopeScheduleRepo{ownerTenant: ownTenant, schedule: schedule}
	sessions := newScopeSessionRepo(ownTenant, session)
	enrollments := newScopeEnrollmentRepo(ownTenant)

	return &scopeFixture{
		ownTenant:   ownTenant,
		otherTenant: otherTenant,
		classID:     classID,
		schedule:    schedule,
		session:     session,
		tx:          tx,
		schedules:   schedules,
		sessions:    sessions,
		enrollments: enrollments,
		usecase:     NewScheduleUsecase(tx, nil, schedules, sessions, enrollments),
	}
}

// writes reports how many rows the fixture observed being written, so a test can
// assert "no data change" without inspecting individual counters.
func (f *scopeFixture) writes() int {
	return f.schedules.updates + f.schedules.creates +
		f.sessions.updates + f.sessions.creates + f.sessions.batchCreates +
		f.sessions.cancelBySchedule + f.sessions.cancelByClass + f.sessions.tutorUpdates
}

func ptrToUUID(id uuid.UUID) *uuid.UUID { return &id }

func rescheduleRequest(sessionID uuid.UUID) *domain.RescheduleSessionRequest {
	return &domain.RescheduleSessionRequest{
		SessionID:      sessionID,
		NewSessionDate: time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC),
		NewStartTime:   "18:00:00",
		NewEndTime:     "19:00:00",
	}
}

func permanentScheduleRequest(scheduleID uuid.UUID) *domain.PermanentScheduleChangeRequest {
	return &domain.PermanentScheduleChangeRequest{
		OldScheduleID: scheduleID,
		NewDayOfWeek:  3,
		NewStartTime:  "10:00:00",
		NewEndTime:    "11:00:00",
		EffectiveDate: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
	}
}

func substituteTutorRequest(sessionID, tutorID uuid.UUID) *domain.SubstituteTutorRequest {
	return &domain.SubstituteTutorRequest{SessionID: sessionID, SubstituteTutorID: tutorID}
}

func permanentTutorRequest(scheduleID, tutorID uuid.UUID) *domain.PermanentTutorChangeRequest {
	return &domain.PermanentTutorChangeRequest{
		ScheduleID:    scheduleID,
		NewTutorID:    tutorID,
		EffectiveDate: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
	}
}

// AC: a tenant member must not be able to reschedule, permanently change,
// substitute a tutor for, or read attendees of another tenant's session.
func TestSessionMutationsRejectAnotherTenantsResources(t *testing.T) {
	t.Run("reschedule session", func(t *testing.T) {
		f := newScopeFixture()
		res, err := f.usecase.RescheduleSession(context.Background(), f.otherTenant, rescheduleRequest(f.session.ID))
		if !errors.Is(err, ErrSessionNotFound) || res != nil {
			t.Fatalf("res=%+v err=%v want ErrSessionNotFound", res, err)
		}
		if f.writes() != 0 {
			t.Fatalf("writes=%d want=0 for a session owned by another tenant", f.writes())
		}
	})

	t.Run("permanent schedule change", func(t *testing.T) {
		f := newScopeFixture()
		res, err := f.usecase.ChangeSchedulePermanent(context.Background(), f.otherTenant, permanentScheduleRequest(f.schedule.ID))
		if !errors.Is(err, ErrScheduleNotFound) || res != nil {
			t.Fatalf("res=%+v err=%v want ErrScheduleNotFound", res, err)
		}
		if f.writes() != 0 {
			t.Fatalf("writes=%d want=0 for a schedule owned by another tenant", f.writes())
		}
	})

	t.Run("substitute tutor", func(t *testing.T) {
		f := newScopeFixture()
		res, err := f.usecase.ChangeTutorTemporary(context.Background(), f.otherTenant, substituteTutorRequest(f.session.ID, uuid.New()))
		if !errors.Is(err, ErrSessionNotFound) || res != nil {
			t.Fatalf("res=%+v err=%v want ErrSessionNotFound", res, err)
		}
		if f.writes() != 0 {
			t.Fatalf("writes=%d want=0 for a session owned by another tenant", f.writes())
		}
	})

	t.Run("permanent tutor change", func(t *testing.T) {
		f := newScopeFixture()
		res, err := f.usecase.ChangeTutorPermanent(context.Background(), f.otherTenant, permanentTutorRequest(f.schedule.ID, uuid.New()))
		if !errors.Is(err, ErrScheduleNotFound) || res != nil {
			t.Fatalf("res=%+v err=%v want ErrScheduleNotFound", res, err)
		}
		if f.writes() != 0 {
			t.Fatalf("writes=%d want=0 for a schedule owned by another tenant", f.writes())
		}
	})

	t.Run("session attendees", func(t *testing.T) {
		f := newScopeFixture()
		attendees, err := f.usecase.GetSessionAttendees(context.Background(), f.otherTenant, f.session.ID)
		if !errors.Is(err, ErrSessionNotFound) || attendees != nil {
			t.Fatalf("attendees=%+v err=%v want ErrSessionNotFound", attendees, err)
		}
		// No student record may be read once the session is out of scope.
		if f.enrollments.accessCalls != 0 || f.enrollments.scheduleCalls != 0 {
			t.Fatalf("enrollment reads=(access=%d schedule=%d) want=(0,0)",
				f.enrollments.accessCalls, f.enrollments.scheduleCalls)
		}
	})
}

// The same five operations must keep working for the tenant that owns the
// session/schedule; without this the cross-tenant assertions above would also
// pass on a use case that simply rejected everything.
func TestSessionMutationsSucceedForTheOwningTenant(t *testing.T) {
	t.Run("reschedule session", func(t *testing.T) {
		f := newScopeFixture()
		res, err := f.usecase.RescheduleSession(context.Background(), f.ownTenant, rescheduleRequest(f.session.ID))
		if err != nil {
			t.Fatalf("RescheduleSession error: %v", err)
		}
		if res.OriginalSession == nil || res.OriginalSession.Status != "rescheduled" {
			t.Fatalf("original session=%+v want status=rescheduled", res.OriginalSession)
		}
		if res.NewSession == nil || res.NewSession.ScheduleID != nil || res.NewSession.Status != "scheduled" {
			t.Fatalf("new session=%+v want schedule_id=nil status=scheduled", res.NewSession)
		}
		if res.NewSession.ClassID != f.session.ClassID || res.NewSession.TutorID != f.session.TutorID {
			t.Fatalf("new session=%+v want class_id=%s tutor_id=%s", res.NewSession, f.session.ClassID, f.session.TutorID)
		}
		if res.NewSession.SessionDate != time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC) {
			t.Fatalf("new session date=%s want=2026-09-21", res.NewSession.SessionDate)
		}
		if f.sessions.updates != 1 || f.sessions.creates != 1 {
			t.Fatalf("updates=%d creates=%d want=(1,1)", f.sessions.updates, f.sessions.creates)
		}
	})

	t.Run("permanent schedule change", func(t *testing.T) {
		f := newScopeFixture()
		res, err := f.usecase.ChangeSchedulePermanent(context.Background(), f.ownTenant, permanentScheduleRequest(f.schedule.ID))
		if err != nil {
			t.Fatalf("ChangeSchedulePermanent error: %v", err)
		}
		wantValidUntil := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
		if res.OldSchedule == nil || res.OldSchedule.ValidUntil == nil || !res.OldSchedule.ValidUntil.Equal(wantValidUntil) {
			t.Fatalf("old schedule=%+v want valid_until=%s", res.OldSchedule, wantValidUntil)
		}
		if res.NewSchedule == nil || res.NewSchedule.DayOfWeek != 3 || res.NewSchedule.ClassID != f.classID {
			t.Fatalf("new schedule=%+v want day_of_week=3 class_id=%s", res.NewSchedule, f.classID)
		}
		// The future-session cancellation is an unconditional tenant-scoped write,
		// so it proves the owner's schedule was actually mutated.
		if f.schedules.updates != 1 || f.schedules.creates != 1 || f.sessions.cancelBySchedule != 1 {
			t.Fatalf("schedule writes=(updates=%d creates=%d) session cancels=%d want=(1,1,1)",
				f.schedules.updates, f.schedules.creates, f.sessions.cancelBySchedule)
		}
		if len(res.NewSessions) != 3 || res.NewSchedule.Capacity != f.schedule.Capacity {
			t.Fatalf("new sessions=%d capacity=%d want 3 and %d", len(res.NewSessions), res.NewSchedule.Capacity, f.schedule.Capacity)
		}
	})

	t.Run("substitute tutor", func(t *testing.T) {
		f := newScopeFixture()
		substitute := uuid.New()
		res, err := f.usecase.ChangeTutorTemporary(context.Background(), f.ownTenant, substituteTutorRequest(f.session.ID, substitute))
		if err != nil {
			t.Fatalf("ChangeTutorTemporary error: %v", err)
		}
		if res.Session == nil || res.Session.TutorID != substitute {
			t.Fatalf("session=%+v want tutor_id=%s", res.Session, substitute)
		}
		// Only the session row may change; the schedule must stay untouched.
		if f.sessions.updates != 1 || f.schedules.updates != 0 {
			t.Fatalf("session updates=%d schedule updates=%d want=(1,0)", f.sessions.updates, f.schedules.updates)
		}
	})

	t.Run("permanent tutor change", func(t *testing.T) {
		f := newScopeFixture()
		newTutor := uuid.New()
		res, err := f.usecase.ChangeTutorPermanent(context.Background(), f.ownTenant, permanentTutorRequest(f.schedule.ID, newTutor))
		if err != nil {
			t.Fatalf("ChangeTutorPermanent error: %v", err)
		}
		if res.NewSchedule == nil || res.NewSchedule.TutorID == nil || *res.NewSchedule.TutorID != newTutor {
			t.Fatalf("new schedule=%+v want tutor_id=%s", res.NewSchedule, newTutor)
		}
		if res.OldSchedule == nil || res.OldSchedule.ValidUntil == nil {
			t.Fatalf("old schedule=%+v want valid_until set", res.OldSchedule)
		}
		if f.schedules.updates != 1 || f.schedules.creates != 1 || f.sessions.tutorUpdates != 1 {
			t.Fatalf("schedule writes=(updates=%d creates=%d) session tutor updates=%d want all 1",
				f.schedules.updates, f.schedules.creates, f.sessions.tutorUpdates)
		}
	})
}

func TestPermanentChangeRejectsEffectiveDateOutsideOriginalValidity(t *testing.T) {
	for _, tutor := range []bool{false, true} {
		for _, date := range []time.Time{
			time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		} {
			f := newScopeFixture()
			start := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
			until := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
			f.schedule.ValidFrom, f.schedule.ValidUntil = &start, &until
			var err error
			if tutor {
				req := permanentTutorRequest(f.schedule.ID, uuid.New())
				req.EffectiveDate = date
				_, err = f.usecase.ChangeTutorPermanent(context.Background(), f.ownTenant, req)
			} else {
				req := permanentScheduleRequest(f.schedule.ID)
				req.EffectiveDate = date
				_, err = f.usecase.ChangeSchedulePermanent(context.Background(), f.ownTenant, req)
			}
			if err == nil || f.writes() != 0 || !f.schedule.ValidUntil.Equal(until) {
				t.Fatalf("tutor=%v date=%s err=%v writes=%d", tutor, date, err, f.writes())
			}
		}
	}
}

// The private-class branch must not hand out the enrollment of another tenant
// even when the session itself is readable, and the group branch must ask for
// the caller's tenant.
func TestSessionAttendeesAreScopedToTheCallingTenant(t *testing.T) {
	t.Run("private class reads own enrollment", func(t *testing.T) {
		f := newScopeFixture()
		enrollmentID := uuid.New()
		f.sessions.sessions[f.session.ID].EnrollmentID = &enrollmentID
		f.sessions.sessions[f.session.ID].ScheduleID = nil
		f.enrollments.byID[enrollmentID] = &domain.Enrollment{ID: enrollmentID, TenantID: f.ownTenant}

		attendees, err := f.usecase.GetSessionAttendees(context.Background(), f.ownTenant, f.session.ID)
		if err != nil {
			t.Fatalf("GetSessionAttendees error: %v", err)
		}
		if len(attendees) != 1 || attendees[0].ID != enrollmentID {
			t.Fatalf("attendees=%+v want the enrollment %s", attendees, enrollmentID)
		}
		if f.enrollments.scheduleCalls != 0 {
			t.Fatalf("group lookup ran for a private session (calls=%d)", f.enrollments.scheduleCalls)
		}
	})

	t.Run("group class lists schedule enrollments", func(t *testing.T) {
		f := newScopeFixture()
		enrollmentID := uuid.New()
		f.enrollments.bySchedule[f.schedule.ID] = []*domain.Enrollment{{ID: enrollmentID, TenantID: f.ownTenant}}

		attendees, err := f.usecase.GetSessionAttendees(context.Background(), f.ownTenant, f.session.ID)
		if err != nil {
			t.Fatalf("GetSessionAttendees error: %v", err)
		}
		if len(attendees) != 1 || attendees[0].ID != enrollmentID {
			t.Fatalf("attendees=%+v want the enrollment %s", attendees, enrollmentID)
		}
		if f.enrollments.accessCalls != 0 {
			t.Fatalf("private lookup ran for a group session (calls=%d)", f.enrollments.accessCalls)
		}
	})

	t.Run("group class never lists another tenants enrollments", func(t *testing.T) {
		f := newScopeFixture()
		f.enrollments.bySchedule[f.schedule.ID] = []*domain.Enrollment{{ID: uuid.New(), TenantID: f.otherTenant}}

		attendees, err := f.usecase.GetSessionAttendees(context.Background(), f.otherTenant, f.session.ID)
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("err=%v want ErrSessionNotFound", err)
		}
		if attendees != nil {
			t.Fatalf("attendees=%+v want nil", attendees)
		}
		if f.enrollments.scheduleCalls != 0 {
			t.Fatalf("schedule enrollments read for a foreign tenant (calls=%d)", f.enrollments.scheduleCalls)
		}
	})
}

// A missing session or schedule keeps answering "not found" rather than an
// internal error, so the scoped read must not be confused with a real failure.
func TestScopedReadsMapMissingRowsToNotFound(t *testing.T) {
	t.Run("unknown session", func(t *testing.T) {
		f := newScopeFixture()
		if _, err := f.usecase.RescheduleSession(context.Background(), f.ownTenant, rescheduleRequest(uuid.New())); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("err=%v want ErrSessionNotFound", err)
		}
		if f.writes() != 0 {
			t.Fatalf("writes=%d want=0", f.writes())
		}
	})

	t.Run("unknown schedule", func(t *testing.T) {
		f := newScopeFixture()
		if _, err := f.usecase.ChangeTutorPermanent(context.Background(), f.ownTenant, permanentTutorRequest(uuid.New(), uuid.New())); !errors.Is(err, ErrScheduleNotFound) {
			t.Fatalf("err=%v want ErrScheduleNotFound", err)
		}
		if f.writes() != 0 {
			t.Fatalf("writes=%d want=0", f.writes())
		}
	})

	t.Run("deleted nil session is not a repository error", func(t *testing.T) {
		f := newScopeFixture()
		// A scoped read that returns no row and no error must still be treated as
		// "not found", never as a nil-pointer success.
		f.sessions.nilResult = true
		if _, err := f.usecase.ChangeTutorTemporary(context.Background(), f.ownTenant, substituteTutorRequest(f.session.ID, uuid.New())); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("err=%v want ErrSessionNotFound", err)
		}
		if f.writes() != 0 {
			t.Fatalf("writes=%d want=0", f.writes())
		}
	})
}

// Every mutation has to run inside a transaction, including the temporary tutor
// substitution which previously wrote without one.
func TestSessionMutationsRunInATransaction(t *testing.T) {
	cases := []struct {
		name string
		run  func(f *scopeFixture) error
	}{
		{name: "reschedule session", run: func(f *scopeFixture) error {
			_, err := f.usecase.RescheduleSession(context.Background(), f.ownTenant, rescheduleRequest(f.session.ID))
			return err
		}},
		{name: "permanent schedule change", run: func(f *scopeFixture) error {
			_, err := f.usecase.ChangeSchedulePermanent(context.Background(), f.ownTenant, permanentScheduleRequest(f.schedule.ID))
			return err
		}},
		{name: "substitute tutor", run: func(f *scopeFixture) error {
			_, err := f.usecase.ChangeTutorTemporary(context.Background(), f.ownTenant, substituteTutorRequest(f.session.ID, uuid.New()))
			return err
		}},
		{name: "permanent tutor change", run: func(f *scopeFixture) error {
			_, err := f.usecase.ChangeTutorPermanent(context.Background(), f.ownTenant, permanentTutorRequest(f.schedule.ID, uuid.New()))
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newScopeFixture()
			if err := tc.run(f); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if f.tx.calls != 1 {
				t.Fatalf("transactions=%d want=1", f.tx.calls)
			}
		})
	}
}

// A repository failure must surface instead of being masked as "not found", and
// no write may happen once the transaction is being unwound.
func TestScopedReadsPropagateRepositoryFailures(t *testing.T) {
	boom := errors.New("connection reset")

	cases := []struct {
		name string
		run  func(f *scopeFixture) error
	}{
		{name: "reschedule session", run: func(f *scopeFixture) error {
			_, err := f.usecase.RescheduleSession(context.Background(), f.ownTenant, rescheduleRequest(f.session.ID))
			return err
		}},
		{name: "substitute tutor", run: func(f *scopeFixture) error {
			_, err := f.usecase.ChangeTutorTemporary(context.Background(), f.ownTenant, substituteTutorRequest(f.session.ID, uuid.New()))
			return err
		}},
		{name: "permanent schedule change", run: func(f *scopeFixture) error {
			_, err := f.usecase.ChangeSchedulePermanent(context.Background(), f.ownTenant, permanentScheduleRequest(f.schedule.ID))
			return err
		}},
		{name: "permanent tutor change", run: func(f *scopeFixture) error {
			_, err := f.usecase.ChangeTutorPermanent(context.Background(), f.ownTenant, permanentTutorRequest(f.schedule.ID, uuid.New()))
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name+" propagates the error", func(t *testing.T) {
			f := newScopeFixture()
			f.sessions.readErr = boom
			f.schedules.readErr = boom
			if err := tc.run(f); !errors.Is(err, boom) {
				t.Fatalf("err=%v want the repository error %v", err, boom)
			}
			if f.writes() != 0 {
				t.Fatalf("writes=%d want=0 after a failed scoped read", f.writes())
			}
		})
	}
}
