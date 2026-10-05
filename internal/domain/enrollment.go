package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrInvalidEnrollmentStatus = errors.New("invalid enrollment status")
var ErrIdempotencyConflict = errors.New("idempotency key already used with a different request")
var ErrParentRequired = errors.New("parent authentication is required")
var ErrInvalidPaymentMethod = errors.New("invalid payment method")
var ErrStudentOwnership = errors.New("student does not belong to parent")
var ErrInvalidEnrollmentTransition = errors.New("invalid enrollment transition")
var ErrScheduleNotFound = errors.New("schedule not found")
var ErrScheduleClassMismatch = errors.New("schedule does not belong to enrollment class")
var ErrScheduleFull = errors.New("schedule capacity is full")
var ErrScheduleEnded = errors.New("schedule has ended")
var ErrScheduleRequired = errors.New("a schedule is required for group enrollment")

// Enrollment lifecycle statuses. 'suspended' (KEL-149) is set by the internal
// billing-driven endpoints and holds no seat: every seat-counting predicate in
// the repository counts only pending and active rows, so a suspended enrollment
// frees its schedule slot until it is resumed or ended.
const (
	EnrollmentStatusPending   = "pending"
	EnrollmentStatusActive    = "active"
	EnrollmentStatusSuspended = "suspended"
	EnrollmentStatusCompleted = "completed"
	EnrollmentStatusDropped   = "dropped"
)

// ErrEnrollmentSuspendedConflict reports that an enrollment could not leave
// `suspended` because the class or schedule no longer has a seat for it (or the
// student already holds another live enrollment there). The enrollment stays
// suspended; the caller's retry changes nothing until a seat frees up.
var ErrEnrollmentSuspendedConflict = errors.New("enrollment cannot resume: no seat available")

// ErrDuplicateEnrollment reports that the student already holds a pending or
// active enrollment in the class (the rows covered by the partial unique index
// idx_student_class_active). A dropped, completed, or soft-deleted enrollment
// does not count, so a student can enroll again after cancelling or expiring.
var ErrDuplicateEnrollment = errors.New("student already has a pending or active enrollment in this class")

// DuplicateEnrollmentErrorCode is the machine-readable `code` returned with the
// HTTP 409 for ErrDuplicateEnrollment, so clients can tell it apart from a full
// schedule or an idempotency conflict without parsing the message.
const DuplicateEnrollmentErrorCode = "duplicate_enrollment"

// ErrPlatformFeeExceedsGross reports that billing permanently refused the
// enrollment invoice because the platform fee plus the payment gateway fee exceed
// the gross amount (billing KEL-99). The enrollment created for the attempt is
// dropped so it holds no seat and does not block a new attempt, and a replay of
// the same Idempotency-Key answers this error again instead of a success. The
// message equals billing's, the owner-approved text for this rejection.
var ErrPlatformFeeExceedsGross = errors.New("Biaya platform melebihi jumlah pembayaran")

// PlatformFeeExceedsGrossErrorCode is the machine-readable `code` returned with
// the HTTP 422 for ErrPlatformFeeExceedsGross. It equals billing's code on purpose.
const PlatformFeeExceedsGrossErrorCode = "platform_fee_exceeds_gross"

// PaymentStatusPlatformFeeRejected marks an enrollment dropped because billing
// refused its invoice for ErrPlatformFeeExceedsGross. It is what lets an
// Idempotency-Key replay answer the rejection rather than retry the invoice, and it
// tells this drop apart from a parent cancellation or an expired payment.
const PaymentStatusPlatformFeeRejected = "platform_fee_rejected"

