package repository

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPrivateEnrollmentScheduleProjectionPostgres(t *testing.T) {
	dsn := os.Getenv("KEL222_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL222_TEST_DSN not set")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*cfg)
	defer admin.Close()
	schema := "kel222_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	cfg.RuntimeParams["search_path"] = schema + ",public"
	pool := stdlib.OpenDB(*cfg)
	defer pool.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../migrations/00000000000000_init_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(strings.ReplaceAll(string(raw), "CREATE EXTENSION IF NOT EXISTS pgcrypto;", "")).Error; err != nil {
		t.Fatal(err)
	}
	indexSQL, err := os.ReadFile("../../migrations/00001790900005_private_enrollment_schedule_lookup.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(string(indexSQL)).Error; err != nil {
		t.Fatal(err)
	}
	var indexCount int64
	if err = db.Raw("SELECT count(*) FROM pg_indexes WHERE schemaname = ? AND indexname = 'idx_class_schedules_live_enrollment'", schema).Scan(&indexCount).Error; err != nil || indexCount != 1 {
		t.Fatalf("lookup index missing: %d %v", indexCount, err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	tenant, otherTenant, parent, otherParent := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	category, class, otherClass := uuid.New(), uuid.New(), uuid.New()
	must(db.Exec("INSERT INTO categories(id,tenant_id,name) VALUES (?,?,'test')", category, tenant).Error)
	must(db.Exec("INSERT INTO classes(id,tenant_id,category_id,name,type,price) VALUES (?,?,?,'private','private',100),(?,?,?,'foreign','private',100)", class, tenant, category, otherClass, otherTenant, category).Error)
	enroll := func(owner, par, cls uuid.UUID) *domain.Enrollment {
		student := uuid.New()
		must(db.Exec("INSERT INTO students(id,parent_id,first_name) VALUES (?,?,'fixture')", student, par).Error)
		e := &domain.Enrollment{ID: uuid.New(), TenantID: owner, StudentID: student, ClassID: cls, Status: "pending", BillingCycle: "monthly"}
		must(db.Create(e).Error)
		return e
	}
	a := enroll(tenant, parent, class)
	b := enroll(tenant, parent, class)
	foreign := enroll(otherTenant, otherParent, otherClass)
	slot := func(e *domain.Enrollment, cls uuid.UUID, day int, from time.Time) *domain.ClassSchedule {
		s := &domain.ClassSchedule{ID: uuid.New(), ClassID: cls, EnrollmentID: &e.ID, Capacity: 1, DayOfWeek: day, StartTime: "10:00:00", EndTime: "11:00:00", ValidFrom: &from}
		must(db.Create(s).Error)
		return s
	}
	past := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	future := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	history := slot(a, class, 2, past)
	must(db.Model(history).Update("valid_until", past.AddDate(0, 1, 0)).Error)
	slot(a, class, 2, future)
	slot(a, class, 4, past)
	removed := slot(a, class, 7, past)
	must(db.Delete(removed).Error)
	slot(foreign, otherClass, 1, past)
	// Even a corrupt enrollment->schedule class association must not leak.
	slot(a, otherClass, 6, past)
	for i := 0; i < 105; i++ {
		slot(b, class, 3, past.AddDate(0, 0, i))
	}
	queries := 0
	must(db.Callback().Query().After("gorm:query").Register("kel222_count", func(*gorm.DB) { queries++ }))
	repo := NewEnrollmentRepository(db)
	ctx := context.Background()
	one, err := repo.GetByIDForAccess(ctx, &tenant, nil, a.ID)
	must(err)
	if !one.PrivateSchedulesLoaded || len(one.PrivateSchedules) != 3 {
		t.Fatalf("slots=%+v", one.PrivateSchedules)
	}
	if one.PrivateSchedules[0].ID != history.ID || one.PrivateSchedules[1].ValidFrom.Year() != 2030 || one.PrivateSchedules[2].DayOfWeek != 4 {
		t.Fatal("history/future order lost")
	}
	large, err := repo.GetByIDForAccess(ctx, nil, &parent, b.ID)
	must(err)
	if len(large.PrivateSchedules) != 105 {
		t.Fatalf("truncated slots=%d", len(large.PrivateSchedules))
	}
	queries = 0
	items, _, err := repo.List(ctx, &tenant, nil, domain.EnrollmentQuery{Page: 1, PageSize: 1})
	must(err)
	smallCount := queries
	queries = 0
	items, _, err = repo.List(ctx, &tenant, nil, domain.EnrollmentQuery{Page: 1, PageSize: 20})
	must(err)
	if len(items) != 2 || queries != smallCount {
		t.Fatalf("N+1: items=%d small=%d batch=%d", len(items), smallCount, queries)
	}
	for _, scope := range []struct{ tenant, parent *uuid.UUID }{{&otherTenant, nil}, {nil, &otherParent}} {
		if _, err = repo.GetByIDForAccess(ctx, scope.tenant, scope.parent, a.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("foreign scope: %v", err)
		}
	}
	must(db.Model(a).Update("status", "active").Error)
	active, err := repo.GetByIDForAccess(ctx, &tenant, nil, a.ID)
	must(err)
	if len(active.PrivateSchedules) != 3 {
		t.Fatal("payment/activation hid slots")
	}
	must(db.Delete(&domain.Class{}, "id = ?", class).Error)
	deleted, err := repo.GetByIDForAccess(ctx, &tenant, nil, a.ID)
	must(err)
	if deleted.PrivateSchedulesLoaded || len(deleted.PrivateSchedules) != 0 {
		t.Fatal("deleted class exposed slots")
	}
	// A projection read failure must fail the response, never become a successful [].
	must(db.Exec("DROP TABLE class_schedules CASCADE").Error)
	if _, err = repo.GetByIDForAccess(ctx, &otherTenant, nil, foreign.ID); err == nil {
		t.Fatal("schedule read failure became empty")
	}
}
