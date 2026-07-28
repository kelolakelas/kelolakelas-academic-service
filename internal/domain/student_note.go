package domain

import (
	"time"

	"github.com/google/uuid"
)

type StudentNote struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `gorm:"type:uuid;not null;index:idx_tenant_student" json:"tenant_id"`  // Cross-service, ordinary UUID
	StudentID uuid.UUID `gorm:"type:uuid;not null;index:idx_tenant_student" json:"student_id"` // Relation to Student
	AuthorID  uuid.UUID `gorm:"type:uuid;not null" json:"author_id"`                           // Cross-service, ordinary UUID
	NoteType  string    `gorm:"type:varchar(50);not null" json:"note_type"`                    // 'medical', 'academic', 'behavioral'
	Content   string    `gorm:"type:text;not null" json:"content"`
	CreatedAt time.Time `gorm:"type:timestamp;not null;default:now()" json:"created_at"`
	UpdatedAt time.Time `gorm:"type:timestamp;not null;default:now()" json:"updated_at"`

	Student *Student `gorm:"foreignKey:StudentID" json:"student,omitempty"`
}
