package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type CategoryRepository interface {
	Create(ctx context.Context, category *domain.Category) error
	ListByTenant(ctx context.Context, tenantID uuid.UUID, query domain.ListQuery) ([]domain.Category, int64, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Category, error)
	Update(ctx context.Context, category *domain.Category) error
	CountActiveClasses(ctx context.Context, categoryID uuid.UUID) (int64, error)
	DeleteByTenant(ctx context.Context, tenantID, id uuid.UUID) error
}

type ClassRepository interface {
	Create(ctx context.Context, class *domain.Class) error
	ListByTenant(ctx context.Context, tenantID uuid.UUID, query domain.ListQuery) ([]domain.Class, int64, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Class, error)
	UpdatePublicationStatus(ctx context.Context, classID, tenantID uuid.UUID, isPublished bool) (*domain.Class, error)
	UpdateByTenant(ctx context.Context, class *domain.Class) error
	Update(ctx context.Context, class *domain.Class) error
	DeleteByTenant(ctx context.Context, tenantID, id uuid.UUID) error
}

type ClassTeacherRepository interface {
	Assign(ctx context.Context, ct *domain.ClassTeacher) error
	Unassign(ctx context.Context, classID, teacherID uuid.UUID) error
}

type ClassScheduleRepository interface {
	Create(ctx context.Context, schedule *domain.ClassSchedule) error
	ListByTenant(ctx context.Context, tenantID uuid.UUID, query domain.ListQuery) ([]domain.ClassSchedule, int64, error)
	BatchCreate(ctx context.Context, schedules []*domain.ClassSchedule) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.ClassSchedule, error)
	GetByIDForTenant(ctx context.Context, tenantID, id uuid.UUID) (*domain.ClassSchedule, error)
	GetByIDForTenantForUpdate(ctx context.Context, tenantID, id uuid.UUID) (*domain.ClassSchedule, error)
	Update(ctx context.Context, schedule *domain.ClassSchedule) error
	DeleteByTenant(ctx context.Context, tenantID, id uuid.UUID) error
	DeleteByClass(ctx context.Context, tenantID, classID uuid.UUID) error
}

type ScheduleRepository = ClassScheduleRepository

type SessionRepository interface {
	Create(ctx context.Context, session *domain.ClassSession) error
	BatchCreate(ctx context.Context, sessions []*domain.ClassSession) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.ClassSession, error)
	GetByIDForTenant(ctx context.Context, tenantID, id uuid.UUID) (*domain.ClassSession, error)
	DeleteByTenant(ctx context.Context, tenantID, id uuid.UUID) error
	FindForAttendance(ctx context.Context, tenantID, scheduleID, enrollmentID uuid.UUID, date time.Time) (*domain.ClassSession, error)
	// FindSessionForAttendance resolves a session directly by id (KEL-134),
	// including reschedule replacements whose schedule is nil. The session is
	// returned only when it belongs to the calling tenant, and only together
	// with the enrollment cohort it actually covers (see
	// resolveSessionCohortScheduleID).
	FindSessionForAttendance(ctx context.Context, tenantID, sessionID uuid.UUID) (*domain.ClassSession, error)
	IsTutorForSession(ctx context.Context, tenantID, sessionID, memberID uuid.UUID) (bool, error)
	ListSessionsForAttendanceCohort(ctx context.Context, tenantID, classID uuid.UUID, status string) ([]domain.ClassSession, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID, query domain.SessionQuery) ([]domain.ClassSession, int64, error)
	// ListSessionsForParent lists sessions across every tenant that belong to
	// the parent's children (KEL-140): a session is included when it is a
	// private session of one of the parent's enrollments, a group session of a
	// schedule holding a live (pending/active) enrollment of the parent, or a
	// reschedule replacement linked to such an origin session. The tenant claim
	// is never consulted, so a parent-scoped list cannot leak another tenant.
	ListSessionsForParent(ctx context.Context, parentID uuid.UUID, query domain.SessionQuery) ([]domain.ClassSession, int64, error)
	// GetSessionForParent resolves one session for a parent (KEL-140): the
	// session is returned only when it satisfies the same ownership predicate
	// as ListSessionsForParent, otherwise gorm.ErrRecordNotFound.
	GetSessionForParent(ctx context.Context, parentID, id uuid.UUID) (*domain.ClassSession, error)
	Update(ctx context.Context, session *domain.ClassSession) error
	CancelFutureSessionsBySchedule(ctx context.Context, tenantID, scheduleID uuid.UUID, fromDate time.Time) error
	CancelFutureSessionsByClass(ctx context.Context, tenantID, classID uuid.UUID, fromDate time.Time) error
	UpdateFutureSessionsTutor(ctx context.Context, tenantID, scheduleID uuid.UUID, newTutorID uuid.UUID, newScheduleID uuid.UUID, fromDate time.Time) error
}

