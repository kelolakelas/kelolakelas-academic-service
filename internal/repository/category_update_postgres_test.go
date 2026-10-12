package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// KEL-188 scoped category update against real PostgreSQL: a rename writes only
// the owning tenant's row, leaves the tenant's other categories and every
// foreign-tenant category untouched, and never moves a class to another
// category id.
//
// Run with KEL188_TEST_DSN against a disposable PostgreSQL database, e.g.
// docker run -d --rm --name kel188-db-test -e POSTGRES_PASSWORD=postgres -p 127.0.0.1::5432 postgres:16-alpine
//
//	KEL188_TEST_DSN="postgres://postgres:postgres@127.0.0.1:<port>/postgres?sslmode=disable" \
//	  go test -count=3 -race -run TestKEL188 ./internal/repository/
func TestKEL188CategoryScopedUpdate(t *testing.T) {
	dsn := os.Getenv("KEL188_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL188_TEST_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock(188, 1)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(188, 1)") })

	schema, err := os.ReadFile("../../migrations/00000000000000_init_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatal(err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	own, sibling, foreign := uuid.New(), uuid.New(), uuid.New()
	classID := uuid.New()
	exec := func(stmt string, args ...any) {
		t.Helper()
		if err := db.Exec(stmt, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", own, tenantA, "Matematika")
	exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", sibling, tenantA, "Fisika")
	exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", foreign, tenantB, "Matematika seberang")
	exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		classID, tenantA, own, "Aljabar Grup", "group", 100, true, "open")

	repo := NewCategoryRepository(db)
	before := func(id uuid.UUID) domain.Category {
		t.Helper()
		var c domain.Category
		if err := db.First(&c, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		return c
	}

	if err := repo.UpdateByTenant(context.Background(), &domain.Category{ID: own, TenantID: tenantA, Name: "Matematika Dasar"}); err != nil {
		t.Fatalf("own update: %v", err)
	}
	if got := before(own); got.Name != "Matematika Dasar" {
		t.Fatalf("own name=%q", got.Name)
	}
	if got := before(sibling); got.Name != "Fisika" {
		t.Fatalf("sibling changed: %q", got.Name)
	}
	if got := before(foreign); got.Name != "Matematika seberang" {
		t.Fatalf("foreign changed: %q", got.Name)
	}
	var class domain.Class
	if err := db.First(&class, "id = ?", classID).Error; err != nil {
		t.Fatal(err)
	}
	if class.CategoryID != own {
		t.Fatalf("class moved to %s", class.CategoryID)
	}

	// A write whose scope does not match any row is a no-op (RecordNotFound),
	// never a cross-tenant update.
	if err := repo.UpdateByTenant(context.Background(), &domain.Category{ID: foreign, TenantID: tenantA, Name: "Milik orang"}); err == nil {
		t.Fatal("cross-tenant write succeeded")
	} else if !strings.Contains(err.Error(), "record not found") {
		t.Fatalf("cross-tenant write error=%v", err)
	}
	if got := before(foreign); got.Name != "Matematika seberang" {
		t.Fatalf("foreign changed after blocked write: %q", got.Name)
	}

	if err := repo.UpdateByTenant(context.Background(), &domain.Category{ID: uuid.New(), TenantID: tenantA, Name: "Hantu"}); err == nil {
		t.Fatal("missing-row write succeeded")
	} else if !strings.Contains(err.Error(), "record not found") {
		t.Fatalf("missing-row write error=%v", err)
	}
}
