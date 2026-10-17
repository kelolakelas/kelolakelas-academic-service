package usecase_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const deletionTestSecret = "kel206-synthetic-fixture-secret"

// Use a private schema without public-table fallback, even if the supplied
// development database already has tables with the same names.
func deletionDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("KEL196_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL196_TEST_DSN not set")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("test database unavailable")
	}
	sqlBase, _ := base.DB()
	t.Cleanup(func() { sqlBase.Close() })
	schema := "kel206_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { base.Exec("DROP SCHEMA " + schema + " CASCADE") })
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	// The baseline is the repository's idempotent current-schema initializer.
	raw, err := os.ReadFile("../../migrations/00000000000000_init_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(raw)).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

type deletionFixture struct {
	db           *gorm.DB
	router       *gin.Engine
	tenant, role uuid.UUID
	token        string
}

func newDeletionFixture(t *testing.T) *deletionFixture {
	db := deletionDB(t)
	tenant, role := uuid.New(), uuid.New()
	id := creationIdentity{tenant: tenant}
	tx := repository.NewTransactionManager(db)
	classes := repository.NewClassRepository(db)
	categories := repository.NewCategoryRepository(db)
	schedules := repository.NewScheduleRepository(db)
	sessions := repository.NewSessionRepository(db)
	enrollments := repository.NewEnrollmentRepository(db)
	cu := usecase.NewClassUsecase(classes, schedules, sessions, enrollments, id, tx, categories)
	su := usecase.NewScheduleUsecase(tx, classes, schedules, sessions, enrollments)
	cat := usecase.NewCategoryUsecase(categories, id, tx)
	lists := handler.NewListHandler(cat, cu, su)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": uuid.NewString(), "tenant_id": tenant.String(), "role_id": role.String(), "member_id": uuid.NewString(), "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(deletionTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	group := r.Group("/api/v1", middleware.AuthMiddleware(deletionTestSecret))
	group.DELETE("/classes/:id", middleware.RequirePermission(id, "class:delete"), handler.NewClassHandler(cu, nil).Delete)
	group.GET("/classes", lists.ListClasses)
	group.GET("/schedules", lists.ListSchedules)
	group.GET("/categories", lists.ListCategories)
	return &deletionFixture{db, r, tenant, role, token}
}
func (f *deletionFixture) request(method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	f.router.ServeHTTP(w, req)
	return w
}
func (f *deletionFixture) seed(t *testing.T, name string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	class, schedule, category := uuid.New(), uuid.New(), uuid.New()
	for _, stmt := range []struct {
		q    string
		args []any
	}{
		{"INSERT INTO categories(id,tenant_id,name) VALUES(?,?,'Deletion category')", []any{category, f.tenant}},
		{"INSERT INTO classes(id,tenant_id,category_id,name,type,price) VALUES(?,?,?,?,'group',100000)", []any{class, f.tenant, category, name}},
		{"INSERT INTO class_schedules(id,class_id,day_of_week,start_time,end_time,capacity) VALUES(?,?,1,'09:00','10:00',10)", []any{schedule, class}},
	} {
		if err := f.db.Exec(stmt.q, stmt.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	return class, schedule
}
func assertDeletionState(t *testing.T, f *deletionFixture, class, schedule uuid.UUID, deleted bool) {
	t.Helper()
	for _, row := range []struct {
		table string
		id    uuid.UUID
	}{{"classes", class}, {"class_schedules", schedule}} {
		var got bool
		result := f.db.Table(row.table).Select("deleted_at IS NOT NULL").Where("id = ?", row.id).Scan(&got)
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		if result.RowsAffected != 1 {
			t.Fatalf("%s history row disappeared", row.table)
		}
		if got != deleted {
			t.Fatalf("%s deleted=%v want %v", row.table, got, deleted)
		}
	}
}
func TestClassDeletionPostgres(t *testing.T) {
	f := newDeletionFixture(t)
	t.Run("cascade hides schedules on every page and class search while retaining history", func(t *testing.T) {
		class, slot := f.seed(t, "Delete lifecycle target")
		f.seed(t, "Keep lifecycle control")
		past, future, completed := uuid.New(), uuid.New(), uuid.New()
		today := time.Now().UTC().Truncate(24 * time.Hour)
		for _, row := range []struct {
			id     uuid.UUID
			date   time.Time
			status string
		}{{past, today.AddDate(0, 0, -2), "completed"}, {future, today.AddDate(0, 0, 2), "scheduled"}, {completed, today.AddDate(0, 0, 3), "completed"}} {
			if err := f.db.Exec("INSERT INTO class_sessions(id,class_id,schedule_id,tutor_id,session_date,start_time,end_time,status) VALUES(?,?,?,?,?,'09:00','10:00',?)", row.id, class, slot, uuid.New(), row.date, row.status).Error; err != nil {
				t.Fatal(err)
			}
		}
		if w := f.request("DELETE", "/api/v1/classes/"+class.String()); w.Code != 200 {
			t.Fatalf("delete: %d %s", w.Code, w.Body)
		}
		assertDeletionState(t, f, class, slot, true)
		for _, path := range []string{"/classes?page_size=1", "/classes?page=2&page_size=1", "/classes?search=Delete%20lifecycle", "/schedules?page_size=1", "/schedules?page=2&page_size=1", "/schedules?search=Delete%20lifecycle"} {
			w := f.request("GET", "/api/v1"+path)
			if w.Code != 200 {
				t.Fatalf("list %s: %d %s", path, w.Code, w.Body)
			}
			var result struct {
				Status string `json:"status"`
				Data   struct {
					Items      []json.RawMessage `json:"items"`
					Pagination struct {
						TotalItems int `json:"total_items"`
					} `json:"pagination"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Status != "success" {
				t.Fatalf("invalid listing %s", w.Body)
			}
			if path == "/classes?page_size=1" || path == "/schedules?page_size=1" {
				if len(result.Data.Items) != 1 || result.Data.Pagination.TotalItems != 1 {
					t.Fatalf("live control lost or incorrect count: %s", w.Body)
				}
			}
			if strings.Contains(w.Body.String(), class.String()) || strings.Contains(w.Body.String(), slot.String()) {
				t.Fatalf("deleted record leaked in %s: %s", path, w.Body)
			}
		}
		for _, row := range []struct {
			id     uuid.UUID
			status string
		}{{past, "completed"}, {future, "cancelled"}, {completed, "completed"}} {
			var status string
			f.db.Table("class_sessions").Select("status").Where("id = ?", row.id).Scan(&status)
			if status != row.status {
				t.Fatalf("session %s status=%s want %s", row.id, status, row.status)
			}
		}
		if w := f.request("DELETE", "/api/v1/classes/"+class.String()); w.Code != 404 {
			t.Fatalf("repeated delete=%d", w.Code)
		}
	})
	t.Run("active enrollment and foreign tenant preserve both class and schedules", func(t *testing.T) {
		class, slot := f.seed(t, "Protected lifecycle target")
		student := uuid.New()
		if err := f.db.Exec("INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'Synthetic')", student, uuid.New()).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.db.Exec("INSERT INTO enrollments(id,tenant_id,student_id,class_id,schedule_id,status,billing_cycle) VALUES(?,?,?,?,?,'active','monthly')", uuid.New(), f.tenant, student, class, slot).Error; err != nil {
			t.Fatal(err)
		}
		if w := f.request("DELETE", "/api/v1/classes/"+class.String()); w.Code != 409 {
			t.Fatalf("active conflict=%d %s", w.Code, w.Body)
		}
		assertDeletionState(t, f, class, slot, false)
		if err := f.db.Exec("UPDATE classes SET tenant_id=? WHERE id=?", uuid.New(), class).Error; err != nil {
			t.Fatal(err)
		}
		if w := f.request("DELETE", "/api/v1/classes/"+class.String()); w.Code != 403 {
			t.Fatalf("foreign delete=%d", w.Code)
		}
		assertDeletionState(t, f, class, slot, false)
	})
	t.Run("failure after schedule deletion rolls back all rows", func(t *testing.T) {
		class, slot := f.seed(t, "Rollback lifecycle target")
		future := uuid.New()
		if err := f.db.Exec("INSERT INTO class_sessions(id,class_id,schedule_id,tutor_id,session_date,start_time,end_time,status) VALUES(?,?,?,?,CURRENT_DATE+2,'09:00','10:00','scheduled')", future, class, slot, uuid.New()).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.db.Exec(`CREATE FUNCTION reject_class_delete() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'injected class delete failure'; END $$ LANGUAGE plpgsql; CREATE TRIGGER reject_class_delete BEFORE UPDATE OF deleted_at ON classes FOR EACH ROW EXECUTE FUNCTION reject_class_delete()`).Error; err != nil {
			t.Fatal(err)
		}
		defer f.db.Exec("DROP TRIGGER reject_class_delete ON classes; DROP FUNCTION reject_class_delete()")
		if w := f.request("DELETE", "/api/v1/classes/"+class.String()); w.Code != 500 {
			t.Fatalf("injected failure=%d %s", w.Code, w.Body)
		}
		assertDeletionState(t, f, class, slot, false)
		var status string
		if err := f.db.Table("class_sessions").Select("status").Where("id = ?", future).Scan(&status).Error; err != nil {
			t.Fatal(err)
		}
		if status != "scheduled" {
			t.Fatalf("future cancellation escaped rollback: %s", status)
		}
	})

	t.Run("concurrent deletes leave one consistent committed lifecycle", func(t *testing.T) {
		class, slot := f.seed(t, "Concurrent lifecycle target")
		var wg sync.WaitGroup
		codes := make(chan int, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); codes <- f.request("DELETE", "/api/v1/classes/"+class.String()).Code }()
		}
		wg.Wait()
		close(codes)
		for code := range codes {
			if code != 200 && code != 404 {
				t.Fatalf("concurrent delete=%d", code)
			}
		}
		assertDeletionState(t, f, class, slot, true)
	})
}
