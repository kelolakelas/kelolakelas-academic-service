package domain

import (
	"errors"

	"github.com/google/uuid"
)

var ErrChatContextNotFound = errors.New("chat context not found")

// These projections are the entire internal chat contract; do not embed source models.
type ScheduleRequestChatContext struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"tenant_id"`
	ParentID         uuid.UUID `json:"parent_id"`
	ClassID          uuid.UUID `json:"class_id"`
	ClassName        string    `json:"class_name"`
	StudentID        uuid.UUID `json:"student_id"`
	StudentFirstName string    `json:"student_first_name"`
	Status           string    `json:"status"`
}

type ReportChatContext struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"tenant_id"`
	EnrollmentID     uuid.UUID `json:"enrollment_id"`
	StudentID        uuid.UUID `json:"student_id"`
	StudentFirstName string    `json:"student_first_name"`
	ParentID         uuid.UUID `json:"parent_id"`
	ClassName        string    `json:"class_name"`
	Title            string    `json:"title"`
	ReporterID       uuid.UUID `json:"reporter_id"`
}
