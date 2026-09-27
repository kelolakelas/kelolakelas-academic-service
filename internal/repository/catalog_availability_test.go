package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

func newCatalogMock(t *testing.T) (*catalogRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return &catalogRepository{db: db}, mock, func() { sqlDB.Close() }
}

func TestCatalogDetailIncludesScheduleLocation(t *testing.T) {
	repo, mock, cleanup := newCatalogMock(t)
	defer cleanup()
	classID, tenantID := uuid.New(), uuid.New()
	scheduleJSON := `[{"location":"Ruang A","available_slots":0,"is_available":false},{"location":null,"available_slots":2,"is_available":true}]`
	mock.ExpectQuery(`SELECT .*'location', cs\.location.*AS schedules.*FROM classes c`).WillReturnRows(sqlmock.NewRows([]string{
		"id", "tenant_id", "tenant_name", "tenant_address", "category_id", "category_name", "name", "description", "type", "price", "schedules", "is_enrollable", "created_at",
	}).AddRow(classID, tenantID, "Tenant", "Address", uuid.New(), "Math", "Group", nil, "group", int64(100), []byte(scheduleJSON), true, time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)))
	item, err := repo.GetByID(context.Background(), classID)
	if err != nil {
		t.Fatalf("GetByID error: %v", err)
	}
	if string(item.Schedules) != scheduleJSON {
		t.Fatalf("schedules=%s, want %s", item.Schedules, scheduleJSON)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestCatalogListEscapesPercentSearch(t *testing.T) {
	repo, mock, cleanup := newCatalogMock(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT count.*c.name ILIKE.*ESCAPE`).
		WithArgs(true, true, "open", `%50\%%`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT .*FROM classes c.*c.name ILIKE.*ESCAPE`).
		WithArgs(true, true, "open", `%50\%%`, 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "tenant_name", "tenant_address", "category_id", "category_name", "name", "description", "type", "price", "schedules", "distance_km", "is_enrollable", "created_at"}))

	_, total, err := repo.List(context.Background(), domain.CatalogQuery{ListQuery: domain.ListQuery{Page: 1, PageSize: 20, Search: "50%"}, RadiusKM: 25, Sort: "newest"})
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if total != 0 {
		t.Fatalf("total=%d, want 0", total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestCategoryListEscapesUnderscoreSearch(t *testing.T) {
	catalogRepo, mock, cleanup := newCatalogMock(t)
	defer cleanup()
	repo := &categoryRepository{db: catalogRepo.db}
	tenantID := uuid.New()

	mock.ExpectQuery(`SELECT count.*FROM "categories"`).
		WithArgs(tenantID, `%a\_b%`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT .*FROM "categories"`).
		WithArgs(tenantID, `%a\_b%`, 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "description", "created_at", "updated_at", "deleted_at"}))

	_, total, err := repo.ListByTenant(context.Background(), tenantID, domain.ListQuery{Page: 1, PageSize: 20, Search: "a_b"})
	if err != nil {
		t.Fatalf("ListByTenant error: %v", err)
	}
	if total != 0 {
		t.Fatalf("total=%d, want 0", total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestCatalogAvailabilityIsReturnedPerSchedule(t *testing.T) {
	tenantID, classID, firstScheduleID, secondScheduleID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	scheduleJSON := `[{"id":"` + firstScheduleID.String() + `","location":"Ruang A","capacity":10,"available_slots":8,"is_available":true},{"id":"` + secondScheduleID.String() + `","location":null,"capacity":8,"available_slots":0,"is_available":false}]`
	repo, mock, cleanup := newCatalogMock(t)
	defer cleanup()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM classes c")).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT .*'location', cs\.location.*AS schedules.*FROM classes c`).WillReturnRows(sqlmock.NewRows([]string{
		"id", "tenant_id", "tenant_name", "tenant_address", "category_id", "category_name", "name", "description", "type", "price", "schedules", "distance_km", "is_enrollable", "created_at",
	}).AddRow(classID, tenantID, "Tenant", "Address", uuid.New(), "Math", "Group", nil, "group", int64(100), []byte(scheduleJSON), nil, true, time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)))

	items, total, err := repo.List(context.Background(), domain.CatalogQuery{ListQuery: domain.ListQuery{Page: 1, PageSize: 20}, RadiusKM: 25, Sort: "newest"})
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("items=%d total=%d, want one item", len(items), total)
	}
	if string(items[0].Schedules) != scheduleJSON {
		t.Fatalf("schedules=%s, want %s", items[0].Schedules, scheduleJSON)
	}
	if !strings.Contains(string(items[0].Schedules), `"location":null`) {
		t.Fatal("nullable location missing from list response")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
