package usecase

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

// ErrAttendanceSessionNotFound reports a session id that does not exist for
// the calling tenant (KEL-134). It keeps the cross-tenant "missing" answer so
// no session of another tenant leaks through session-addressed writes.
var ErrAttendanceSessionNotFound = errors.New("class session not found")

// ErrAttendanceSessionCancelled rejects attendance writes to a cancelled
// session (KEL-134): the session exists, so it must not be reported as
// missing, but it cannot take attendance.
var ErrAttendanceSessionCancelled = errors.New("class session is cancelled")

// ErrAttendanceEnrollmentMismatch rejects an enrollment that is not part of
// the session's cohort (KEL-134 AC3): answered as 400 (validation), never as
// a silent skip, so the caller knows which enrollment was refused.
var ErrAttendanceEnrollmentMismatch = errors.New("enrollment does not belong to this session")

type AttendanceUsecase interface {
	List(ctx context.Context, tenantID uuid.UUID, query domain.AttendanceQuery) (*domain.AttendanceListResponse, error)
	// ListForParent lists attendance rows across every tenant that belong to
	// the parent's children (KEL-140). The tenant claim is never consulted.
	ListForParent(ctx context.Context, parentID uuid.UUID, query domain.AttendanceQuery) (*domain.AttendanceListResponse, error)
	Create(ctx context.Context, tenantID, memberID uuid.UUID, req *domain.CreateAttendanceRequest) (*domain.Attendance, error)
	// CreateBySession records one attendance row addressed directly at a
	// session id, including reschedule replacements (KEL-134 AC1).
	CreateBySession(ctx context.Context, tenantID, memberID uuid.UUID, req *domain.CreateAttendanceBySessionRequest) (*domain.Attendance, error)
	// CreateBulk records a whole session's students in one idempotent,
	// atomic request (KEL-134 AC2).
	CreateBulk(ctx context.Context, tenantID, memberID uuid.UUID, req *domain.BulkAttendanceRequest) (*domain.BulkAttendanceResponse, error)
	Get(ctx context.Context, tenantID, id uuid.UUID) (*domain.Attendance, error)
	GetBySession(ctx context.Context, tenantID, sessionID, enrollmentID uuid.UUID) (*domain.Attendance, error)
	// GetForParent resolves one attendance row for a parent (KEL-140), or
	// gorm.ErrRecordNotFound when the row does not belong to the parent.
	GetForParent(ctx context.Context, parentID, id uuid.UUID) (*domain.Attendance, error)
	// GetBySessionForParent reads one attendance row addressed at a session
	// id for a parent (KEL-140): the session must satisfy the parent session
	// ownership predicate and the row's enrollment must belong to the
	// parent, otherwise not-found (never forbidden, so ids do not leak).
	GetBySessionForParent(ctx context.Context, parentID, sessionID, enrollmentID uuid.UUID) (*domain.Attendance, error)
	Update(ctx context.Context, tenantID, memberID, id uuid.UUID, req *domain.UpdateAttendanceRequest) (*domain.Attendance, error)
}

type attendanceUsecase struct {
	repo        repository.AttendanceRepository
	sessions    repository.SessionRepository
	enrollments repository.EnrollmentRepository
}

// NewAttendanceUsecase keeps its historic two-argument form so existing call
// sites compile; pass an enrollment repository to enable session-addressed
// writes and bulk upserts.
func NewAttendanceUsecase(repo repository.AttendanceRepository, sessions repository.SessionRepository, enrollments ...repository.EnrollmentRepository) AttendanceUsecase {
	var er repository.EnrollmentRepository
	if len(enrollments) > 0 {
		er = enrollments[0]
	}
	return &attendanceUsecase{repo: repo, sessions: sessions, enrollments: er}
}

func (u *attendanceUsecase) List(ctx context.Context, tenantID uuid.UUID, query domain.AttendanceQuery) (*domain.AttendanceListResponse, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 || query.PageSize > 100 {
		query.PageSize = 20
	}
	items, total, err := u.repo.List(ctx, tenantID, query)
	if err != nil {
		return nil, err
	}
	return &domain.AttendanceListResponse{Items: items, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}}, nil
}

