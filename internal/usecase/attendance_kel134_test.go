package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

// kel134SessionRepo is a controllable SessionRepository for KEL-134: sessions
// resolve only for the owning tenant, mirroring the SQL ownership filter.
type kel134SessionRepo struct {
	repository.SessionRepository
	ownerTenant uuid.UUID
	sessions    map[uuid.UUID]*domain.ClassSession
	assigned    map[string]bool
	findErr     error
	cohortErr   error
	origins     []domain.ClassSession
}

func (r *kel134SessionRepo) FindSessionForAttendance(_ context.Context, tenantID, id uuid.UUID) (*domain.ClassSession, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	session, ok := r.sessions[id]
	if !ok || tenantID != r.ownerTenant {
		return nil, gorm.ErrRecordNotFound
	}
	stored := *session
	return &stored, nil
}

func (r *kel134SessionRepo) ListSessionsForAttendanceCohort(_ context.Context, tenantID, _ uuid.UUID, _ string) ([]domain.ClassSession, error) {
	if r.cohortErr != nil {
		return nil, r.cohortErr
	}
	if tenantID != r.ownerTenant {
		return nil, nil
	}
	return r.origins, nil
}

func (r *kel134SessionRepo) IsTutorForSession(_ context.Context, tenantID, sessionID, memberID uuid.UUID) (bool, error) {
	if tenantID != r.ownerTenant {
		return false, nil
	}
	return r.assigned[sessionID.String()+memberID.String()], nil
}

type kel134AttendanceRepo struct {
	repository.AttendanceRepository
	byPair  map[string]*domain.Attendance
	created []*domain.Attendance
	bulkOut []domain.Attendance
	bulkErr error
}

func kel134Key(sessionID, enrollmentID uuid.UUID) string {
	return sessionID.String() + "/" + enrollmentID.String()
}

func (r *kel134AttendanceRepo) GetBySessionEnrollment(_ context.Context, sessionID, enrollmentID uuid.UUID) (*domain.Attendance, error) {
	if item, ok := r.byPair[kel134Key(sessionID, enrollmentID)]; ok {
		return item, nil
	}
	return nil, nil
}

func (r *kel134AttendanceRepo) UpsertBulk(_ context.Context, _ uuid.UUID, sessionID uuid.UUID, items []domain.BulkAttendanceItem) ([]domain.Attendance, error) {
	if r.bulkErr != nil {
		return nil, r.bulkErr
	}
	out := make([]domain.Attendance, 0, len(items))
	for _, item := range items {
		row := &domain.Attendance{ID: uuid.New(), EnrollmentID: item.EnrollmentID, SessionID: sessionID, Status: item.Status}
		r.byPair[kel134Key(sessionID, item.EnrollmentID)] = row
		out = append(out, *row)
	}
	r.bulkOut = out
	return out, nil
}

func (r *kel134AttendanceRepo) Create(_ context.Context, attendance *domain.Attendance) error {
	r.created = append(r.created, attendance)
	r.byPair[kel134Key(attendance.SessionID, attendance.EnrollmentID)] = attendance
	return nil
}

type kel134EnrollmentRepo struct {
	repository.EnrollmentRepository
	live       map[uuid.UUID]*domain.Enrollment
	bySchedule map[uuid.UUID][]*domain.Enrollment
	schedErr   error
}

