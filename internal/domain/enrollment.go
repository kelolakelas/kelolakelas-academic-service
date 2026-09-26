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
var ErrStudentOwnership = errors.New("student does not belong to parent")
var ErrInvalidEnrollmentTransition = errors.New("invalid enrollment transition")
var ErrScheduleNotFound = errors.New("schedule not found")
var ErrScheduleClassMismatch = errors.New("schedule does not belong to enrollment class")
var ErrScheduleFull = errors.New("schedule capacity is full")
var ErrScheduleEnded = errors.New("schedule has ended")
var ErrScheduleRequired = errors.New("a schedule is required for group enrollment")

// ErrDuplicateEnrollment reports that the student already holds a pending or
// active enrollment in the class (the rows covered by the partial unique index
// idx_student_class_active). A dropped, completed, or soft-deleted enrollment
// does not count, so a student can enroll again after cancelling or expiring.
var ErrDuplicateEnrollment = errors.New("student already has a pending or active enrollment in this class")

// DuplicateEnrollmentErrorCode is the machine-readable `code` returned with the
// HTTP 409 for ErrDuplicateEnrollment, so clients can tell it apart from a full
// schedule or an idempotency conflict without parsing the message.
const DuplicateEnrollmentErrorCode = "duplicate_enrollment"

type Enrollment struct {
	ID                   uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TenantID             uuid.UUID      `gorm:"type:uuid;not null;index:idx_tenant_status" json:"tenant_id"` // Cross-service, ordinary UUID
	StudentID            uuid.UUID      `gorm:"type:uuid;not null;index:idx_student_class,unique" json:"student_id"`
	ClassID              uuid.UUID      `gorm:"type:uuid;not null;index:idx_student_class,unique" json:"class_id"`
	ScheduleID           *uuid.UUID     `gorm:"type:uuid;index:idx_enrollment_schedule_status" json:"schedule_id,omitempty"`
	Status               string         `gorm:"type:varchar(50);not null;index:idx_tenant_status" json:"status"` // 'pending', 'active', 'completed', 'dropped'
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
}

type EnrollStudentRequest struct {
	StudentID      uuid.UUID  `json:"student_id" binding:"required"`
	ClassID        uuid.UUID  `json:"class_id" binding:"required"`
	BillingCycle   string     `json:"billing_cycle" binding:"required,oneof=monthly quarterly yearly"`
	ScheduleID     *uuid.UUID `json:"schedule_id,omitempty"`
	IdempotencyKey string     `json:"-"`
}

type PublicEnrollmentRequest struct {
	StudentID    uuid.UUID  `json:"student_id" binding:"required"`
	BillingCycle string     `json:"billing_cycle" binding:"required,oneof=monthly quarterly yearly"`
	ScheduleID   *uuid.UUID `json:"schedule_id,omitempty"`
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