// Create keeps the legacy schedule_id + date addressing working (KEL-134
// AC4) and additionally accepts the session_id form, which is the only form
// that addresses reschedule replacements. Exactly one form is accepted.
func (u *attendanceUsecase) Create(ctx context.Context, tenantID, memberID uuid.UUID, req *domain.CreateAttendanceRequest) (*domain.Attendance, error) {
	if req.SessionID != nil {
		if req.ScheduleID != nil || req.Date != "" {
			return nil, errors.New("session_id cannot be combined with schedule_id/date")
		}
		return u.createForSession(ctx, tenantID, memberID, req.EnrollmentID, *req.SessionID, req.Status)
	}
	if req.ScheduleID == nil || req.Date == "" {
		return nil, errors.New("either session_id or schedule_id with date is required")
	}
	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		return nil, err
	}
	session, err := u.sessions.FindForAttendance(ctx, tenantID, *req.ScheduleID, req.EnrollmentID, date)
	if err != nil {
		return nil, err
	}
	assigned, err := u.sessions.IsTutorForSession(ctx, tenantID, session.ID, memberID)
	if err != nil {
		return nil, err
	}
	if !assigned {
		return nil, domain.ErrAttendanceForbidden
	}
	existing, err := u.repo.GetByUnique(ctx, req.EnrollmentID, session.ID, date)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, domain.ErrAttendanceDuplicate
	}
	item := &domain.Attendance{ID: uuid.New(), EnrollmentID: req.EnrollmentID, SessionID: session.ID, Date: date, Status: req.Status}
	if err := u.repo.Create(ctx, item); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, domain.ErrAttendanceDuplicate
		}
		return nil, err
	}
	return item, nil
}

func (u *attendanceUsecase) CreateBySession(ctx context.Context, tenantID, memberID uuid.UUID, req *domain.CreateAttendanceBySessionRequest) (*domain.Attendance, error) {
	return u.createForSession(ctx, tenantID, memberID, req.EnrollmentID, req.SessionID, req.Status)
}

// CreateBulk records a whole session's students in one idempotent, atomic
// request (KEL-134 AC2). Every enrollment in the request must belong to the
// session's cohort (AC3) and the caller must be the session's tutor; the
// repository upsert repeats the same statement safely and refuses to touch
// another tenant's session at the SQL level. Duplicate enrollment ids inside
// one request keep the last status so the write has one row per enrollment.
func (u *attendanceUsecase) CreateBulk(ctx context.Context, tenantID, memberID uuid.UUID, req *domain.BulkAttendanceRequest) (*domain.BulkAttendanceResponse, error) {
	if u.enrollments == nil {
		return nil, errors.New("attendance bulk writes are unavailable")
	}
	session, cohortScheduleID, err := u.resolveSessionCohort(ctx, tenantID, req.SessionID)
	if err != nil {
		return nil, err
	}
	assigned, err := u.sessions.IsTutorForSession(ctx, tenantID, session.ID, memberID)
	if err != nil {
		return nil, err
	}
	if !assigned {
		return nil, domain.ErrAttendanceForbidden
	}
	deduped := dedupeBulkItems(req.Items)
	if err := u.checkCohort(ctx, tenantID, session, cohortScheduleID, enrollmentIDs(deduped)); err != nil {
		return nil, err
	}
	rows, err := u.repo.UpsertBulk(ctx, tenantID, session.ID, deduped)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttendanceSessionNotFound
		}
		return nil, err
	}
	return &domain.BulkAttendanceResponse{SessionID: session.ID, Attendances: rows}, nil
}

func (u *attendanceUsecase) Get(ctx context.Context, tenantID, id uuid.UUID) (*domain.Attendance, error) {
	return u.repo.GetByIDForTenant(ctx, tenantID, id)
}

// ListForParent lists attendance rows across every tenant that belong to the
// parent's children (KEL-140). Pagination defaults match List; a foreign
// enrollment, student, or schedule filter only narrows the parent's own rows.
func (u *attendanceUsecase) ListForParent(ctx context.Context, parentID uuid.UUID, query domain.AttendanceQuery) (*domain.AttendanceListResponse, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 || query.PageSize > 100 {
		query.PageSize = 20
	}
	items, total, err := u.repo.ListForParent(ctx, parentID, query)
	if err != nil {
		return nil, err
	}
	return &domain.AttendanceListResponse{Items: items, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}}, nil
}

