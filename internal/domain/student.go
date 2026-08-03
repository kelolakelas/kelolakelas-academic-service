package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrStudentNotFound     = errors.New("student not found")
	ErrStudentForbidden    = errors.New("student access forbidden")
	ErrStudentActiveEnroll = errors.New("student has active enrollments")
)

type Student struct {
	ID          uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ParentID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"parent_id"` // Cross-service, ordinary UUID
	FullName    string         `gorm:"type:varchar(255);not null" json:"full_name"`
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

type CreateStudentRequest struct {
	ParentID    uuid.UUID `json:"parent_id" binding:"required"`
	FullName    string    `json:"full_name" binding:"required"`
	DateOfBirth string    `json:"date_of_birth" binding:"required,datetime=2006-01-02"`
}

type UpdateStudentRequest struct {
	FullName    string `json:"full_name" binding:"required"`
	DateOfBirth string `json:"date_of_birth" binding:"required,datetime=2006-01-02"`
}
