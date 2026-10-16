package usecase_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type creationIdentity struct {
	tenant, teacher uuid.UUID
	denied          string
	unavailable     bool
}

func (s creationIdentity) ValidateTenantStatus(context.Context, string) (bool, string, error) {
	return true, "", nil
}
func (s creationIdentity) GetTenantPublicInfo(context.Context, []string) (map[string]grpcclient.TenantPublicInfo, error) {
	return nil, nil
}
func (s creationIdentity) Close() error { return nil }
func (s creationIdentity) CheckActiveMember(_ context.Context, tenant, member string) (bool, error) {
	if s.unavailable {
		return false, fmt.Errorf("unavailable")
	}
	return tenant == s.tenant.String() && member == s.teacher.String(), nil
}
func (s creationIdentity) CheckPermission(_ context.Context, tenant, role, member, permission string) (bool, error) {
	if s.unavailable {
		return false, fmt.Errorf("unavailable")
	}
	return tenant == s.tenant.String() && permission != s.denied, nil
}

// KEL196_TEST_DSN must name a disposable test database. Each run owns a unique schema.
func TestClassCreationPostgres(t *testing.T) {
	dsn := os.Getenv("KEL196_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL196_TEST_DSN not set")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := "kel196_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	for _, file := range []string{"00000000000000_init_schema.up.sql", "00001790812223_add_rescheduled_from_to_class_sessions.up.sql", "00001790900000_class_creation_requests.up.sql"} {
		sql, err := os.ReadFile("../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(sql)).Error; err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
	tenant, teacher, category := uuid.New(), uuid.New(), uuid.New()
	identity := creationIdentity{tenant: tenant, teacher: teacher}
	must := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	must("INSERT INTO categories(id,tenant_id,name) VALUES(?,?,'Existing')", category, tenant)
	counts := func() string {
		t.Helper()
		values := []string{}
		for _, table := range []string{"categories", "classes", "class_teachers", "class_schedules", "class_sessions", "class_creation_requests"} {
			var n int64
			if err := db.Table(table).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			values = append(values, fmt.Sprint(n))
		}
		return strings.Join(values, ",")
	}
	makeUsecase := func(id creationIdentity) usecase.ClassCreationUsecase {
		return usecase.NewClassCreationUsecase(repository.NewTransactionManager(db), repository.NewCategoryRepository(db), repository.NewClassRepository(db), repository.NewClassTeacherRepository(db), repository.NewScheduleRepository(db), repository.NewSessionRepository(db), id, usecase.ClassCreationOptions{Membership: id, Store: repository.NewClassCreationStore(db)})
	}
	fresh := func() *domain.CreateClassWithCategoryRequest {
		from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		return &domain.CreateClassWithCategoryRequest{Category: &domain.CreateCategoryRequest{Name: "New category"}, Class: domain.CreateClassPayload{Name: "Atomic class", Type: "group", Price: 100}, TeacherIDs: []uuid.UUID{teacher}, IdempotencyKey: uuid.NewString(), Schedules: []domain.ScheduleItemRequest{{TutorID: &teacher, Capacity: 8, DayOfWeek: 1, StartTime: "09:00", EndTime: "10:00:00", ValidFrom: &from}, {Capacity: 3, DayOfWeek: 3, StartTime: "10:00", EndTime: "11:00", ValidFrom: &from}}}
	}
	ctx := context.Background()
	u := makeUsecase(identity)
	t.Run("rollback each persisted stage and retry", func(t *testing.T) {
		for _, table := range []string{"categories", "classes", "class_teachers", "class_schedules", "class_sessions", "class_creation_requests"} {
			t.Run(table, func(t *testing.T) {
				before := counts()
				req := fresh()
				must("CREATE OR REPLACE FUNCTION fail_creation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected creation failure'; END $$")
				event := "INSERT"
				if table == "class_creation_requests" {
					event = "UPDATE"
				}
				must("CREATE TRIGGER fail_creation BEFORE " + event + " ON " + table + " FOR EACH ROW EXECUTE FUNCTION fail_creation()")
				_, err := u.CreateClassWithCategory(ctx, tenant, req)
				must("DROP TRIGGER fail_creation ON " + table)
				if err == nil {
					t.Fatal("expected failure")
				}
				if after := counts(); after != before {
					t.Fatalf("partial writes: %s -> %s", before, after)
				}
				if _, err := u.CreateClassWithCategory(ctx, tenant, req); err != nil {
					t.Fatalf("retry after rollback: %v", err)
				}
			})
		}
	})
	t.Run("concurrent retry one aggregate and conflict", func(t *testing.T) {
		req := fresh()
		before := counts()
		var wg sync.WaitGroup
		ids := make(chan uuid.UUID, 12)
		errs := make(chan error, 12)
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := u.CreateClassWithCategory(ctx, tenant, req)
				if err != nil {
					errs <- err
					return
				}
				ids <- res.Class.ID
			}()
		}
		wg.Wait()
		close(ids)
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		var id uuid.UUID
		for got := range ids {
			if id != uuid.Nil && id != got {
				t.Fatal("duplicate class")
			}
			id = got
		}
		if id == uuid.Nil {
			t.Fatal("no response")
		}
		var n int64
		db.Table("classes").Where("id = ?", id).Count(&n)
		if n != 1 {
			t.Fatal("class missing")
		}
		replay, err := u.CreateClassWithCategory(ctx, tenant, req)
		if err != nil || len(replay.Schedules) != 2 || len(replay.Sessions) != 9 {
			t.Fatalf("replay=%+v err=%v, before=%s", replay, err, before)
		}
		stable := counts()
		req.Class.Price++
		if _, err := u.CreateClassWithCategory(ctx, tenant, req); err != repository.ErrClassCreationKeyConflict {
			t.Fatalf("conflict %v", err)
		}
		if counts() != stable {
			t.Fatal("conflict wrote data")
		}
	})
	t.Run("existing category legacy and private", func(t *testing.T) {
		req := fresh()
		req.Category = nil
		req.CategoryID = category
		req.Schedules = nil
		req.IdempotencyKey = ""
		req.Class.Type = "private"
		res, err := u.CreateClassWithCategory(ctx, tenant, req)
		if err != nil || res.Class.CategoryID != category || len(res.Schedules) != 0 {
			t.Fatalf("legacy private: %+v %v", res, err)
		}
		req = fresh()
		req.Class.Type = "private"
		req.Schedules = nil
		private, err := u.CreateClassWithCategory(ctx, tenant, req)
		if err != nil || private.Category == nil || len(private.Schedules) != 0 {
			t.Fatalf("new private: %+v %v", private, err)
		}
		req = fresh()
		req.Category = nil
		req.CategoryID = category
		res, err = u.CreateClassWithCategory(ctx, tenant, req)
		if err != nil || len(res.Schedules) != 2 {
			t.Fatalf("existing group: %+v %v", res, err)
		}
		var name string
		db.Raw("SELECT name FROM categories WHERE id = ?", category).Scan(&name)
		if name != "Existing" {
			t.Fatal("existing category mutated")
		}
	})
	t.Run("invalid or foreign references never write", func(t *testing.T) {
		cases := []func(*domain.CreateClassWithCategoryRequest){func(r *domain.CreateClassWithCategoryRequest) { r.CategoryID = category }, func(r *domain.CreateClassWithCategoryRequest) { r.Category = nil; r.CategoryID = uuid.New() }, func(r *domain.CreateClassWithCategoryRequest) { r.IdempotencyKey = "" }, func(r *domain.CreateClassWithCategoryRequest) { r.Class.Type = "private" }, func(r *domain.CreateClassWithCategoryRequest) { r.Schedules[1].StartTime = "12:00" }, func(r *domain.CreateClassWithCategoryRequest) { r.TeacherIDs = []uuid.UUID{uuid.New()} }, func(r *domain.CreateClassWithCategoryRequest) { r.Schedules[0].TutorID = ptrUUID(uuid.New()) }, func(r *domain.CreateClassWithCategoryRequest) { r.TeacherIDs = append(r.TeacherIDs, teacher) }, func(r *domain.CreateClassWithCategoryRequest) { r.Schedules[0].Capacity = 0 }}
		foreign := uuid.New()
		must("INSERT INTO categories(id,tenant_id,name) VALUES(?,?,'Foreign')", foreign, uuid.New())
		cases = append(cases, func(r *domain.CreateClassWithCategoryRequest) { r.Category = nil; r.CategoryID = foreign })
		for i, change := range cases {
			req := fresh()
			change(req)
			before := counts()
			if _, err := u.CreateClassWithCategory(ctx, tenant, req); err == nil {
				t.Fatalf("case %d accepted", i)
			}
			if counts() != before {
				t.Fatalf("case %d partial write", i)
			}
		}
		before := counts()
		bad := identity
		bad.unavailable = true
		if _, err := makeUsecase(bad).CreateClassWithCategory(ctx, tenant, fresh()); err != usecase.ErrSubstituteTutorUnavailable {
			t.Fatal(err)
		}
		if counts() != before {
			t.Fatal("membership outage wrote data")
		}
	})
	gin.SetMode(gin.TestMode)
	secret := "kel196-disposable-test-secret"
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": uuid.NewString(), "tenant_id": tenant.String(), "role_id": uuid.NewString(), "member_id": uuid.NewString(), "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(secret))
	router := func(id creationIdentity) *gin.Engine {
		r := gin.New()
		r.Use(middleware.AuthMiddleware(secret))
		h := handler.NewClassHandler(nil, makeUsecase(id)).WithCreationPermissions(id)
		r.POST("/api/v1/classes/with-category", middleware.RequirePermission(id, "class:create"), h.CreateWithCategory)
		return r
	}
	send := func(client *http.Client, url string, req *domain.CreateClassWithCategoryRequest) int {
		t.Helper()
		data, _ := json.Marshal(req)
		request, _ := http.NewRequest("POST", url, bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Idempotency-Key", req.IdempotencyKey)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode >= 500 {
			t.Logf("HTTP %d %s", response.StatusCode, body)
		}
		return response.StatusCode
	}
	t.Run("HTTP permission and validation", func(t *testing.T) {
		for _, permission := range []string{"class:create", "category:create", "schedule:create"} {
			bad := identity
			bad.denied = permission
			server := httptest.NewServer(router(bad))
			before := counts()
			if status := send(server.Client(), server.URL+"/api/v1/classes/with-category", fresh()); status != 403 {
				t.Fatalf("%s: %d", permission, status)
			}
			server.Close()
			if counts() != before {
				t.Fatal("denied write")
			}
		}
		server := httptest.NewServer(router(identity))
		defer server.Close()
		req := fresh()
		if status := send(server.Client(), server.URL+"/api/v1/classes/with-category", req); status != 201 {
			t.Fatal(status)
		}
		req.Class.Price++
		if status := send(server.Client(), server.URL+"/api/v1/classes/with-category", req); status != 409 {
			t.Fatal(status)
		}
		req = fresh()
		req.Schedules[0].DayOfWeek = 8
		if status := send(server.Client(), server.URL+"/api/v1/classes/with-category", req); status != 400 {
			t.Fatal(status)
		}
	})
	t.Run("existing gateway executable forwards aggregate and retry key", func(t *testing.T) {
		binary := os.Getenv("KEL196_GATEWAY_BINARY")
		if binary == "" {
			t.Skip("optional gateway executable not supplied; covered in local acceptance run")
		}
		academic := httptest.NewServer(router(identity))
		defer academic.Close()
		auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		defer auth.Close()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		command := exec.Command(binary)
		command.Dir = t.TempDir()
		command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "JWT_SECRET=" + secret, fmt.Sprintf("PORT=%d", port), "APP_URL=http://localhost:3000", "IDENTITY_SERVICE_URL=" + auth.URL, "ACADEMIC_SERVICE_URL=" + academic.URL, "BILLING_SERVICE_URL=" + auth.URL, "REDIS_HOST=127.0.0.1", "REDIS_PORT=55497", "GIN_MODE=release"}
		var output bytes.Buffer
		command.Stdout = &output
		command.Stderr = &output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { command.Process.Signal(os.Interrupt); command.Wait() }()
		url := fmt.Sprintf("http://127.0.0.1:%d", port)
		ready := false
		for i := 0; i < 200; i++ {
			res, err := http.Get(url + "/health")
			if err == nil {
				res.Body.Close()
				ready = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !ready {
			t.Fatalf("gateway did not start")
		}
		req := fresh()
		for i := 0; i < 2; i++ {
			if status := send(http.DefaultClient, url+"/api/v1/classes/with-category", req); status != 201 {
				t.Fatal(status)
			}
		}
	})
}
func ptrUUID(id uuid.UUID) *uuid.UUID { return &id }
