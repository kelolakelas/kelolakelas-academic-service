package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrPrivateRequestNotFound   = errors.New("schedule request not found")
	ErrPrivateRequestConflict   = errors.New("student already has a pending schedule request for this class")
	ErrPrivateRequestTransition = errors.New("schedule request is no longer pending")
	ErrPrivateClassRequired     = errors.New("schedule requests require a private class")
	ErrPrivateRequestSlots      = errors.New("slots must have valid non-overlapping times with end_time after start_time")
	ErrPrivateCheckout          = errors.New("private classes require an approved schedule request; direct checkout is unavailable")
)

const PrivateCheckoutErrorCode = "private_schedule_request_required"

type PrivateScheduleSlot struct {
	DayOfWeek int    `json:"day_of_week" binding:"required,min=1,max=7"`
	StartTime string `json:"start_time" binding:"required"`
	EndTime   string `json:"end_time" binding:"required"`
}

type CreatePrivateScheduleRequest struct {
	StudentID    uuid.UUID             `json:"student_id" binding:"required"`
	BillingCycle string                `json:"billing_cycle" binding:"required,oneof=monthly quarterly yearly"`
	Slots        []PrivateScheduleSlot `json:"slots" binding:"required,min=1,dive"`
	Note         *string               `json:"note,omitempty" binding:"omitempty,max=2000"`
	// ParentEmail is populated solely from the verified token.
	ParentEmail string `json:"-"`
}

type RejectPrivateScheduleRequest struct {
	Reason           *string               `json:"reason,omitempty" binding:"omitempty,max=2000"`
	RecommendedSlots []PrivateScheduleSlot `json:"recommended_slots,omitempty"`
}

type PrivateScheduleRequest struct {
	ID               uuid.UUID             `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID         uuid.UUID             `gorm:"type:uuid;not null" json:"tenant_id"`
	ClassID          uuid.UUID             `gorm:"type:uuid;not null" json:"class_id"`
	StudentID        uuid.UUID             `gorm:"type:uuid;not null" json:"student_id"`
	ParentID         uuid.UUID             `gorm:"type:uuid;not null" json:"parent_id"`
	ParentEmail      string                `gorm:"type:text;not null" json:"parent_email"`
	BillingCycle     string                `gorm:"type:varchar(20);not null" json:"billing_cycle"`
	Slots            []PrivateScheduleSlot `gorm:"serializer:json;type:jsonb;not null" json:"slots"`
	RecommendedSlots []PrivateScheduleSlot `gorm:"serializer:json;type:jsonb" json:"recommended_slots,omitempty"`
	Note             *string               `gorm:"type:text" json:"note,omitempty"`
	Status           string                `gorm:"type:varchar(20);not null" json:"status"`
	RejectionReason  *string               `gorm:"type:text" json:"rejection_reason,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
	DecidedAt        *time.Time            `json:"decided_at,omitempty"`
}

func (*PrivateScheduleRequest) TableName() string { return "private_schedule_requests" }
