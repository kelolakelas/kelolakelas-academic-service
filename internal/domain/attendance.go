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
	// ScheduleID + Date is the legacy addressing form and keeps working
	// (KEL-134 AC4). SessionID is the newer form that also addresses
	// reschedule replacements (whose schedule is nil). Exactly one form is
	// accepted: SessionID XOR (ScheduleID, Date).
	ScheduleID *uuid.UUID `json:"schedule_id,omitempty"`
	SessionID  *uuid.UUID `json:"session_id,omitempty"`
	Date       string     `json:"date,omitempty" binding:"omitempty,datetime=2006-01-02"`
	Status     string     `json:"status" binding:"required,oneof=present absent late excused"`
}

// CreateAttendanceBySessionRequest records attendance addressed directly at a
// session (KEL-134). It covers reschedule replacements that the legacy
// schedule_id + date form cannot address.
type CreateAttendanceBySessionRequest struct {
	EnrollmentID uuid.UUID `json:"enrollment_id" binding:"required"`
	SessionID    uuid.UUID `json:"session_id" binding:"required"`
	Status       string    `json:"status" binding:"required,oneof=present absent late excused"`
}

// BulkAttendanceItem is one row of a bulk attendance request (KEL-134).
type BulkAttendanceItem struct {
	EnrollmentID uuid.UUID `json:"enrollment_id" binding:"required"`
	Status       string    `json:"status" binding:"required,oneof=present absent late excused"`
}

// BulkAttendanceRequest records the status of a whole session's students in
// one request (KEL-134). The write is idempotent: repeating the same request
// updates rows instead of creating duplicates.
type BulkAttendanceRequest struct {
	SessionID uuid.UUID            `json:"session_id" binding:"required"`
	Items     []BulkAttendanceItem `json:"items" binding:"required,min=1,max=200,dive"`
}

// BulkAttendanceResponse carries one result row per requested enrollment.
type BulkAttendanceResponse struct {
	SessionID   uuid.UUID    `json:"session_id"`
	Attendances []Attendance `json:"attendances"`
}

type UpdateAttendanceRequest struct {
	Status string `json:"status" binding:"required,oneof=present absent late excused"`
}
