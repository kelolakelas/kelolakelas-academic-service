package repository

import (
	"context"
	"regexp"
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

func TestCatalogAvailabilityIsReturnedPerSchedule(t *testing.T) {
	tenantID, classID, firstScheduleID, secondScheduleID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	scheduleJSON := `[{"id":"` + firstScheduleID.String() + `","capacity":10,"available_slots":8,"is_available":true},{"id":"` + secondScheduleID.String() + `","capacity":8,"available_slots":0,"is_available":false}]`
	repo, mock, cleanup := newCatalogMock(t)
	defer cleanup()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM classes c")).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT .*schedules.*FROM classes c`).WillReturnRows(sqlmock.NewRows([]string{
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
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
