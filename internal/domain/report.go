package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

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
