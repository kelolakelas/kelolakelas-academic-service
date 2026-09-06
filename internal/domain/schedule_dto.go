package domain

import (
	"time"

	"github.com/google/uuid"
)

// Scenario 1: Create Initial Schedules for an Existing Class
type ScheduleItemRequest struct {
	EnrollmentID *uuid.UUID `json:"enrollment_id,omitempty"`
	TutorID      *uuid.UUID `json:"tutor_id,omitempty"`
	Capacity     int        `json:"capacity" binding:"required,min=1"`
	Location     *string    `json:"location,omitempty"`
	DayOfWeek    int        `json:"day_of_week" binding:"required,min=1,max=7"` // 1=Monday, ..., 7=Sunday
	StartTime    string     `json:"start_time" binding:"required"`              // HH:MM:SS
	EndTime      string     `json:"end_time" binding:"required"`                // HH:MM:SS
	ValidFrom    *time.Time `json:"valid_from,omitempty"`
	ValidUntil   *time.Time `json:"valid_until,omitempty"`
}

type CreateInitialSchedulesRequest struct {
	ClassID   uuid.UUID             `json:"class_id" binding:"required"`
	Schedules []ScheduleItemRequest `json:"schedules" binding:"required,min=1,dive"`
}

type CreateInitialSchedulesResponse struct {
	Schedules []*ClassSchedule `json:"schedules"`
	Sessions  []*ClassSession  `json:"sessions"`
}

// Scenario 2: Temporary Schedule Change (One-off Reschedule / Make-up Class)
type RescheduleSessionRequest struct {
	SessionID      uuid.UUID `json:"session_id" binding:"required"`
	NewSessionDate time.Time `json:"new_session_date" binding:"required"`
	NewStartTime   string    `json:"new_start_time" binding:"required"`
	NewEndTime     string    `json:"new_end_time" binding:"required"`
	NewLocation    *string   `json:"new_location,omitempty"`
}

type RescheduleSessionResponse struct {
	OriginalSession *ClassSession `json:"original_session"`
	NewSession      *ClassSession `json:"new_session"`
}

// Scenario 3: Permanent Schedule Change
type PermanentScheduleChangeRequest struct {
	OldScheduleID uuid.UUID `json:"old_schedule_id" binding:"required"`
	NewDayOfWeek  int       `json:"new_day_of_week" binding:"required,min=1,max=7"`
	NewStartTime  string    `json:"new_start_time" binding:"required"`
	NewEndTime    string    `json:"new_end_time" binding:"required"`
	EffectiveDate time.Time `json:"effective_date" binding:"required"`
}

type PermanentScheduleChangeResponse struct {
	OldSchedule *ClassSchedule  `json:"old_schedule"`
	NewSchedule *ClassSchedule  `json:"new_schedule"`
	NewSessions []*ClassSession `json:"new_sessions"`
}

// Scenario 4: Temporary Tutor Change (Substitute Teacher)
type SubstituteTutorRequest struct {
	SessionID         uuid.UUID `json:"session_id" binding:"required"`
	SubstituteTutorID uuid.UUID `json:"substitute_tutor_id" binding:"required"`
}

type SubstituteTutorResponse struct {
	Session *ClassSession `json:"session"`
}

// Scenario 5: Permanent Tutor Change
type PermanentTutorChangeRequest struct {
	ScheduleID    uuid.UUID `json:"schedule_id" binding:"required"`
	NewTutorID    uuid.UUID `json:"new_tutor_id" binding:"required"`
	EffectiveDate time.Time `json:"effective_date" binding:"required"`
}

type PermanentTutorChangeResponse struct {
	OldSchedule *ClassSchedule `json:"old_schedule"`
	NewSchedule *ClassSchedule `json:"new_schedule"`
}

type SessionAttendeesResponse struct {
	SessionID uuid.UUID     `json:"session_id"`
	ClassID   uuid.UUID     `json:"class_id"`
	Attendees []*Enrollment `json:"attendees"`
}
