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
