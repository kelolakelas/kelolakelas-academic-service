package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Class struct {
	ID          uuid.UUID        `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID        `gorm:"type:uuid;not null;index:idx_tenant_category" json:"tenant_id"`
	CategoryID  uuid.UUID        `gorm:"type:uuid;not null;index:idx_tenant_category" json:"category_id"`
	Name        string           `gorm:"type:varchar(255);not null" json:"name"`
	Description *json.RawMessage `gorm:"type:jsonb;serializer:json" json:"description,omitempty"`
	Type        string           `gorm:"type:varchar(50);not null" json:"type"` // 'private' or 'group'
	Price       int64            `gorm:"type:bigint;not null" json:"price"`     // Financial Data: Always use int64 for money, price, balance, or amounts
	Capacity    *int             `gorm:"type:integer" json:"capacity,omitempty"`
	CreatedAt   time.Time        `gorm:"type:timestamp;not null;default:now()" json:"created_at"`
	UpdatedAt   time.Time        `gorm:"type:timestamp;not null;default:now()" json:"updated_at"`
	DeletedAt   gorm.DeletedAt   `gorm:"index" json:"deleted_at,omitempty"`

	Category *Category `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
}

type CreateClassRequest struct {
	CategoryID  uuid.UUID        `json:"category_id" binding:"required"`
	Name        string           `json:"name" binding:"required"`
	Description *json.RawMessage `json:"description,omitempty"`
	Type        string           `json:"type" binding:"required,oneof=private group"`
	Price       int64            `json:"price" binding:"required,min=0"`
	Capacity    *int             `json:"capacity,omitempty"`
}

type ClassResponse struct {
	ID          uuid.UUID        `json:"id"`
	TenantID    uuid.UUID        `json:"tenant_id"`
	CategoryID  uuid.UUID        `json:"category_id"`
	Name        string           `json:"name"`
	Description *json.RawMessage `json:"description,omitempty"`
	Type        string           `json:"type"`
	Price       int64            `json:"price"`
	Capacity    *int             `json:"capacity,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
}
