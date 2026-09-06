package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrScheduleForbidden = errors.New("class schedule access forbidden")

type ClassSchedule struct {
	ID           uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ClassID      uuid.UUID      `gorm:"type:uuid;not null;index:idx_class_day" json:"class_id"`
	EnrollmentID *uuid.UUID     `gorm:"type:uuid" json:"enrollment_id,omitempty"`
	TutorID      *uuid.UUID     `gorm:"type:uuid" json:"tutor_id,omitempty"`
	Capacity     int            `gorm:"type:integer;not null" json:"capacity"`
	Location     *string        `gorm:"type:varchar(255)" json:"location,omitempty"`
	DayOfWeek    int            `gorm:"type:integer;not null;index:idx_class_day" json:"day_of_week"` // 1 = Senin, 7 = Minggu (ISO 8601)
	StartTime    string         `gorm:"type:time;not null" json:"start_time"`                         // standard HH:MM:SS for time column
	EndTime      string         `gorm:"type:time;not null" json:"end_time"`                           // standard HH:MM:SS for time column
	ValidFrom    *time.Time     `gorm:"type:date" json:"valid_from,omitempty"`
	ValidUntil   *time.Time     `gorm:"type:date" json:"valid_until,omitempty"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	Class      *Class      `gorm:"foreignKey:ClassID" json:"class,omitempty"`
	Enrollment *Enrollment `gorm:"foreignKey:EnrollmentID" json:"enrollment,omitempty"`
}
