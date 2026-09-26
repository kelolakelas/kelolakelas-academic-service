package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type catalogRepository struct{ db *gorm.DB }

func NewCatalogRepository(db *gorm.DB) domain.CatalogRepository { return &catalogRepository{db: db} }

func (r *catalogRepository) TenantIDs(ctx context.Context) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Table("classes").Where("deleted_at IS NULL").Distinct("tenant_id").Pluck("tenant_id", &ids).Error
	return ids, err
}

// FreshSnapshotTenantIDs returns the tenants whose snapshot is at least as fresh as the
// cutoff. The catalog subtracts it from the tenants that own classes, so a missing or aged
// snapshot triggers exactly one refresh instead of a write on every request.
func (r *catalogRepository) FreshSnapshotTenantIDs(ctx context.Context, since time.Time) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Table("tenant_location_snapshots").Where("updated_at >= ?", since).Pluck("tenant_id", &ids).Error
	return ids, err
}

// UpsertTenantSnapshots writes only the refreshed rows and their timestamps, so an
// unchanged tenant keeps the snapshot age that the TTL is measured against.
func (r *catalogRepository) UpsertTenantSnapshots(ctx context.Context, snapshots []domain.TenantLocationSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "address_formatted", "latitude", "longitude", "is_active", "updated_at"}),
	}).Create(&snapshots).Error
}

func catalogValidityDateSQL() string {
	return "DATE '" + time.Now().Format("2006-01-02") + "'"
}

func (r *catalogRepository) List(ctx context.Context, query domain.CatalogQuery) ([]domain.CatalogItem, int64, error) {
	// An inner join on an active tenant snapshot keeps list and detail consistent: a class
	// of an inactive (or unknown) tenant is not visible on either path.
	db := r.db.WithContext(ctx).Table("classes c").Joins("JOIN categories cat ON cat.id = c.category_id AND cat.deleted_at IS NULL").Joins("JOIN tenant_location_snapshots t ON t.tenant_id = c.tenant_id AND t.is_active = ?", true).Where("c.deleted_at IS NULL AND c.is_published = ? AND c.enrollment_status = ?", true, "open")
	if query.Search != "" {
		db = db.Where("c.name ILIKE ?", "%"+query.Search+"%")
	}
	if query.CategoryID != nil {
		db = db.Where("c.category_id = ?", *query.CategoryID)
	}
	if query.TenantID != nil {
		db = db.Where("c.tenant_id = ?", *query.TenantID)
	}
	if query.Type != "" {
		db = db.Where("c.type = ?", query.Type)
	}
	if query.MinPrice != nil {
		db = db.Where("c.price >= ?", *query.MinPrice)
	}
	if query.MaxPrice != nil {
		db = db.Where("c.price <= ?", *query.MaxPrice)
	}
	distance := "NULL::double precision"
	if query.Latitude != nil {
		distance = "6371 * acos(least(1, greatest(-1, sin(radians(?)) * sin(radians(t.latitude)) + cos(radians(?)) * cos(radians(t.latitude)) * cos(radians(t.longitude) - radians(?)))))"
	}
	if query.Latitude != nil {
		db = db.Where("t.latitude IS NOT NULL AND t.longitude IS NOT NULL AND "+distance+" <= ?", *query.Latitude, *query.Latitude, *query.Longitude, query.RadiusKM)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := "c.created_at DESC"
	switch query.Sort {
	case "distance_asc":
		order = "distance_km ASC"
	case "name_asc":
		order = "c.name ASC"
	case "price_asc":
		order = "c.price ASC"
	case "price_desc":
		order = "c.price DESC"
	}
	items := make([]domain.CatalogItem, 0)
	schedules := "COALESCE((SELECT jsonb_agg(jsonb_build_object('id', cs.id, 'day_of_week', cs.day_of_week, 'start_time', cs.start_time, 'end_time', cs.end_time, 'location', cs.location, 'capacity', cs.capacity, 'available_slots', GREATEST(cs.capacity - (SELECT COUNT(*) FROM enrollments e WHERE e.schedule_id = cs.id AND e.status IN ('pending', 'active') AND e.deleted_at IS NULL), 0), 'is_available', GREATEST(cs.capacity - (SELECT COUNT(*) FROM enrollments e WHERE e.schedule_id = cs.id AND e.status IN ('pending', 'active') AND e.deleted_at IS NULL), 0) > 0)) FROM class_schedules cs WHERE cs.class_id = c.id AND cs.deleted_at IS NULL AND (cs.valid_until IS NULL OR cs.valid_until >= " + catalogValidityDateSQL() + ")), '[]'::jsonb)"
	selectSQL := "c.id, c.tenant_id, t.name AS tenant_name, t.address_formatted AS tenant_address, c.category_id, cat.name AS category_name, c.name, c.description, c.type, c.price, " + schedules + " AS schedules, " + distance + " AS distance_km, c.enrollment_status = 'open' AS is_enrollable, c.created_at"
	args := []interface{}{}
	if query.Latitude != nil {
		args = []interface{}{*query.Latitude, *query.Latitude, *query.Longitude}
	}
	if err := db.Select(selectSQL, args...).Order(order).Limit(query.PageSize).Offset((query.Page - 1) * query.PageSize).Scan(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *catalogRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.CatalogItem, error) {
	var item domain.CatalogItem
	schedules := "COALESCE((SELECT jsonb_agg(jsonb_build_object('id', cs.id, 'day_of_week', cs.day_of_week, 'start_time', cs.start_time, 'end_time', cs.end_time, 'location', cs.location, 'capacity', cs.capacity, 'available_slots', GREATEST(cs.capacity - (SELECT COUNT(*) FROM enrollments e WHERE e.schedule_id = cs.id AND e.status IN ('pending', 'active') AND e.deleted_at IS NULL), 0), 'is_available', GREATEST(cs.capacity - (SELECT COUNT(*) FROM enrollments e WHERE e.schedule_id = cs.id AND e.status IN ('pending', 'active') AND e.deleted_at IS NULL), 0) > 0)) FROM class_schedules cs WHERE cs.class_id = c.id AND cs.deleted_at IS NULL AND (cs.valid_until IS NULL OR cs.valid_until >= " + catalogValidityDateSQL() + ")), '[]'::jsonb)"
	err := r.db.WithContext(ctx).Table("classes c").Joins("JOIN categories cat ON cat.id = c.category_id AND cat.deleted_at IS NULL").Joins("JOIN tenant_location_snapshots t ON t.tenant_id = c.tenant_id AND t.is_active = ?", true).Where("c.id = ? AND c.deleted_at IS NULL AND c.is_published = ? AND c.enrollment_status = ?", id, true, "open").Select("c.id, c.tenant_id, t.name AS tenant_name, t.address_formatted AS tenant_address, c.category_id, cat.name AS category_name, c.name, c.description, c.type, c.price, " + schedules + " AS schedules, c.enrollment_status = 'open' AS is_enrollable, c.created_at").Scan(&item).Error
	if err != nil {
		return nil, err
	}
	if item.ID == uuid.Nil {
		return nil, gorm.ErrRecordNotFound
	}
	return &item, nil
}