// GetForParent resolves one attendance row for a parent (KEL-140). A row of
// another parent's child answers gorm.ErrRecordNotFound (404 upstream), never
// forbidden, so ids do not leak.
func (u *attendanceUsecase) GetForParent(ctx context.Context, parentID, id uuid.UUID) (*domain.Attendance, error) {
	return u.repo.GetForParent(ctx, parentID, id)
}

// GetBySessionForParent reads one attendance row addressed at a session id
// for a parent (KEL-140). The session must satisfy the parent session
// ownership predicate and the row's enrollment must belong to the parent;
// otherwise the call answers not-found, never forbidden, so ids do not leak.
func (u *attendanceUsecase) GetBySessionForParent(ctx context.Context, parentID, sessionID, enrollmentID uuid.UUID) (*domain.Attendance, error) {
	if u.enrollments == nil {
		return nil, errors.New("attendance parent reads are unavailable")
	}
	session, err := u.sessions.GetSessionForParent(ctx, parentID, sessionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttendanceSessionNotFound
		}
		return nil, err
	}
	_ = session
	if _, err := u.enrollments.GetByIDForAccess(ctx, nil, &parentID, enrollmentID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrAttendanceNotFound
		}
		return nil, err
	}
	item, err := u.repo.GetBySessionEnrollment(ctx, sessionID, enrollmentID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, domain.ErrAttendanceNotFound
	}
	return item, nil
}

// GetBySession reads one attendance row addressed at a session id (KEL-134
// AC1: absensi sesi reschedule dapat dibaca). Tenant-scoped through the
// session: another tenant's session reports not-found.
func (u *attendanceUsecase) GetBySession(ctx context.Context, tenantID, sessionID, enrollmentID uuid.UUID) (*domain.Attendance, error) {
	session, err := u.sessions.FindSessionForAttendance(ctx, tenantID, sessionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttendanceSessionNotFound
		}
		return nil, err
	}
	_ = session
	item, err := u.repo.GetBySessionEnrollment(ctx, sessionID, enrollmentID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, domain.ErrAttendanceNotFound
	}
	return item, nil
}