// SessionGenerationRepository backs the background worker that keeps every live
// schedule's sessions generated through the rolling horizon (KEL-90). It spans all
// tenants by design: it is never reachable from a request.
type SessionGenerationRepository interface {
	LockNextScheduleDueForGeneration(ctx context.Context, horizonEnd time.Time, afterID uuid.UUID) (*domain.ClassSchedule, error)
	InsertMissingSessions(ctx context.Context, sessions []*domain.ClassSession) (int64, error)
	MarkSessionsGeneratedUntil(ctx context.Context, scheduleID uuid.UUID, until time.Time) error
}

type TransactionManager interface {
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type StudentRepository interface {
	Create(ctx context.Context, student *domain.Student) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Student, error)
	GetByIDForAccess(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID) (*domain.Student, error)
	List(ctx context.Context, tenantID, parentID *uuid.UUID, query domain.StudentQuery) ([]domain.Student, int64, error)
	CountActiveEnrollments(ctx context.Context, studentID uuid.UUID) (int64, error)
	Update(ctx context.Context, student *domain.Student) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type StudentNoteRepository interface {
	Create(ctx context.Context, note *domain.StudentNote) error
	GetByIDForAccess(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID) (*domain.StudentNote, error)
	UpdateForAccess(ctx context.Context, note *domain.StudentNote, tenantID, parentID *uuid.UUID) error
	DeleteForAccess(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID) error
}

type EnrollmentRepository interface {
	Create(ctx context.Context, enrollment *domain.Enrollment) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Enrollment, error)
	GetByIDForAccess(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.Enrollment, error)
	List(ctx context.Context, tenantID, parentID *uuid.UUID, query domain.EnrollmentQuery) ([]*domain.Enrollment, int64, error)
	ExistsActive(ctx context.Context, studentID, classID uuid.UUID) (bool, error)
	GetByIdempotencyKey(ctx context.Context, parentID uuid.UUID, key string) (*domain.Enrollment, error)
	CreateIfCapacityAvailable(ctx context.Context, enrollment *domain.Enrollment) error
	IsTutorForEnrollment(ctx context.Context, enrollmentID, memberID uuid.UUID) (bool, error)
	GetActiveByClassID(ctx context.Context, classID uuid.UUID) ([]*domain.Enrollment, error)
	GetActiveByScheduleID(ctx context.Context, tenantID, scheduleID uuid.UUID) ([]*domain.Enrollment, error)
	// GetActiveByScheduleIDForParent returns the live (pending/active)
	// enrollments on a schedule that belong to the parent's children
	// (KEL-140), so a parent-scoped attendee list never exposes other
	// students. The tenant claim is never consulted.
	GetActiveByScheduleIDForParent(ctx context.Context, parentID, scheduleID uuid.UUID) ([]*domain.Enrollment, error)
	TransferSchedule(ctx context.Context, tenantID, classID, oldScheduleID, newScheduleID uuid.UUID) error
	AssignSchedule(ctx context.Context, enrollmentID, scheduleID uuid.UUID) error
	Update(ctx context.Context, enrollment *domain.Enrollment) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// EnrollmentPaymentRepository writes invoice fields without overwriting a concurrent
// webhook transition of enrollment status.
type EnrollmentPaymentRepository interface {
	UpdatePaymentDetails(ctx context.Context, id, transactionID uuid.UUID, checkoutURL string) error
	RestoreRejectedEnrollment(ctx context.Context, id uuid.UUID) error
}

type EnrollmentLockingRepository interface {
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Enrollment, error)
	GetByIdempotencyKeyForTenant(ctx context.Context, tenantID uuid.UUID, key string) (*domain.Enrollment, error)
	GetByIdempotencyKeyAny(ctx context.Context, key string) (*domain.Enrollment, error)
	// ResumeUnderCapacity reclaims the seat of a suspended enrollment. The caller
	// holds the enrollment row lock in the same transaction; it fails with
	// ErrScheduleFull, ErrDuplicateEnrollment or ErrEnrollmentSuspendedConflict
	// without writing anything, so the enrollment stays suspended.
	ResumeUnderCapacity(ctx context.Context, enrollment *domain.Enrollment) error
}

