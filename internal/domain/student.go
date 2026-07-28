package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
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
