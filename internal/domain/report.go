package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrReportForbidden = errors.New("report access forbidden")

type Report struct {
	ID              uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TenantID        uuid.UUID      `gorm:"type:uuid;not null;index" json:"tenant_id"`     // Cross-service, ordinary UUID
	EnrollmentID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"enrollment_id"` // Relation to Enrollment
	ReporterID      uuid.UUID      `gorm:"type:uuid;not null;index" json:"reporter_id"`   // Cross-service, ordinary UUID
	Title           string         `gorm:"type:varchar(255);not null" json:"title"`
	EvaluationNotes *string        `gorm:"type:text" json:"evaluation_notes,omitempty"`
	Score           *float64       `gorm:"type:numeric(5,2)" json:"score,omitempty"` // Decimal using float64
	CreatedAt       time.Time      `gorm:"type:timestamp;not null;default:now()" json:"created_at"`
	UpdatedAt       time.Time      `gorm:"type:timestamp;not null;default:now()" json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	Enrollment *Enrollment `gorm:"foreignKey:EnrollmentID" json:"enrollment,omitempty"`
}

type ReportQuery struct {
	Page         int
	PageSize     int
	EnrollmentID *uuid.UUID
	StudentID    *uuid.UUID
	ReporterID   *uuid.UUID
	DateFrom     *time.Time
	DateTo       *time.Time
	Search       string
}

type ReportListResponse struct {
	Items      []Report   `json:"items"`
	Pagination Pagination `json:"pagination"`
}

type CreateReportRequest struct {
	EnrollmentID    uuid.UUID `json:"enrollment_id" binding:"required"`
	Title           string    `json:"title" binding:"required"`
	EvaluationNotes *string   `json:"evaluation_notes"`
	Score           *float64  `json:"score" binding:"omitempty,gte=0,lte=100"`
}

type UpdateReportRequest struct {
	Title           string   `json:"title" binding:"required"`
	EvaluationNotes *string  `json:"evaluation_notes"`
	Score           *float64 `json:"score" binding:"omitempty,gte=0,lte=100"`
}
