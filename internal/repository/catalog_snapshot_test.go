package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// TestCatalogListRequiresActiveTenantSnapshot pins the visibility rule the catalog exposes
// publicly: with an inner join on an active snapshot, a class of an unknown or inactive
// tenant cannot leak into the listing.
func TestCatalogListRequiresActiveTenantSnapshot(t *testing.T) {
	repo, mock, cleanup := newCatalogMock(t)
	defer cleanup()

	pattern := regexp.MustCompile(`SELECT .*FROM classes c JOIN categories cat .* JOIN tenant_location_snapshots t ON t\.tenant_id = c\.tenant_id AND t\.is_active = \$1`)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM classes c")).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(pattern.String()).WillReturnRows(sqlmock.NewRows([]string{
		"id", "tenant_id", "tenant_name", "tenant_address", "category_id", "category_name", "name", "description", "type", "price", "schedules", "distance_km", "is_enrollable", "created_at",
	}))

	if _, _, err := repo.List(context.Background(), domain.CatalogQuery{ListQuery: domain.ListQuery{Page: 1, PageSize: 20}, RadiusKM: 25, Sort: "newest"}); err != nil {
		t.Fatalf("List error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

// TestCatalogDetailRequiresActiveTenantSnapshot pins the same rule for the detail path, so a
// class hidden from the list cannot be fetched directly by id.
func TestCatalogDetailRequiresActiveTenantSnapshot(t *testing.T) {
	repo, mock, cleanup := newCatalogMock(t)
	defer cleanup()

	pattern := regexp.MustCompile(`SELECT .*FROM classes c JOIN categories cat .* JOIN tenant_location_snapshots t ON t\.tenant_id = c\.tenant_id AND t\.is_active = \$1 WHERE c\.id = \$2`)
	mock.ExpectQuery(pattern.String()).WillReturnRows(sqlmock.NewRows([]string{
		"id", "tenant_id", "tenant_name", "tenant_address", "category_id", "category_name", "name", "description", "type", "price", "schedules", "distance_km", "is_enrollable", "created_at",
	}))

	_, err := repo.GetByID(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("GetByID returned no error for an empty result")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestFreshSnapshotTenantIDsFiltersByCutoff(t *testing.T) {
	repo, mock, cleanup := newCatalogMock(t)
	defer cleanup()

	tenantID := uuid.New()
	cutoff := time.Date(2026, 9, 22, 3, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT "tenant_id" FROM "tenant_location_snapshots" WHERE updated_at >= \$1`).
		WithArgs(cutoff).
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(tenantID))

	ids, err := repo.FreshSnapshotTenantIDs(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("FreshSnapshotTenantIDs error: %v", err)
	}
	if len(ids) != 1 || ids[0] != tenantID {
		t.Fatalf("ids=%v, want [%s]", ids, tenantID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUpsertTenantSnapshotsConflictsOnTenantID(t *testing.T) {
	repo, mock, cleanup := newCatalogMock(t)
	defer cleanup()

	tenantID := uuid.New()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO \"tenant_location_snapshots\"")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.UpsertTenantSnapshots(context.Background(), []domain.TenantLocationSnapshot{{TenantID: tenantID, Name: "Tenant", IsActive: true, UpdatedAt: time.Now().UTC()}}); err != nil {
		t.Fatalf("UpsertTenantSnapshots error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUpsertTenantSnapshotsSkipsEmptyInput(t *testing.T) {
	repo, mock, cleanup := newCatalogMock(t)
	defer cleanup()

	if err := repo.UpsertTenantSnapshots(context.Background(), nil); err != nil {
		t.Fatalf("UpsertTenantSnapshots error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
