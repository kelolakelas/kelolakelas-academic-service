package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

// KEL-134 acceptance over real HTTP requests against a local PostgreSQL:
// legacy schedule_id + date writes keep working (AC4), session-addressed
// single + bulk writes cover a reschedule replacement (AC1, AC2), a foreign
// tutor is refused with 403 and a foreign enrollment with a validation error
// (AC3), and repeating the bulk request does not duplicate rows (AC2).
//
// Run with KEL134_TEST_DSN against a disposable PostgreSQL database, e.g.
// docker run -d --rm --name kel134-db-test -p 127.0.0.1::5432 postgres:16-alpine
//
//	KEL134_TEST_DSN="postgres://postgres:***@127.0.0.1:<port>/postgres?sslmode=disable" \
//	  go test -count=1 -race -run TestKEL134API ./cmd/server/
type kel134APIFixture struct {
	db       *gorm.DB
	engine   *gin.Engine
	tenant   uuid.UUID
	tutor    uuid.UUID
	stranger uuid.UUID
	classID  uuid.UUID
	schedID  uuid.UUID
	session  uuid.UUID
	replace  uuid.UUID
	enrolls  []uuid.UUID
	foreign  uuid.UUID
}

func newKEL134APIFixture(t *testing.T) *kel134APIFixture {
	t.Helper()
	dsn := os.Getenv("KEL134_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL134_TEST_DSN not set")
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
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock(134, 2)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(134, 2)") })
	for _, file := range []string{
		"../../migrations/00000000000000_init_schema.up.sql",
		"../../migrations/00001790812223_add_rescheduled_from_to_class_sessions.up.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(schema)).Error; err != nil {
			t.Fatal(err)
		}
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	f := &kel134APIFixture{tenant: uuid.New(), tutor: uuid.New(), stranger: uuid.New(), classID: uuid.New(), schedID: uuid.New()}
	categoryID := uuid.New()
	exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryID, f.tenant, "KEL-134 API")
	exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", f.classID, f.tenant, categoryID, "KEL-134 API group", "group", 100, true, "open")
	exec("INSERT INTO class_schedules (id, class_id, capacity, day_of_week, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)", f.schedID, f.classID, 10, 1, "10:00:00", "11:00:00")
	f.session = uuid.New()
	origin := uuid.New()
	// Origin session marked rescheduled so the fallback path also resolves it;
	// the replacement links to it through the KEL-134 column.
	exec("INSERT INTO class_sessions (id, class_id, schedule_id, tutor_id, session_date, start_time, end_time, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", origin, f.classID, f.schedID, f.tutor, "2026-09-28", "10:00:00", "11:00:00", "rescheduled")
	exec("INSERT INTO class_sessions (id, class_id, schedule_id, tutor_id, session_date, start_time, end_time, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", f.session, f.classID, f.schedID, f.tutor, "2026-10-05", "10:00:00", "11:00:00", "scheduled")
	f.replace = uuid.New()
	exec("INSERT INTO class_sessions (id, class_id, schedule_id, tutor_id, session_date, start_time, end_time, status, rescheduled_from_session_id) VALUES (?, ?, NULL, ?, ?, ?, ?, ?, ?)", f.replace, f.classID, f.tutor, "2026-10-12", "18:00:00", "19:00:00", "scheduled", origin)
	for range 2 {
		studentID := uuid.New()
		exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", studentID, uuid.New(), "KEL-134 API student")
		enrollID := uuid.New()
		exec("INSERT INTO enrollments (id, tenant_id, student_id, class_id, schedule_id, status, billing_cycle, idempotency_key, payment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", enrollID, f.tenant, studentID, f.classID, f.schedID, "active", "monthly", uuid.NewString(), "paid")
		f.enrolls = append(f.enrolls, enrollID)
	}
	otherStudent := uuid.New()
	exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", otherStudent, uuid.New(), "KEL-134 foreign student")
	f.foreign = uuid.New()
	exec("INSERT INTO enrollments (id, tenant_id, student_id, class_id, status, billing_cycle, idempotency_key, payment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", f.foreign, f.tenant, otherStudent, f.classID, "active", "monthly", uuid.NewString(), "paid")

	db2 := db.Session(&gorm.Session{})
	attendanceUC := usecase.NewAttendanceUsecase(
		repository.NewAttendanceRepository(db2),
		repository.NewSessionRepository(db2),
		repository.NewEnrollmentRepository(db2),
	)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("tenant_id", f.tenant.String())
		c.Next()
	})
	api := engine.Group("/api/v1")
	// Member identity goes straight in the context: permission gating is
	// covered by the route matrix test, here the API behaviour is exercised.
	h := handler.NewAttendanceHandler(attendanceUC)
	api.POST("/attendance", func(c *gin.Context) {
		c.Set("member_id", c.GetHeader("X-KEL134-Member"))
		h.Create(c)
	})
	api.POST("/attendance/by-session", func(c *gin.Context) {
		c.Set("member_id", c.GetHeader("X-KEL134-Member"))
		h.CreateBySession(c)
	})
	api.POST("/attendance/bulk", func(c *gin.Context) {
		c.Set("member_id", c.GetHeader("X-KEL134-Member"))
		h.CreateBulk(c)
	})
	api.GET("/attendance/by-session", h.GetBySession)
	f.db, f.engine = db, engine
	return f
}

