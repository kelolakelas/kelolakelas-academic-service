package domain

import (
	"time"

	"github.com/google/uuid"
)

type ClassTeacher struct {
	ClassID    uuid.UUID `gorm:"type:uuid;primaryKey;index:idx_class_teacher,unique" json:"class_id"`
	TeacherID  uuid.UUID `gorm:"type:uuid;primaryKey;index:idx_class_teacher,unique" json:"teacher_id"`
	AssignedAt time.Time `gorm:"type:timestamp;not null;default:now()" json:"assigned_at"`

	Class *Class `gorm:"foreignKey:ClassID" json:"class,omitempty"`
}
