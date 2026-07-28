package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
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
	Name        string           `json:"name" binding:"required"`
	Description *json.RawMessage `json:"description,omitempty"`
}

type CategoryResponse struct {
	ID          uuid.UUID        `json:"id"`
	TenantID    uuid.UUID        `json:"tenant_id"`
	Name        string           `json:"name"`
	Description *json.RawMessage `json:"description,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
}
