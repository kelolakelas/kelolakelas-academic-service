package repository

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestClassListPreloadsTenantCategory(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			tenant, categoryID := uuid.New(), uuid.New()
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "classes" WHERE tenant_id = $1 AND "classes"."deleted_at" IS NULL`)).WithArgs(tenant).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "classes" WHERE tenant_id = $1 AND "classes"."deleted_at" IS NULL ORDER BY created_at DESC LIMIT $2`)).WithArgs(tenant, 20).WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "category_id", "name"}).AddRow(uuid.New(), tenant, categoryID, "Algebra").AddRow(uuid.New(), tenant, categoryID, "Geometry"))
			categories := sqlmock.NewRows([]string{"id", "tenant_id", "name"})
			if !missing {
				categories.AddRow(categoryID, tenant, "Matematika")
			}
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1 AND tenant_id = $2 AND "categories"."deleted_at" IS NULL`)).WithArgs(categoryID, tenant).WillReturnRows(categories)
			rows, total, err := NewClassRepository(db).ListByTenant(context.Background(), tenant, domain.ListQuery{Page: 1, PageSize: 20})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 || total != 2 {
				t.Fatalf("rows=%d total=%d", len(rows), total)
			}
			for _, row := range rows {
				if missing && row.Category != nil {
					t.Fatalf("unexpected category: %+v", row.Category)
				}
				if !missing && (row.Category == nil || row.Category.Name != "Matematika") {
					t.Fatalf("missing category: %+v", row)
				}
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
