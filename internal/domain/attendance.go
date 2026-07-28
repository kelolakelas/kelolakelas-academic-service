package domain

import (
	"time"

	"github.com/google/uuid"
)

type Attendance struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	EnrollmentID uuid.UUID `gorm:"type:uuid;not null;index:idx_enrollment_date" json:"enrollment_id"`
	ScheduleID   uuid.UUID `gorm:"type:uuid;not null" json:"schedule_id"`
	Date         time.Time `gorm:"type:date;not null;index:idx_enrollment_date" json:"date"`
	Status       string    `gorm:"type:varchar(50);not null" json:"status"` // 'present', 'absent', 'late', 'excused'
	CreatedAt    time.Time `gorm:"type:timestamp;not null;default:now()" json:"created_at"`
	UpdatedAt    time.Time `gorm:"type:timestamp;not null;default:now()" json:"updated_at"`

	Enrollment    *Enrollment    `gorm:"foreignKey:EnrollmentID" json:"enrollment,omitempty"`
	ClassSchedule *ClassSchedule `gorm:"foreignKey:ScheduleID" json:"class_schedule,omitempty"`
}
