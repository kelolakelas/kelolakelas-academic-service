package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidAttendanceStatus = errors.New("invalid attendance status")
	ErrAttendanceNotFound      = errors.New("attendance not found")
	ErrAttendanceDuplicate     = errors.New("attendance already exists")
	ErrAttendanceForbidden     = errors.New("tutor is not assigned to this session")
)

type Attendance struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	EnrollmentID uuid.UUID `gorm:"type:uuid;not null;index:idx_enrollment_date;uniqueIndex:idx_session_enrollment" json:"enrollment_id"`
	SessionID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_session_enrollment" json:"session_id"`
	Date         time.Time `gorm:"type:date;not null;index:idx_enrollment_date" json:"date"`
	Status       string    `gorm:"type:varchar(50);not null" json:"status"` // 'present', 'absent', 'late', 'excused'
	CreatedAt    time.Time `gorm:"type:timestamp;not null;default:now()" json:"created_at"`
	UpdatedAt    time.Time `gorm:"type:timestamp;not null;default:now()" json:"updated_at"`

	Enrollment *Enrollment   `gorm:"foreignKey:EnrollmentID" json:"enrollment,omitempty"`
	Session    *ClassSession `gorm:"foreignKey:SessionID" json:"session,omitempty"`
}

type AttendanceQuery struct {
	Page         int
	PageSize     int
	EnrollmentID *uuid.UUID
	StudentID    *uuid.UUID
	ScheduleID   *uuid.UUID
	DateFrom     *time.Time
	DateTo       *time.Time
	Status       string
}

type AttendanceListResponse struct {
	Items      []Attendance `json:"items"`
	Pagination Pagination   `json:"pagination"`
}

type CreateAttendanceRequest struct {
	EnrollmentID uuid.UUID `json:"enrollment_id" binding:"required"`
	ScheduleID   uuid.UUID `json:"schedule_id" binding:"required"`
	Date         string    `json:"date" binding:"required,datetime=2006-01-02"`
	Status       string    `json:"status" binding:"required,oneof=present absent late excused"`
}

type UpdateAttendanceRequest struct {
	Status string `json:"status" binding:"required,oneof=present absent late excused"`
}