func (f *kel134APIFixture) post(t *testing.T, member uuid.UUID, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-KEL134-Member", member.String())
	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, req)
	return rec
}

func (f *kel134APIFixture) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestKEL134APIAcceptance(t *testing.T) {
	f := newKEL134APIFixture(t)

	// AC4: the legacy schedule_id + date form keeps working.
	legacyBody := fmt.Sprintf(`{"enrollment_id":%q,"schedule_id":%q,"date":"2026-10-05","status":"present"}`, f.enrolls[0], f.schedID)
	if rec := f.post(t, f.tutor, "/api/v1/attendance", legacyBody); rec.Code != http.StatusCreated {
		t.Fatalf("legacy create status=%d body=%s, want 201", rec.Code, rec.Body.String())
	}

	// AC3: a tutor who does not teach the session is refused with 403.
	if rec := f.post(t, f.stranger, "/api/v1/attendance/by-session", fmt.Sprintf(`{"enrollment_id":%q,"session_id":%q,"status":"present"}`, f.enrolls[0], f.session)); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign tutor status=%d body=%s, want 403", rec.Code, rec.Body.String())
	}

	// AC3: an enrollment outside the session's cohort is a validation error (400).
	if rec := f.post(t, f.tutor, "/api/v1/attendance/by-session", fmt.Sprintf(`{"enrollment_id":%q,"session_id":%q,"status":"present"}`, f.foreign, f.session)); rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign enrollment status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}

	// AC1: single write addressed at the reschedule replacement, then read back.
	singleBody := fmt.Sprintf(`{"enrollment_id":%q,"session_id":%q,"status":"present"}`, f.enrolls[0], f.replace)
	if rec := f.post(t, f.tutor, "/api/v1/attendance/by-session", singleBody); rec.Code != http.StatusCreated {
		t.Fatalf("replacement single status=%d body=%s, want 201", rec.Code, rec.Body.String())
	}
	readPath := fmt.Sprintf("/api/v1/attendance/by-session?session_id=%s&enrollment_id=%s", f.replace, f.enrolls[0])
	if rec := f.get(t, readPath); rec.Code != http.StatusOK {
		t.Fatalf("replacement read status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}

	// AC2: one bulk request for the replacement session, repeated without duplicates.
	bulkBody := fmt.Sprintf(`{"session_id":%q,"items":[{"enrollment_id":%q,"status":"present"},{"enrollment_id":%q,"status":"absent"}]}`, f.replace, f.enrolls[0], f.enrolls[1])
	first := f.post(t, f.tutor, "/api/v1/attendance/bulk", bulkBody)
	if first.Code != http.StatusCreated {
		t.Fatalf("bulk status=%d body=%s, want 201", first.Code, first.Body.String())
	}
	var payload struct {
		Data struct {
			Attendances []struct {
				EnrollmentID uuid.UUID `json:"enrollment_id"`
			} `json:"attendances"`
		} `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &payload); err != nil {
		t.Fatalf("bulk response: %v", err)
	}
	if len(payload.Data.Attendances) != 2 {
		t.Fatalf("bulk rows=%d, want 2", len(payload.Data.Attendances))
	}
	second := f.post(t, f.tutor, "/api/v1/attendance/bulk", bulkBody)
	if second.Code != http.StatusCreated {
		t.Fatalf("repeat bulk status=%d body=%s, want 201", second.Code, second.Body.String())
	}
	var count int64
	if err := f.db.Table("attendances").Where("session_id = ?", f.replace).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("stored replacement rows=%d, want 2 (repeat must not duplicate)", count)
	}

	// AC1 (update): reschedule rows update through the id-addressed endpoint path
	// — covered at the usecase level; here confirm the updated row reads back.
	if rec := f.post(t, f.tutor, "/api/v1/attendance/bulk", fmt.Sprintf(`{"session_id":%q,"items":[{"enrollment_id":%q,"status":"late"},{"enrollment_id":%q,"status":"absent"}]}`, f.replace, f.enrolls[0], f.enrolls[1])); rec.Code != http.StatusCreated {
		t.Fatalf("bulk update status=%d body=%s, want 201", rec.Code, rec.Body.String())
	}
	read := f.get(t, readPath)
	var readPayload struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &readPayload); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if readPayload.Data.Status != "late" {
		t.Fatalf("updated status=%q, want late", readPayload.Data.Status)
	}
}