func (r *kel134EnrollmentRepo) GetByIDForAccess(_ context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.Enrollment, error) {
	if e, ok := r.live[id]; ok && tenantID != nil && *tenantID == e.TenantID && parentID == nil {
		return e, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *kel134EnrollmentRepo) GetActiveByScheduleID(_ context.Context, _ uuid.UUID, scheduleID uuid.UUID) ([]*domain.Enrollment, error) {
	if r.schedErr != nil {
		return nil, r.schedErr
	}
	return r.bySchedule[scheduleID], nil
}

type kel134Fixture struct {
	tenant   uuid.UUID
	other    uuid.UUID
	tutor    uuid.UUID
	stranger uuid.UUID
	classID  uuid.UUID
	schedule uuid.UUID
	session  *domain.ClassSession
	sessions *kel134SessionRepo
	attend   *kel134AttendanceRepo
	enroll   *kel134EnrollmentRepo
	usecase  AttendanceUsecase
	extra    map[string]uuid.UUID
}

// newKEL134Fixture builds a group class: one schedule, two live enrollments
// on it, one foreign enrollment (other class), one suspended enrollment, and
// one regular group session owned by the fixture tenant and taught by the
// fixture tutor.
func newKEL134Fixture() *kel134Fixture {
	tenant, other := uuid.New(), uuid.New()
	tutor, stranger := uuid.New(), uuid.New()
	classID, schedule := uuid.New(), uuid.New()
	sessionID := uuid.New()
	enrollA, enrollB, foreign, suspended := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	session := &domain.ClassSession{
		ID: sessionID, ClassID: classID, ScheduleID: &schedule, TutorID: tutor,
		SessionDate: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		StartTime:   "16:00:00", EndTime: "17:00:00", Status: "scheduled",
	}
	sessions := &kel134SessionRepo{
		ownerTenant: tenant,
		sessions:    map[uuid.UUID]*domain.ClassSession{sessionID: session},
		assigned:    map[string]bool{sessionID.String() + tutor.String(): true},
	}
	attend := &kel134AttendanceRepo{byPair: map[string]*domain.Attendance{}}
	enroll := &kel134EnrollmentRepo{
		live: map[uuid.UUID]*domain.Enrollment{
			enrollA: {ID: enrollA, TenantID: tenant, ClassID: classID, Status: "active"},
			enrollB: {ID: enrollB, TenantID: tenant, ClassID: classID, Status: "active"},
		},
		bySchedule: map[uuid.UUID][]*domain.Enrollment{
			schedule: {
				{ID: enrollA, TenantID: tenant, ClassID: classID, ScheduleID: &schedule, Status: "active"},
				{ID: enrollB, TenantID: tenant, ClassID: classID, ScheduleID: &schedule, Status: "active"},
			},
		},
	}
	return &kel134Fixture{
		tenant: tenant, other: other, tutor: tutor, stranger: stranger,
		classID: classID, schedule: schedule, session: session,
		sessions: sessions, attend: attend, enroll: enroll,
		usecase: NewAttendanceUsecase(attend, sessions, enroll),
		extra:   map[string]uuid.UUID{"a": enrollA, "b": enrollB, "foreign": foreign, "suspended": suspended},
	}
}

func (f *kel134Fixture) liveIDs() []uuid.UUID {
	var ids []uuid.UUID
	for _, e := range f.enroll.bySchedule[f.schedule] {
		ids = append(ids, e.ID)
	}
	return ids
}

// addReplacement registers a reschedule replacement session linked to the
// fixture's group session, taught by the same tutor.
func (f *kel134Fixture) addReplacement() *domain.ClassSession {
	replacement := &domain.ClassSession{
		ID: uuid.New(), ClassID: f.classID, ScheduleID: nil, EnrollmentID: nil,
		RescheduledFromSessionID: &f.session.ID, TutorID: f.tutor,
		SessionDate: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		StartTime:   "18:00:00", EndTime: "19:00:00", Status: "scheduled",
	}
	f.sessions.sessions[replacement.ID] = replacement
	f.sessions.assigned[replacement.ID.String()+f.tutor.String()] = true
	return replacement
}

func bulkItems(ids []uuid.UUID, status string) []domain.BulkAttendanceItem {
	items := make([]domain.BulkAttendanceItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, domain.BulkAttendanceItem{EnrollmentID: id, Status: status})
	}
	return items
}

// AC1: attendance for a reschedule replacement can be created and read.
func TestKEL134RescheduleReplacementCreateAndRead(t *testing.T) {
	f := newKEL134Fixture()
	replacement := f.addReplacement()
	enrollA := f.extra["a"]

	created, err := f.usecase.CreateBySession(context.Background(), f.tenant, f.tutor, &domain.CreateAttendanceBySessionRequest{
		EnrollmentID: enrollA, SessionID: replacement.ID, Status: "present",
	})
	if err != nil {
		t.Fatalf("CreateBySession error: %v", err)
	}
	if created.SessionID != replacement.ID || created.EnrollmentID != enrollA || created.Status != "present" {
		t.Fatalf("created=%+v want replacement session %s enrollment %s present", created, replacement.ID, enrollA)
	}

	read, err := f.usecase.GetBySession(context.Background(), f.tenant, replacement.ID, enrollA)
	if err != nil {
		t.Fatalf("GetBySession error: %v", err)
	}
	if read.Status != "present" {
		t.Fatalf("read status=%q want present", read.Status)
	}

	// The session_id form of the legacy endpoint addresses the replacement too.
	viaLegacy, err := f.usecase.Create(context.Background(), f.tenant, f.tutor, &domain.CreateAttendanceRequest{
		EnrollmentID: f.extra["b"], SessionID: &replacement.ID, Status: "late",
	})
	if err != nil {
		t.Fatalf("Create with session_id error: %v", err)
	}
	if viaLegacy.SessionID != replacement.ID {
		t.Fatalf("legacy session_id write landed on %s, want %s", viaLegacy.SessionID, replacement.ID)
	}
}

