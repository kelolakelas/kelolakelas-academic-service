package usecase

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func classReadPG(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("KEL227_TEST_DSN")
	if dsn == "" {
		t.Skip("set KEL227_TEST_DSN to a disposable PostgreSQL database")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { admin.Close() })
	schema := "kel227_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	cfg.RuntimeParams["search_path"] = schema + ",public"
	cfg.RuntimeParams["timezone"] = "UTC"
	pool := stdlib.OpenDB(*cfg)
	pool.SetMaxOpenConns(12)
	t.Cleanup(func() { pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("../../migrations/*.up.sql")
	sort.Strings(files)
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sql := strings.ReplaceAll(string(raw), "CREATE EXTENSION IF NOT EXISTS pgcrypto;", "")
		if err = db.Exec(sql).Error; err != nil {
			t.Fatalf("migration%s: %v", file, err)
		}
	}
	return db
}

type classReadQueries struct {
	logger.Interface
	count atomic.Int64
}

func (l *classReadQueries) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	query, _ := fc()
	if strings.HasPrefix(query, "SELECT") {
		l.count.Add(1)
	}
}

func TestKEL227ClassConfigurationPostgres(t *testing.T) {
	db := classReadPG(t)
	tenant, foreign, category, target, other, teacher := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	must := func(query string, args ...any) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	must("INSERT INTO categories(id,tenant_id,name) VALUES(?,?,'Fixture category')", category, tenant)
	must("INSERT INTO classes(id,tenant_id,category_id,name,type,price,is_published,enrollment_status,created_at) VALUES(?,?,?,'Unpublished late target','group',100,false,'open',now()-interval '1 year'),(?,?,?,'Foreign','group',100,false,'open',now())", target, tenant, category, other, foreign, category)
	must("INSERT INTO classes(id,tenant_id,category_id,name,type,price,is_published,enrollment_status) SELECT gen_random_uuid(),?,?,'Unrelated '||i,'group',100,false,'open' FROM generate_series(1,205) i", tenant, category)
	must("INSERT INTO class_teachers(class_id,teacher_id) VALUES(?,?)", target, teacher)
	must("INSERT INTO class_schedules(id,class_id,day_of_week,start_time,end_time,capacity,valid_from,valid_until) SELECT gen_random_uuid(),?,1,'09:00:00','10:00:00',5,CURRENT_DATE+(i-60),CASE WHEN i<60 THEN CURRENT_DATE+(i-59) ELSE NULL END FROM generate_series(1,125) i", target)
	must("INSERT INTO class_schedules(id,class_id,day_of_week,start_time,end_time,capacity) SELECT gen_random_uuid(),?,2,'10:00:00','11:00:00',5 FROM generate_series(1,205)", other)
	counter := &classReadQueries{Interface: logger.Default.LogMode(logger.Silent)}
	store := repository.NewClassConfigurationReader(db.Session(&gorm.Session{Logger: counter}))
	u := &ClassConfigurationRead{Store: store}
	ctx := context.Background()
	got, err := u.Class(ctx, tenant, target)
	if err != nil || got.ID != target || got.IsPublished || len(got.TeacherIDs) != 1 || got.TeacherIDs[0] != teacher || got.Category == nil {
		t.Fatalf("saved class: %+v %v", got, err)
	}
	if count := counter.count.Load(); count != 3 {
		t.Fatalf("class queries=%d, want3 independent of tenant size", count)
	}
	counter.count.Store(0)
	first, err := u.Schedules(ctx, tenant, target, domain.ListQuery{Page: 1, PageSize: 100})
	if err != nil || len(first.Items) != 100 || first.Pagination.TotalItems != 125 || first.Pagination.TotalPages != 2 {
		t.Fatalf("first page: %+v %v", first, err)
	}
	if count := counter.count.Load(); count != 3 {
		t.Fatalf("page queries=%d, want3", count)
	}
	second, err := u.Schedules(ctx, tenant, target, domain.ListQuery{Page: 2, PageSize: 100})
	if err != nil || len(second.Items) != 25 {
		t.Fatal("later page missing", err)
	}
	seen := map[uuid.UUID]bool{}
	for _, item := range append(first.Items, second.Items...) {
		if item.ClassID != target || seen[item.ID] || item.Enrollment != nil || item.Class != nil {
			t.Fatal("scope, duplicate or unnecessary preload")
		}
		seen[item.ID] = true
	}
	repeat, err := u.Schedules(ctx, tenant, target, domain.ListQuery{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range first.Items {
		if repeat.Items[i].ID != item.ID {
			t.Fatal("unstable pagination order")
		}
	}
	for _, id := range []uuid.UUID{other, uuid.New()} {
		if _, err := u.Class(ctx, tenant, id); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatal("foreign/missing class exposed", err)
		}
		if _, err := u.Schedules(ctx, tenant, id, domain.ListQuery{Page: 1, PageSize: 100}); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatal("foreign/missing schedule scope accepted", err)
		}
	}
	must("UPDATE classes SET price=200 WHERE id=?", target)
	fresh, err := u.Class(ctx, tenant, target)
	if err != nil || fresh.Price != 200 {
		t.Fatal("stale after save", err)
	}
	must("UPDATE class_schedules SET deleted_at=now() WHERE id=?", first.Items[0].ID)
	deleted, err := u.Schedules(ctx, tenant, target, domain.ListQuery{Page: 1, PageSize: 100})
	if err != nil || deleted.Pagination.TotalItems != 124 {
		t.Fatal("deleted schedule visible", err)
	}
	must("UPDATE classes SET deleted_at=now() WHERE id=?", target)
	if _, err := u.Class(ctx, tenant, target); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("deleted class visible")
	}
	if _, err := u.Schedules(ctx, tenant, target, domain.ListQuery{Page: 1, PageSize: 100}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("deleted class schedules visible")
	}
}
