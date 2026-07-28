package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/tutorin-id/tutorin-academic-service/internal/domain"
)

type AcademicUsecase interface {
	// Category operations
	CreateCategory(ctx context.Context, category *domain.Category) error
	GetCategoryByID(ctx context.Context, id uuid.UUID) (*domain.Category, error)

	// Class operations
	CreateClass(ctx context.Context, class *domain.Class) error
	GetClassByID(ctx context.Context, id uuid.UUID) (*domain.Class, error)

	// Student operations
	CreateStudent(ctx context.Context, student *domain.Student) error
	GetStudentByID(ctx context.Context, id uuid.UUID) (*domain.Student, error)

	// Enrollment operations
	EnrollStudent(ctx context.Context, enrollment *domain.Enrollment) error
	GetEnrollmentByID(ctx context.Context, id uuid.UUID) (*domain.Enrollment, error)

	// Attendance operations
	RecordAttendance(ctx context.Context, attendance *domain.Attendance) error

	// Report operations
	CreateReport(ctx context.Context, report *domain.Report) error
	GetReportByID(ctx context.Context, id uuid.UUID) (*domain.Report, error)
}

type ScheduleUsecase interface {
	// Scenario 1: Create Initial Schedules for an Existing Class
	CreateInitialSchedules(ctx context.Context, req *domain.CreateInitialSchedulesRequest) (*domain.CreateInitialSchedulesResponse, error)

	// Scenario 2: Temporary Schedule Change (One-off Reschedule / Make-up Class)
	RescheduleSession(ctx context.Context, req *domain.RescheduleSessionRequest) (*domain.RescheduleSessionResponse, error)

	// Scenario 3: Permanent Schedule Change
	ChangeSchedulePermanent(ctx context.Context, req *domain.PermanentScheduleChangeRequest) (*domain.PermanentScheduleChangeResponse, error)

	// Scenario 4: Temporary Tutor Change (Substitute Teacher)
	ChangeTutorTemporary(ctx context.Context, req *domain.SubstituteTutorRequest) (*domain.SubstituteTutorResponse, error)

	// Scenario 5: Permanent Tutor Change
	ChangeTutorPermanent(ctx context.Context, req *domain.PermanentTutorChangeRequest) (*domain.PermanentTutorChangeResponse, error)

	// Attendance/Session Read Logic
	GetSessionAttendees(ctx context.Context, sessionID uuid.UUID) ([]*domain.Enrollment, error)
}
