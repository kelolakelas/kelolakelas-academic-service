package usecase_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Exercise cmd/migrate's actual directory/driver, including an upgrade from the
// deployed payment-method version that previously collided with retry storage.
func TestClassCreationMigrationUpgrade(t *testing.T) {
	dsn := os.Getenv("KEL196_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL196_TEST_DSN not set")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	baseSQL, _ := base.DB()
	defer baseSQL.Close()
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing_retry_table_%t", existing), func(t *testing.T) {
			schema := "kel204_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if err := base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
				t.Fatal(err)
			}
			defer base.Exec("DROP SCHEMA " + schema + " CASCADE")
			db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema+",public"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			defer sqlDB.Close()
			driver, err := migratepg.WithInstance(sqlDB, &migratepg.Config{SchemaName: schema})
			if err != nil {
				t.Fatal(err)
			}
			m, err := migrate.NewWithDatabaseInstance("file://../../migrations", schema, driver)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Migrate(1790900000); err != nil {
				t.Fatal(err)
			}
			tenant, teacher := uuid.New(), uuid.New()
			identity := creationIdentity{tenant: tenant, teacher: teacher}
			u := usecase.NewClassCreationUsecase(repository.NewTransactionManager(db), repository.NewCategoryRepository(db), repository.NewClassRepository(db), repository.NewClassTeacherRepository(db), repository.NewScheduleRepository(db), repository.NewSessionRepository(db), identity, usecase.ClassCreationOptions{Membership: identity, Store: repository.NewClassCreationStore(db)})
			fresh := func() *domain.CreateClassWithCategoryRequest {
				return &domain.CreateClassWithCategoryRequest{Category: &domain.CreateCategoryRequest{Name: "Migration category"}, Class: domain.CreateClassPayload{Name: "Migration class", Type: "group", Price: 100}, TeacherIDs: []uuid.UUID{teacher}, IdempotencyKey: uuid.NewString(), Schedules: []domain.ScheduleItemRequest{{TutorID: &teacher, Capacity: 3, DayOfWeek: 1, StartTime: "09:00", EndTime: "10:00"}}}
			}
			req := fresh()
			if existing {
				data, err := os.ReadFile("../../migrations/00001790900001_class_creation_requests.up.sql")
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Exec(string(data)).Error; err != nil {
					t.Fatal(err)
				}
				if _, err := u.CreateClassWithCategory(context.Background(), tenant, req); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := u.CreateClassWithCategory(context.Background(), tenant, req); err == nil {
					t.Fatal("missing retry table accepted")
				}
				var count int64
				db.Table("classes").Count(&count)
				if count != 0 {
					t.Fatal("failed save left a class")
				}
			}
			if err := m.Up(); err != nil {
				t.Fatal(err)
			}
			version, dirty, err := m.Version()
			if err != nil || dirty || version != 1790900005 {
				t.Fatalf("version=%d dirty=%t err=%v", version, dirty, err)
			}
			first, err := u.CreateClassWithCategory(context.Background(), tenant, req)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := u.CreateClassWithCategory(context.Background(), tenant, req)
			if err != nil || replay.Class.ID != first.Class.ID {
				t.Fatalf("replay changed after upgrade: %v", err)
			}
			private := fresh()
			private.Class.Type = "private"
			private.Schedules = nil
			if _, err := u.CreateClassWithCategory(context.Background(), tenant, private); err != nil {
				t.Fatal(err)
			}
			var count int64
			db.Table("classes").Count(&count)
			if count != 2 {
				t.Fatalf("wanted group + private, got %d", count)
			}
			// Down-versioning application code must retain committed keys and responses.
			if err := m.Steps(-1); err != nil {
				t.Fatal(err)
			}
			if err := m.Up(); err != nil {
				t.Fatal(err)
			}
			again, err := u.CreateClassWithCategory(context.Background(), tenant, req)
			if err != nil || again.Class.ID != first.Class.ID {
				t.Fatalf("rollback lost retry history: %v", err)
			}
		})
	}
}
