package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrSessionForbidden = errors.New("class session access forbidden")

type ClassSession struct {
	ID           uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ClassID      uuid.UUID      `gorm:"type:uuid;not null;index:idx_class_session_date" json:"class_id"`
	ScheduleID   *uuid.UUID     `gorm:"type:uuid" json:"schedule_id,omitempty"`
	EnrollmentID *uuid.UUID     `gorm:"type:uuid" json:"enrollment_id,omitempty"`
	TutorID      uuid.UUID      `gorm:"type:uuid;not null" json:"tutor_id"`
	SessionDate  time.Time      `gorm:"type:date;not null;index:idx_class_session_date" json:"session_date"`
	StartTime    string         `gorm:"type:time;not null" json:"start_time"`
	EndTime      string         `gorm:"type:time;not null" json:"end_time"`
	Status       string         `gorm:"type:varchar(50);not null;default:'scheduled'" json:"status"` // 'scheduled', 'rescheduled', 'cancelled', 'completed'
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	Class      *Class         `gorm:"foreignKey:ClassID" json:"class,omitempty"`
	Schedule   *ClassSchedule `gorm:"foreignKey:ScheduleID" json:"schedule,omitempty"`
	Enrollment *Enrollment    `gorm:"foreignKey:EnrollmentID" json:"enrollment,omitempty"`
}

type SessionQuery struct {
	Page         int
	PageSize     int
	ClassID      *uuid.UUID
	ScheduleID   *uuid.UUID
	EnrollmentID *uuid.UUID
	TutorID      *uuid.UUID
	Status       string
	DateFrom     *time.Time
	DateTo       *time.Time
	Search       string
}

type SessionListResponse struct {
	Items      []ClassSession `json:"items"`
	Pagination Pagination     `json:"pagination"`
}
