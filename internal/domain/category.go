package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrCategoryNotFound      = errors.New("category not found")
	ErrCategoryForbidden     = errors.New("category access forbidden")
	ErrCategoryActiveClasses = errors.New("category has active classes")
	// ErrCategoryNameRequired rejects a blank category name on update. The
	// create path relies on binding:"required"; a pointer field needs an
	// explicit check.
	ErrCategoryNameRequired = errors.New("category name is required")
)

type HTTPResponse struct {
	Status  string      `json:"status" example:"success"`
	Message string      `json:"message" example:"Operation completed successfully"`
	Data    interface{} `json:"data,omitempty"`
}

type ErrorResponse struct {
	Status  string      `json:"status" example:"error"`
	Message string      `json:"message" example:"An error occurred"`
	Data    interface{} `json:"data,omitempty"`
	// Code is a stable machine-readable reason, present only on errors a client
	// must tell apart from others with the same HTTP status (e.g. `duplicate_enrollment`).
	Code string `json:"code,omitempty" example:"duplicate_enrollment"`
}

type Category struct {
	ID          uuid.UUID        `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID        `gorm:"type:uuid;not null;index" json:"tenant_id"`
	Name        string           `gorm:"type:varchar(255);not null" json:"name"`
	Description *json.RawMessage `gorm:"type:jsonb;serializer:json" json:"description,omitempty"`
	CreatedAt   time.Time        `gorm:"type:timestamp;not null;default:now()" json:"created_at"`
	UpdatedAt   time.Time        `gorm:"type:timestamp;not null;default:now()" json:"updated_at"`
	DeletedAt   gorm.DeletedAt   `gorm:"index" json:"deleted_at,omitempty"`
}

type CreateCategoryRequest struct {
	Name        string           `json:"name" binding:"required,max=255"`
	Description *json.RawMessage `json:"description,omitempty"`
}

// UpdateCategoryRequest patches the mutable attributes of an existing category.
//
// Every field is a pointer so an omitted key means "leave unchanged": the
// handler must never treat an absent field as a request to blank the column.
// The validation rules mirror CreateCategoryRequest.
//
// Note that encoding/json cannot distinguish an omitted `description` from an
// explicit `null`, so this DTO cannot clear a description. Clearing is out of
// scope for the category update endpoint.
type UpdateCategoryRequest struct {
	Name        *string          `json:"name,omitempty" binding:"omitempty,min=1,max=255"`
	Description *json.RawMessage `json:"description,omitempty"`
}

type CategoryResponse struct {
	ID          uuid.UUID        `json:"id"`
	TenantID    uuid.UUID        `json:"tenant_id"`
	Name        string           `json:"name"`
	Description *json.RawMessage `json:"description,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
}

type ListQuery struct {
	Page     int
	PageSize int
	Search   string
}

type Pagination struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalItems int64 `json:"total_items"`
	TotalPages int   `json:"total_pages"`
}

type CategoryListResponse struct {
	Items      []CategoryResponse `json:"items"`
	Pagination Pagination         `json:"pagination"`
}

type ClassListResponse struct {
	Items      []ClassResponse `json:"items"`
	Pagination Pagination      `json:"pagination"`
}

type ScheduleListResponse struct {
	Items      []ClassSchedule `json:"items"`
	Pagination Pagination      `json:"pagination"`
}
