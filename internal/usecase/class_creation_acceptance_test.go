package usecase_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Opt-in fixture for the web action/browser tests. Real PostgreSQL, migration
// driver, academic handlers/usecases and unmodified gateway; identity responses
// are controlled test doubles. Never exposes credentials in the exported config.
func TestClassCreationAcceptanceStack(t *testing.T) {
	path := os.Getenv("KEL204_STACK_CONFIG")
	if path == "" {
		t.Skip("KEL204_STACK_CONFIG not set")
	}
	dsn := os.Getenv("KEL196_TEST_DSN")
	if dsn == "" {
		t.Fatal("KEL196_TEST_DSN must name a disposable acceptance database")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("fixture database unavailable")
	}
	baseSQL, _ := base.DB()
	defer baseSQL.Close()
	schema := "kel204_browser_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	defer base.Exec("DROP SCHEMA " + schema + " CASCADE")
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema+",public"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	driver, err := migratepg.WithInstance(sqlDB, &migratepg.Config{SchemaName: schema})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://../../migrations", schema, driver)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Migrate(1790900000); err != nil {
		t.Fatal(err)
	}
	tenant, teacher, category, role := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	id := creationIdentity{tenant: tenant, teacher: teacher}
	if err := db.Exec("INSERT INTO categories(id,tenant_id,name) VALUES(?,?,'Existing')", category, tenant).Error; err != nil {
		t.Fatal(err)
	}
	permissions := []string{"class:read", "class:create", "class:update", "class:delete", "category:create", "category:read", "schedule:read", "schedule:create", "member:read"}
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/internal/session/check":
			w.WriteHeader(204)
		case "/api/v1/members/me/membership":
			json.NewEncoder(w).Encode(gin.H{"status": "success", "data": gin.H{"member_id": teacher, "role_id": role, "role_name": "Creator", "permissions": permissions}})
		case "/api/v1/members":
			json.NewEncoder(w).Encode(gin.H{"status": "success", "data": gin.H{"items": []any{gin.H{"id": teacher, "user_id": uuid.New(), "email": "teacher@example.test", "first_name": "Teacher", "last_name": "One", "status": "active", "role": gin.H{"id": role, "name": "Teacher", "is_system_role": true, "permissions": []any{}}}}, "pagination": gin.H{"page": 1, "total_pages": 1}}})
		default:
			w.WriteHeader(204)
		}
	}))
	defer identity.Close()
	secret := "kel204-isolated-acceptance-secret"
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": uuid.NewString(), "email": "owner@example.test", "tenant_id": tenant.String(), "role_id": role.String(), "member_id": teacher.String(), "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	u := usecase.NewClassCreationUsecase(repository.NewTransactionManager(db), repository.NewCategoryRepository(db), repository.NewClassRepository(db), repository.NewClassTeacherRepository(db), repository.NewScheduleRepository(db), repository.NewSessionRepository(db), id, usecase.ClassCreationOptions{Membership: id, Store: repository.NewClassCreationStore(db)})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestLog())
	done := make(chan struct{}, 1)
	r.POST("/test/shutdown", func(c *gin.Context) { done <- struct{}{}; c.Status(204) })
	r.POST("/test/migrate", func(c *gin.Context) {
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			c.Status(500)
			return
		}
		c.Status(204)
	})
	r.GET("/test/counts", func(c *gin.Context) {
		counts := gin.H{}
		for _, table := range []string{"categories", "classes", "class_teachers", "class_schedules", "class_sessions"} {
			var n int64
			db.Table(table).Count(&n)
			counts[table] = n
		}
		c.JSON(200, counts)
	})
	r.POST("/test/failure", func(c *gin.Context) {
		var err error
		if c.Query("enable") == "true" {
			err = db.Exec("CREATE OR REPLACE FUNCTION fail_creation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected acceptance failure'; END $$; CREATE TRIGGER fail_creation BEFORE INSERT ON class_sessions FOR EACH ROW EXECUTE FUNCTION fail_creation()").Error
		} else {
			err = db.Exec("DROP TRIGGER IF EXISTS fail_creation ON class_sessions").Error
		}
		if err != nil {
			c.Status(500)
			return
		}
		c.Status(204)
	})
	protected := r.Group("/api/v1")
	protected.Use(middleware.AuthMiddleware(secret))
	protected.POST("/classes/with-category", middleware.RequirePermission(id, "class:create"), handler.NewClassHandler(nil, u).WithCreationPermissions(id).CreateWithCategory)
	for _, table := range []string{"categories", "classes", "schedules"} {
		resource := table
		protected.GET("/"+resource, func(c *gin.Context) {
			sqlTable := resource
			if resource == "schedules" {
				sqlTable = "class_schedules"
			}
			var rows []map[string]any
			db.Table(sqlTable).Find(&rows)
			if rows == nil {
				rows = []map[string]any{}
			}
			c.JSON(200, gin.H{"status": "success", "data": gin.H{"items": rows, "pagination": gin.H{"page": 1, "total_pages": 1}}})
		})
	}
	academic := httptest.NewServer(r)
	defer academic.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	command := exec.Command(os.Getenv("KEL196_GATEWAY_BINARY"))
	command.Dir = t.TempDir()
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "JWT_SECRET=" + secret, "PORT=" + strconv.Itoa(port), "APP_URL=http://localhost:3000", "IDENTITY_SERVICE_URL=" + identity.URL, "ACADEMIC_SERVICE_URL=" + academic.URL, "BILLING_SERVICE_URL=" + identity.URL, "REDIS_HOST=127.0.0.1", "REDIS_PORT=55497", "GIN_MODE=release"}
	var logs bytes.Buffer
	command.Stdout = &logs
	command.Stderr = &logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { command.Process.Signal(os.Interrupt); command.Wait() }()
	gateway := "http://127.0.0.1:" + strconv.Itoa(port)
	ready := false
	for n := 0; n < 100; n++ {
		res, err := http.Get(gateway + "/health")
		if err == nil {
			res.Body.Close()
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("gateway not ready")
	}
	config := map[string]string{"token": token, "gateway": gateway, "academic": academic.URL, "category": category.String(), "teacher": teacher.String(), "tenant": tenant.String()}
	data, _ := json.Marshal(config)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	fmt.Println("KEL204 isolated browser fixture ready")
	select {
	case <-done:
	case <-time.After(30 * time.Minute):
		t.Fatal("acceptance fixture timed out")
	}
}