// AC1 (private): a private reschedule replacement keeps the single-student
// cohort through the inherited enrollment id.
func TestKEL134PrivateRescheduleReplacement(t *testing.T) {
	f := newKEL134Fixture()
	enrollP := uuid.New()
	origin := &domain.ClassSession{
		ID: uuid.New(), ClassID: f.classID, ScheduleID: nil, EnrollmentID: &enrollP, TutorID: f.tutor,
		SessionDate: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		StartTime:   "16:00:00", EndTime: "17:00:00", Status: "rescheduled",
	}
	replacement := &domain.ClassSession{
		ID: uuid.New(), ClassID: f.classID, ScheduleID: nil, EnrollmentID: &enrollP,
		RescheduledFromSessionID: &origin.ID, TutorID: f.tutor,
		SessionDate: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		StartTime:   "18:00:00", EndTime: "19:00:00", Status: "scheduled",
	}
	f.sessions.sessions[origin.ID] = origin
	f.sessions.sessions[replacement.ID] = replacement
	f.sessions.assigned[replacement.ID.String()+f.tutor.String()] = true
	f.enroll.live[enrollP] = &domain.Enrollment{ID: enrollP, TenantID: f.tenant, ClassID: f.classID, Status: "active"}

	created, err := f.usecase.CreateBySession(context.Background(), f.tenant, f.tutor, &domain.CreateAttendanceBySessionRequest{
		EnrollmentID: enrollP, SessionID: replacement.ID, Status: "excused",
	})
	if err != nil {
		t.Fatalf("CreateBySession error: %v", err)
	}
	if created.EnrollmentID != enrollP {
		t.Fatalf("created enrollment=%s want %s", created.EnrollmentID, enrollP)
	}
	if _, err := f.usecase.CreateBySession(context.Background(), f.tenant, f.tutor, &domain.CreateAttendanceBySessionRequest{
		EnrollmentID: f.extra["a"], SessionID: replacement.ID, Status: "present",
	}); !errors.Is(err, ErrAttendanceEnrollmentMismatch) {
		t.Fatalf("foreign enrollment err=%v want ErrAttendanceEnrollmentMismatch", err)
	}
}

// AC2: one bulk request records the whole session; repeating it updates
// instead of duplicating.
func TestKEL134BulkIdempotent(t *testing.T) {
	f := newKEL134Fixture()
	ids := f.liveIDs()
	first, err := f.usecase.CreateBulk(context.Background(), f.tenant, f.tutor, &domain.BulkAttendanceRequest{
		SessionID: f.session.ID, Items: bulkItems(ids, "present"),
	})
	if err != nil {
		t.Fatalf("CreateBulk error: %v", err)
	}
	if len(first.Attendances) != 2 {
		t.Fatalf("bulk rows=%d want 2", len(first.Attendances))
	}
	second, err := f.usecase.CreateBulk(context.Background(), f.tenant, f.tutor, &domain.BulkAttendanceRequest{
		SessionID: f.session.ID, Items: bulkItems(ids, "late"),
	})
	if err != nil {
		t.Fatalf("repeat CreateBulk error: %v", err)
	}
	if len(second.Attendances) != 2 {
		t.Fatalf("repeat bulk rows=%d want 2", len(second.Attendances))
	}
	for _, row := range second.Attendances {
		if row.Status != "late" {
			t.Fatalf("row %s status=%q want late (repeat updates)", row.EnrollmentID, row.Status)
		}
	}
	if len(f.attend.byPair) != 2 {
		t.Fatalf("stored rows=%d want 2 (no duplicates)", len(f.attend.byPair))
	}
	// Bulk on the reschedule replacement covers the origin cohort.
	replacement := f.addReplacement()
	repResult, err := f.usecase.CreateBulk(context.Background(), f.tenant, f.tutor, &domain.BulkAttendanceRequest{
		SessionID: replacement.ID, Items: bulkItems(ids, "present"),
	})
	if err != nil {
		t.Fatalf("replacement CreateBulk error: %v", err)
	}
	if len(repResult.Attendances) != 2 {
		t.Fatalf("replacement bulk rows=%d want 2", len(repResult.Attendances))
	}
}

