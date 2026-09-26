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
	IsTutorForSession(ctx context.Context, tenantID, sessionID, memberID uuid.UUID) (bool, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID, query domain.SessionQuery) ([]domain.ClassSession, int64, error)
	Update(ctx context.Context, session *domain.ClassSession) error
	CancelFutureSessionsBySchedule(ctx context.Context, tenantID, scheduleID uuid.UUID, fromDate time.Time) error
	CancelFutureSessionsByClass(ctx context.Context, tenantID, classID uuid.UUID, fromDate time.Time) error
	UpdateFutureSessionsTutor(ctx context.Context, tenantID, scheduleID uuid.UUID, newTutorID uuid.UUID, newScheduleID uuid.UUID, fromDate time.Time) error
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
	TransferSchedule(ctx context.Context, tenantID, classID, oldScheduleID, newScheduleID uuid.UUID) error
	AssignSchedule(ctx context.Context, enrollmentID, scheduleID uuid.UUID) error
	Update(ctx context.Context, enrollment *domain.Enrollment) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type EnrollmentLockingRepository interface {
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Enrollment, error)
	GetByIdempotencyKeyForTenant(ctx context.Context, tenantID uuid.UUID, key string) (*domain.Enrollment, error)
	GetByIdempotencyKeyAny(ctx context.Context, key string) (*domain.Enrollment, error)
}

type AttendanceRepository interface {
	Create(ctx context.Context, attendance *domain.Attendance) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Attendance, error)
	GetByIDForTenant(ctx context.Context, tenantID, id uuid.UUID) (*domain.Attendance, error)
	List(ctx context.Context, tenantID uuid.UUID, query domain.AttendanceQuery) ([]domain.Attendance, int64, error)
	GetByUnique(ctx context.Context, enrollmentID, sessionID uuid.UUID, date time.Time) (*domain.Attendance, error)
	Update(ctx context.Context, attendance *domain.Attendance) error
}

type ReportRepository interface {
	Create(ctx context.Context, report *domain.Report) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Report, error)
	GetByIDForTenant(ctx context.Context, tenantID, id uuid.UUID) (*domain.Report, error)
	List(ctx context.Context, tenantID uuid.UUID, query domain.ReportQuery) ([]domain.Report, int64, error)
	Update(ctx context.Context, report *domain.Report) error
	Delete(ctx context.Context, id uuid.UUID) error
}
