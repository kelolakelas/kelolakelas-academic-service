package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/tutorin-id/tutorin-academic-service/internal/domain"
)

type CategoryRepository interface {
	Create(ctx context.Context, category *domain.Category) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Category, error)
	Update(ctx context.Context, category *domain.Category) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type ClassRepository interface {
	Create(ctx context.Context, class *domain.Class) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Class, error)
	Update(ctx context.Context, class *domain.Class) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type ClassTeacherRepository interface {
	Assign(ctx context.Context, ct *domain.ClassTeacher) error
	Unassign(ctx context.Context, classID, teacherID uuid.UUID) error
}

type ClassScheduleRepository interface {
	Create(ctx context.Context, schedule *domain.ClassSchedule) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.ClassSchedule, error)
	Update(ctx context.Context, schedule *domain.ClassSchedule) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type StudentRepository interface {
	Create(ctx context.Context, student *domain.Student) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Student, error)
	Update(ctx context.Context, student *domain.Student) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type StudentNoteRepository interface {
	Create(ctx context.Context, note *domain.StudentNote) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.StudentNote, error)
	Update(ctx context.Context, note *domain.StudentNote) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type EnrollmentRepository interface {
	Create(ctx context.Context, enrollment *domain.Enrollment) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Enrollment, error)
	Update(ctx context.Context, enrollment *domain.Enrollment) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type AttendanceRepository interface {
	Create(ctx context.Context, attendance *domain.Attendance) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Attendance, error)
	Update(ctx context.Context, attendance *domain.Attendance) error
}

type ReportRepository interface {
	Create(ctx context.Context, report *domain.Report) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Report, error)
	Update(ctx context.Context, report *domain.Report) error
	Delete(ctx context.Context, id uuid.UUID) error
}
