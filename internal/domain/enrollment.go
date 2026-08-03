package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrInvalidEnrollmentStatus = errors.New("invalid enrollment status")

type Enrollment struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID      `gorm:"type:uuid;not null;index:idx_tenant_status" json:"tenant_id"` // Cross-service, ordinary UUID
	StudentID uuid.UUID      `gorm:"type:uuid;not null;index:idx_student_class,unique" json:"student_id"`
	ClassID   uuid.UUID      `gorm:"type:uuid;not null;index:idx_student_class,unique" json:"class_id"`
	Status    string         `gorm:"type:varchar(50);not null;index:idx_tenant_status" json:"status"` // 'pending', 'active', 'completed', 'dropped'
	JoinedAt  time.Time      `gorm:"type:timestamp;not null;default:now()" json:"joined_at"`
	UpdatedAt time.Time      `gorm:"type:timestamp;not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	Student *Student `gorm:"foreignKey:StudentID" json:"student,omitempty"`
	Class   *Class   `gorm:"foreignKey:ClassID" json:"class,omitempty"`
}

type UpdateEnrollmentStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

type EnrollStudentRequest struct {
	StudentID    uuid.UUID `json:"student_id" binding:"required"`
	ClassID      uuid.UUID `json:"class_id" binding:"required"`
	BillingCycle string    `json:"billing_cycle" binding:"required,oneof=monthly quarterly yearly"`
	PlatformFee  int64     `json:"platform_fee" binding:"gte=0"`
}

type EnrollmentResponse struct {
	ID           uuid.UUID `json:"id"`
	TenantID     uuid.UUID `json:"tenant_id"`
	StudentID    uuid.UUID `json:"student_id"`
	ClassID      uuid.UUID `json:"class_id"`
	Status       string    `json:"status"`
	JoinedAt     time.Time `json:"joined_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Class        *Class    `json:"class,omitempty"`
	Student      *Student  `json:"student,omitempty"`
	BillingCycle string    `json:"billing_cycle"`
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