func (u *attendanceUsecase) Update(ctx context.Context, tenantID, memberID, id uuid.UUID, req *domain.UpdateAttendanceRequest) (*domain.Attendance, error) {
	item, err := u.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	assigned, err := u.sessions.IsTutorForSession(ctx, tenantID, item.SessionID, memberID)
	if err != nil {
		return nil, err
	}
	if !assigned {
		return nil, domain.ErrAttendanceForbidden
	}
	item.Status = req.Status
	item.UpdatedAt = time.Now()
	if err := u.repo.Update(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// createForSession is the shared write path for session-addressed single
// writes (direct and via the session_id form of Create). It enforces, in
// order: session exists for this tenant, session not cancelled, caller is
// the session's tutor, enrollment belongs to the session's cohort, no
// duplicate row. Update stays id-addressed and already passes through
// IsTutorForSession, so reschedule rows update the same way.
func (u *attendanceUsecase) createForSession(ctx context.Context, tenantID, memberID, enrollmentID, sessionID uuid.UUID, status string) (*domain.Attendance, error) {
	if u.enrollments == nil {
		return nil, errors.New("attendance session writes are unavailable")
	}
	session, cohortScheduleID, err := u.resolveSessionCohort(ctx, tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	assigned, err := u.sessions.IsTutorForSession(ctx, tenantID, session.ID, memberID)
	if err != nil {
		return nil, err
	}
	if !assigned {
		return nil, domain.ErrAttendanceForbidden
	}
	if err := u.checkCohort(ctx, tenantID, session, cohortScheduleID, []uuid.UUID{enrollmentID}); err != nil {
		return nil, err
	}
	existing, err := u.repo.GetBySessionEnrollment(ctx, session.ID, enrollmentID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, domain.ErrAttendanceDuplicate
	}
	item := &domain.Attendance{ID: uuid.New(), EnrollmentID: enrollmentID, SessionID: session.ID, Date: session.SessionDate, Status: status}
	if err := u.repo.Create(ctx, item); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, domain.ErrAttendanceDuplicate
		}
		return nil, err
	}
	return item, nil
}

// resolveSessionCohort resolves the session and the schedule id whose
// enrollments form its cohort. The cohort rules mirror GetSessionAttendees:
// a private session (non-nil enrollment id) covers exactly that enrollment;
// a replacement (nil schedule) covers its origin's schedule; a regular group
// session covers its own schedule. Cancelled sessions are rejected here so
// every session-addressed write shares the same verdict.
func (u *attendanceUsecase) resolveSessionCohort(ctx context.Context, tenantID, sessionID uuid.UUID) (*domain.ClassSession, *uuid.UUID, error) {
	session, err := u.sessions.FindSessionForAttendance(ctx, tenantID, sessionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrAttendanceSessionNotFound
		}
		return nil, nil, err
	}
	if session == nil {
		return nil, nil, ErrAttendanceSessionNotFound
	}
	if session.Status == "cancelled" {
		return nil, nil, ErrAttendanceSessionCancelled
	}
	if session.EnrollmentID != nil {
		return session, nil, nil
	}
	if session.ScheduleID != nil {
		return session, session.ScheduleID, nil
	}
	// Reschedule replacement: follow the origin link written at reschedule
	// time. Replacements created before the KEL-134 migration carry no link;
	// fall back to the origin session that still has status 'rescheduled' in
	// the same class — exactly one exists per reschedule operation.
	if session.RescheduledFromSessionID != nil {
		origin, err := u.sessions.FindSessionForAttendance(ctx, tenantID, *session.RescheduledFromSessionID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil, ErrAttendanceSessionNotFound
			}
			return nil, nil, err
		}
		if origin == nil || origin.ScheduleID == nil {
			return nil, nil, ErrAttendanceSessionNotFound
		}
		return session, origin.ScheduleID, nil
	}
	origins, err := u.sessions.ListSessionsForAttendanceCohort(ctx, tenantID, session.ClassID, "rescheduled")
	if err != nil {
		return nil, nil, err
	}
	if len(origins) != 1 || origins[0].ScheduleID == nil {
		return nil, nil, ErrAttendanceSessionNotFound
	}
	return session, origins[0].ScheduleID, nil
}

// checkCohort verifies every enrollment belongs to the session: the private
// enrollment itself, or a live (pending/active) enrollment of the cohort
// schedule for group sessions. Suspended enrollments are refused the same
// way the legacy path refuses them via FindForAttendance.
func (u *attendanceUsecase) checkCohort(ctx context.Context, tenantID uuid.UUID, session *domain.ClassSession, cohortScheduleID *uuid.UUID, enrollmentIDs []uuid.UUID) error {
	if session.EnrollmentID != nil {
		for _, id := range enrollmentIDs {
			if id != *session.EnrollmentID {
				return ErrAttendanceEnrollmentMismatch
			}
		}
		enrollment, err := u.enrollments.GetByIDForAccess(ctx, &tenantID, nil, *session.EnrollmentID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAttendanceEnrollmentMismatch
			}
			return err
		}
		if enrollment == nil || enrollment.Status == domain.EnrollmentStatusSuspended {
			return ErrAttendanceEnrollmentMismatch
		}
		return nil
	}
	if cohortScheduleID == nil {
		return ErrAttendanceSessionNotFound
	}
	cohort, err := u.enrollments.GetActiveByScheduleID(ctx, tenantID, *cohortScheduleID)
	if err != nil {
		return err
	}
	members := make(map[uuid.UUID]bool, len(cohort))
	for _, e := range cohort {
		members[e.ID] = true
	}
	for _, id := range enrollmentIDs {
		if !members[id] {
			return ErrAttendanceEnrollmentMismatch
		}
	}
	return nil
}

func dedupeBulkItems(items []domain.BulkAttendanceItem) []domain.BulkAttendanceItem {
	seen := make(map[uuid.UUID]int, len(items))
	out := make([]domain.BulkAttendanceItem, 0, len(items))
	for _, item := range items {
		if i, ok := seen[item.EnrollmentID]; ok {
			out[i] = item
			continue
		}
		seen[item.EnrollmentID] = len(out)
		out = append(out, item)
	}
	return out
}

func enrollmentIDs(items []domain.BulkAttendanceItem) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.EnrollmentID)
	}
	return ids
}
