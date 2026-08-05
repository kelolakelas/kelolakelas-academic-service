package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrStudentNotFound            = errors.New("student not found")
	ErrStudentForbidden           = errors.New("student access forbidden")
	ErrStudentActiveEnroll        = errors.New("student has active enrollments")
	ErrStudentFirstNameRequired   = errors.New("first name is required")
	ErrStudentNoteInvalid         = errors.New("invalid student note")
	ErrStudentNoteTenantRequired  = errors.New("tenant context is required for student note")
	ErrStudentNoteContentRequired = errors.New("student note content is required")
)

type Student struct {
	ID          uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ParentID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"parent_id"` // Cross-service, ordinary UUID
	FirstName   string         `gorm:"type:varchar(255);not null" json:"first_name"`
	LastName    *string        `gorm:"type:varchar(255)" json:"last_name"`
	Nickname    *string        `gorm:"type:varchar(100)" json:"nickname"`
	Gender      *string        `gorm:"type:varchar(10)" json:"gender"`
	DateOfBirth *time.Time     `gorm:"type:date" json:"date_of_birth,omitempty"`
	CreatedAt   time.Time      `gorm:"type:timestamp;not null;default:now()" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"type:timestamp;not null;default:now()" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

type StudentQuery struct {
	Page     int
	PageSize int
	Search   string
}

type StudentListResponse struct {
	Items      []Student  `json:"items"`
	Pagination Pagination `json:"pagination"`
}

type StudentNoteRequest struct {
	NoteType string `json:"note_type" binding:"required,oneof=medical academic behavioral"`
	Content  string `json:"content" binding:"required,max=5000"`
}

type CreateStudentRequest struct {
	ParentID    uuid.UUID           `json:"parent_id" binding:"required"`
	FirstName   string              `json:"first_name" binding:"required,max=255"`
	LastName    *string             `json:"last_name,omitempty" binding:"omitempty,max=255"`
	Nickname    *string             `json:"nickname,omitempty" binding:"omitempty,max=100"`
	Gender      *string             `json:"gender,omitempty" binding:"omitempty,oneof=male female"`
	DateOfBirth string              `json:"date_of_birth" binding:"required,datetime=2006-01-02"`
	StudentNote *StudentNoteRequest `json:"student_note,omitempty"`
}

type UpdateStudentRequest struct {
	FirstName   string              `json:"first_name" binding:"required,max=255"`
	LastName    *string             `json:"last_name,omitempty" binding:"omitempty,max=255"`
	Nickname    *string             `json:"nickname,omitempty" binding:"omitempty,max=100"`
	Gender      *string             `json:"gender,omitempty" binding:"omitempty,oneof=male female"`
	DateOfBirth string              `json:"date_of_birth" binding:"required,datetime=2006-01-02"`
	StudentNote *StudentNoteRequest `json:"student_note,omitempty"`
}
