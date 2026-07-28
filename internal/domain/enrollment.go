package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Enrollment struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID      `gorm:"type:uuid;not null;index:idx_tenant_status" json:"tenant_id"` // Cross-service, ordinary UUID
	StudentID uuid.UUID      `gorm:"type:uuid;not null;index:idx_student_class,unique" json:"student_id"`
	ClassID   uuid.UUID      `gorm:"type:uuid;not null;index:idx_student_class,unique" json:"class_id"`
	Status    string         `gorm:"type:varchar(50);not null;index:idx_tenant_status" json:"status"` // 'pending', 'active', 'completed', 'dropped'
	JoinedAt  time.Time      `gorm:"type:timestamp;not null;default:now()" json:"joined_at"`
	UpdatedAt time.Time      `gorm:"type:timestamp;not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	Student *Student `gorm:"foreignKey:StudentID" json:"student,omitempty"`
	Class   *Class   `gorm:"foreignKey:ClassID" json:"class,omitempty"`
}