type AttendanceRepository interface {
	Create(ctx context.Context, attendance *domain.Attendance) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Attendance, error)
	GetByIDForTenant(ctx context.Context, tenantID, id uuid.UUID) (*domain.Attendance, error)
	List(ctx context.Context, tenantID uuid.UUID, query domain.AttendanceQuery) ([]domain.Attendance, int64, error)
	// ListForParent lists attendance rows across every tenant that belong to
	// the parent's children (KEL-140): a row is included only when its
	// enrollment is held by a student whose parent_id is the caller. The
	// tenant claim is never consulted. Client-supplied enrollment, student, or
	// schedule filters narrow the parent's own rows and can never widen them
	// to another parent's children.
	ListForParent(ctx context.Context, parentID uuid.UUID, query domain.AttendanceQuery) ([]domain.Attendance, int64, error)
	// GetForParent resolves one attendance row for a parent (KEL-140), or
	// gorm.ErrRecordNotFound when the row does not belong to the parent.
	GetForParent(ctx context.Context, parentID, id uuid.UUID) (*domain.Attendance, error)
	GetByUnique(ctx context.Context, enrollmentID, sessionID uuid.UUID, date time.Time) (*domain.Attendance, error)
	// GetBySessionEnrollment resolves a single (session, enrollment) row
	// without the legacy date component (KEL-134).
	GetBySessionEnrollment(ctx context.Context, sessionID, enrollmentID uuid.UUID) (*domain.Attendance, error)
	// UpsertBulk writes every row of one session atomically and idempotently
	// (KEL-134): existing (session_id, enrollment_id) rows are updated, new
	// ones are inserted, all scoped to the session's tenant in SQL. Rows keep
	// the session's own date.
	UpsertBulk(ctx context.Context, tenantID, sessionID uuid.UUID, items []domain.BulkAttendanceItem) ([]domain.Attendance, error)
	Update(ctx context.Context, attendance *domain.Attendance) error
}

type ReportRepository interface {
	Create(ctx context.Context, report *domain.Report) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Report, error)
	GetByIDForTenant(ctx context.Context, tenantID, id uuid.UUID) (*domain.Report, error)
	List(ctx context.Context, tenantID uuid.UUID, query domain.ReportQuery) ([]domain.Report, int64, error)
	// ListForParent lists reports across every tenant that belong to the
	// parent's children (KEL-140): a report is included only when its
	// enrollment is held by a student whose parent_id is the caller. The
	// tenant claim is never consulted. Client-supplied enrollment, student, or
	// reporter filters narrow the parent's own rows and can never widen them
	// to another parent's children.
	ListForParent(ctx context.Context, parentID uuid.UUID, query domain.ReportQuery) ([]domain.Report, int64, error)
	// GetForParent resolves one report for a parent (KEL-140), or
	// gorm.ErrRecordNotFound when the report does not belong to the parent.
	GetForParent(ctx context.Context, parentID, id uuid.UUID) (*domain.Report, error)
	Update(ctx context.Context, report *domain.Report) error
	Delete(ctx context.Context, id uuid.UUID) error
}
