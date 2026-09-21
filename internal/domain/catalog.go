package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidCatalogQuery = errors.New("invalid catalog query")

type CatalogQuery struct {
	ListQuery
	Latitude   *float64
	Longitude  *float64
	RadiusKM   float64
	Sort       string
	CategoryID *uuid.UUID
	TenantID   *uuid.UUID
	Type       string
	MinPrice   *int64
	MaxPrice   *int64
}

func (q CatalogQuery) Validate() error {
	if (q.Latitude == nil) != (q.Longitude == nil) {
		return ErrInvalidCatalogQuery
	}
	if q.Latitude != nil && (*q.Latitude < -90 || *q.Latitude > 90 || *q.Longitude < -180 || *q.Longitude > 180) {
		return ErrInvalidCatalogQuery
	}
	if q.RadiusKM <= 0 || q.RadiusKM > 100 {
		return ErrInvalidCatalogQuery
	}
	if q.Type != "" && q.Type != "private" && q.Type != "group" {
		return ErrInvalidCatalogQuery
	}
	if q.MinPrice != nil && *q.MinPrice < 0 {
		return ErrInvalidCatalogQuery
	}
	if q.MaxPrice != nil && *q.MaxPrice < 0 {
		return ErrInvalidCatalogQuery
	}
	if q.MinPrice != nil && q.MaxPrice != nil && *q.MinPrice > *q.MaxPrice {
		return ErrInvalidCatalogQuery
	}
	switch q.Sort {
	case "distance_asc", "name_asc", "price_asc", "price_desc", "newest":
		return nil
	default:
		return ErrInvalidCatalogQuery
	}
}

type TenantLocationSnapshot struct {
	TenantID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name             string
	AddressFormatted *string
	Latitude         *float64
	Longitude        *float64
	IsActive         bool
	UpdatedAt        time.Time
}

type CatalogItem struct {
	ID              uuid.UUID        `json:"id"`
	TenantID        uuid.UUID        `json:"tenant_id"`
	TenantName      string           `json:"tenant_name"`
	TenantAddress   *string          `json:"tenant_address,omitempty"`
	TenantLatitude  *float64         `json:"latitude,omitempty"`
	TenantLongitude *float64         `json:"longitude,omitempty"`
	CategoryID      uuid.UUID        `json:"category_id"`
	CategoryName    string           `json:"category_name"`
	Name            string           `json:"name"`
	Description     *json.RawMessage `json:"description,omitempty"`
	Type            string           `json:"type"`
	Price           int64            `json:"price"`
	Schedules       json.RawMessage  `json:"schedules"`
	DistanceKM      *float64         `json:"distance_km,omitempty"`
	IsEnrollable    bool             `json:"is_enrollable"`
	CreatedAt       time.Time        `json:"created_at"`
}

type CatalogListResponse struct {
	Items      []CatalogItem `json:"items"`
	Pagination Pagination    `json:"pagination"`
}

type CatalogRepository interface {
	TenantIDs(ctx context.Context) ([]uuid.UUID, error)
	FreshSnapshotTenantIDs(ctx context.Context, since time.Time) ([]uuid.UUID, error)
	UpsertTenantSnapshots(ctx context.Context, snapshots []TenantLocationSnapshot) error
	List(ctx context.Context, query CatalogQuery) ([]CatalogItem, int64, error)
	GetByID(ctx context.Context, id uuid.UUID) (*CatalogItem, error)
}