// AC3: a tutor who does not teach the session is refused, and an enrollment
// outside the cohort is a validation error.
func TestKEL134ForbiddenTutorAndForeignEnrollment(t *testing.T) {
	f := newKEL134Fixture()
	enrollA := f.extra["a"]
	if _, err := f.usecase.CreateBySession(context.Background(), f.tenant, f.stranger, &domain.CreateAttendanceBySessionRequest{
		EnrollmentID: enrollA, SessionID: f.session.ID, Status: "present",
	}); !errors.Is(err, domain.ErrAttendanceForbidden) {
		t.Fatalf("stranger tutor err=%v want ErrAttendanceForbidden", err)
	}
	if _, err := f.usecase.CreateBySession(context.Background(), f.tenant, f.tutor, &domain.CreateAttendanceBySessionRequest{
		EnrollmentID: f.extra["foreign"], SessionID: f.session.ID, Status: "present",
	}); !errors.Is(err, ErrAttendanceEnrollmentMismatch) {
		t.Fatalf("foreign enrollment err=%v want ErrAttendanceEnrollmentMismatch", err)
	}
	if _, err := f.usecase.CreateBulk(context.Background(), f.tenant, f.stranger, &domain.BulkAttendanceRequest{
		SessionID: f.session.ID, Items: bulkItems([]uuid.UUID{enrollA}, "present"),
	}); !errors.Is(err, domain.ErrAttendanceForbidden) {
		t.Fatalf("bulk stranger tutor err=%v want ErrAttendanceForbidden", err)
	}
	if _, err := f.usecase.CreateBulk(context.Background(), f.tenant, f.tutor, &domain.BulkAttendanceRequest{
		SessionID: f.session.ID, Items: bulkItems([]uuid.UUID{enrollA, f.extra["foreign"]}, "present"),
	}); !errors.Is(err, ErrAttendanceEnrollmentMismatch) {
		t.Fatalf("bulk foreign enrollment err=%v want ErrAttendanceEnrollmentMismatch", err)
	}
	// Another tenant's session answers as missing and stores nothing.
	if _, err := f.usecase.CreateBySession(context.Background(), f.other, f.tutor, &domain.CreateAttendanceBySessionRequest{
		EnrollmentID: enrollA, SessionID: f.session.ID, Status: "present",
	}); !errors.Is(err, ErrAttendanceSessionNotFound) {
		t.Fatalf("cross-tenant err=%v want ErrAttendanceSessionNotFound", err)
	}
	if len(f.attend.created) != 0 {
		t.Fatalf("cross-tenant stored %d rows, want 0", len(f.attend.created))
	}
}

// Cancelled sessions and suspended enrollments refuse writes with a
// validation verdict, not "not found".
func TestKEL134CancelledSessionAndSuspendedEnrollment(t *testing.T) {
	f := newKEL134Fixture()
	enrollA := f.extra["a"]
	cancelled := *f.session
	cancelled.Status = "cancelled"
	f.sessions.sessions[cancelled.ID] = &cancelled
	if _, err := f.usecase.CreateBySession(context.Background(), f.tenant, f.tutor, &domain.CreateAttendanceBySessionRequest{
		EnrollmentID: enrollA, SessionID: cancelled.ID, Status: "present",
	}); !errors.Is(err, ErrAttendanceSessionCancelled) {
		t.Fatalf("cancelled session err=%v want ErrAttendanceSessionCancelled", err)
	}

	suspendedID := f.extra["suspended"]
	f.enroll.bySchedule[f.schedule] = append(f.enroll.bySchedule[f.schedule],
		&domain.Enrollment{ID: suspendedID, TenantID: f.tenant, ClassID: f.classID, ScheduleID: &f.schedule, Status: domain.EnrollmentStatusSuspended})
	// Suspended enrollments are not part of the live cohort.
	f.enroll.bySchedule[f.schedule] = f.enroll.bySchedule[f.schedule][:2]
	f.sessions.sessions[f.session.ID] = f.session
	if _, err := f.usecase.CreateBySession(context.Background(), f.tenant, f.tutor, &domain.CreateAttendanceBySessionRequest{
		EnrollmentID: suspendedID, SessionID: f.session.ID, Status: "present",
	}); !errors.Is(err, ErrAttendanceEnrollmentMismatch) {
		t.Fatalf("suspended enrollment err=%v want ErrAttendanceEnrollmentMismatch", err)
	}
}

// Duplicate single writes are refused.
func TestKEL134DuplicateSingleWrite(t *testing.T) {
	f := newKEL134Fixture()
	enrollA := f.extra["a"]
	if _, err := f.usecase.CreateBySession(context.Background(), f.tenant, f.tutor, &domain.CreateAttendanceBySessionRequest{
		EnrollmentID: enrollA, SessionID: f.session.ID, Status: "present",
	}); err != nil {
		t.Fatalf("first write error: %v", err)
	}
	if _, err := f.usecase.CreateBySession(context.Background(), f.tenant, f.tutor, &domain.CreateAttendanceBySessionRequest{
		EnrollmentID: enrollA, SessionID: f.session.ID, Status: "late",
	}); !errors.Is(err, domain.ErrAttendanceDuplicate) {
		t.Fatalf("duplicate err=%v want ErrAttendanceDuplicate", err)
	}
}