type Enrollment struct {
	ID                   uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TenantID             uuid.UUID      `gorm:"type:uuid;not null;index:idx_tenant_status" json:"tenant_id"` // Cross-service, ordinary UUID
	StudentID            uuid.UUID      `gorm:"type:uuid;not null;index:idx_student_class,unique" json:"student_id"`
	ClassID              uuid.UUID      `gorm:"type:uuid;not null;index:idx_student_class,unique" json:"class_id"`
	ScheduleID           *uuid.UUID     `gorm:"type:uuid;index:idx_enrollment_schedule_status" json:"schedule_id,omitempty"`
	Status               string         `gorm:"type:varchar(50);not null;index:idx_tenant_status" json:"status"` // 'pending', 'active', 'suspended', 'completed', 'dropped'
	BillingCycle         string         `gorm:"type:varchar(20);not null;default:'monthly'" json:"billing_cycle"`
	IdempotencyKey       *string        `gorm:"type:varchar(255);index:idx_enrollment_idempotency,unique" json:"-"`
	PaymentTransactionID *uuid.UUID     `gorm:"type:uuid" json:"-"`
	CheckoutSessionURL   *string        `gorm:"type:text" json:"-"`
	PaymentStatus        string         `gorm:"type:varchar(30);default:'pending'" json:"-"`
	GrossAmount          int64          `gorm:"type:bigint;not null;default:0" json:"-"`
	JoinedAt             time.Time      `gorm:"type:timestamp;not null;default:now()" json:"joined_at"`
	UpdatedAt            time.Time      `gorm:"type:timestamp;not null;default:now()" json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	Student *Student `gorm:"foreignKey:StudentID" json:"student,omitempty"`
	Class   *Class   `gorm:"foreignKey:ClassID" json:"class,omitempty"`
	// Schedule is loaded only by the scoped read paths (list and detail) so the
	// response can carry a schedule summary. It is hidden from JSON because raw
	// enrollments are also serialized elsewhere (e.g. session attendees), whose
	// contract must not change; EnrollmentResponse.Schedule is the public shape.
	Schedule *ClassSchedule `gorm:"foreignKey:ScheduleID;references:ID" json:"-"`
}

type EnrollStudentRequest struct {
	StudentID      uuid.UUID  `json:"student_id" binding:"required"`
	ClassID        uuid.UUID  `json:"class_id" binding:"required"`
	BillingCycle   string     `json:"billing_cycle" binding:"required,oneof=monthly quarterly yearly"`
	ScheduleID     *uuid.UUID `json:"schedule_id,omitempty"`
	PaymentMethod  string     `json:"payment_method,omitempty" binding:"omitempty,oneof=VC VA BC SP NQ"`
	IdempotencyKey string     `json:"-"`
}

var ErrVoucherRejected = errors.New("Voucher tidak dapat digunakan. Mulai checkout baru tanpa voucher.")

const VoucherRejectedErrorCode = "voucher_rejected"
const PaymentStatusVoucherRejected = "voucher_rejected"

type VoucherPreviewRequest struct {
	VoucherCode string `json:"voucher_code" binding:"required,max=255"`
}

type PublicEnrollmentRequest struct {
	VoucherCode   string     `json:"voucher_code,omitempty" binding:"omitempty,max=255"`
	StudentID     uuid.UUID  `json:"student_id" binding:"required"`
	BillingCycle  string     `json:"billing_cycle" binding:"required,oneof=monthly quarterly yearly"`
	ScheduleID    *uuid.UUID `json:"schedule_id,omitempty"`
	PaymentMethod string     `json:"payment_method,omitempty" binding:"omitempty,oneof=VC VA BC SP NQ"`
	// SenderEmail is filled in by the handler from the verified JWT email claim
	// (KEL-75). The `json:"-"` binding keeps a client from supplying it through
	// the request body; it is never read from headers either.
	SenderEmail string `json:"-"`
}

type AssignEnrollmentScheduleRequest struct {
	ScheduleID uuid.UUID `json:"schedule_id" binding:"required"`
}

type PublicEnrollmentResponse struct {
	Enrollment *EnrollmentResponse `json:"enrollment"`
	Payment    *PaymentResponse    `json:"payment"`
}

type PaymentResponse struct {
	TransactionID      uuid.UUID `json:"transaction_id"`
	CheckoutSessionURL string    `json:"checkout_session_url"`
	GrossAmount        int64     `json:"gross_amount"`
	Status             string    `json:"status"`
}

type EnrollmentResponse struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	StudentID    uuid.UUID  `json:"student_id"`
	ClassID      uuid.UUID  `json:"class_id"`
	ScheduleID   *uuid.UUID `json:"schedule_id,omitempty"`
	Status       string     `json:"status"`
	JoinedAt     time.Time  `json:"joined_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Class        *Class     `json:"class,omitempty"`
	Student      *Student   `json:"student,omitempty"`
	BillingCycle string     `json:"billing_cycle"`
	// Schedule summarises the weekly slot behind ScheduleID (KEL-70). It is
	// omitted when the enrollment has no schedule (private class) or when the
	// schedule is no longer live (soft-deleted), so clients must treat it as
	// optional even when schedule_id is present.
	Schedule *EnrollmentScheduleSummary `json:"schedule,omitempty"`
}

// EnrollmentScheduleSummary is the read-only view of an enrollment's weekly
// schedule. It deliberately exposes only what a parent needs to know when the
// class meets; capacity, tutor and validity window stay internal.
type EnrollmentScheduleSummary struct {
	DayOfWeek int     `json:"day_of_week" example:"1"`       // 1 = Senin, 7 = Minggu (ISO 8601)
	StartTime string  `json:"start_time" example:"16:00:00"` // HH:MM:SS
	EndTime   string  `json:"end_time" example:"17:30:00"`   // HH:MM:SS
	Location  *string `json:"location,omitempty" example:"Ruang A"`
}

type EnrollmentQuery struct {
	Page      int
	PageSize  int
	Status    string
	ClassID   *uuid.UUID
	StudentID *uuid.UUID
	Search    string
	DateFrom  *time.Time
	DateTo    *time.Time
}

type EnrollmentListResponse struct {
	Items      []*EnrollmentResponse `json:"items"`
	Pagination Pagination            `json:"pagination"`
}
