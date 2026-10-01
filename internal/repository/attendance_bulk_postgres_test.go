package repository

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// KEL-134 bulk upsert against real PostgreSQL: one bulk request writes the
// whole session atomically; two concurrent identical bulk requests leave one
// row per enrollment (ON CONFLICT serializes on uq_attendances_session_enrollment);
// and a caller from another tenant writes nothing (tenant scope in the write
// statement's subselect, not only in the guard read).
//
// Run with KEL134_TEST_DSN against a disposable PostgreSQL database, e.g.
// docker run -d --rm --name kel134-db-test -p 127.0.0.1::5432 postgres:16-alpine
//
//	KEL134_TEST_DSN="postgres://postgres:***@127.0.0.1:<port>/postgres?sslmode=disable" \
//	  go test -count=1 -race -run TestBulk ./internal/repository/
type bulkAttendanceFixture struct {
	db       *gorm.DB
	repo     *attendanceRepository
	tenantID uuid.UUID
	classID  uuid.UUID
	schedID  uuid.UUID
	session  uuid.UUID
	enrolls  []uuid.UUID
}

func newBulkAttendanceFixture(t *testing.T) *bulkAttendanceFixture {
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
	// Go runs packages concurrently; serialize shared-schema setup and fixtures.
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock(134, 1)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(134, 1)") })
	schema, err := os.ReadFile("../../migrations/00000000000000_init_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/00001790812223_add_rescheduled_from_to_class_sessions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	f := &bulkAttendanceFixture{db: db, repo: NewAttendanceRepository(db).(*attendanceRepository), tenantID: uuid.New(), classID: uuid.New(), schedID: uuid.New()}
	categoryID := uuid.New()
	if err := db.Exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryID, f.tenantID, "KEL-134").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", f.classID, f.tenantID, categoryID, "KEL-134 group", "group", 100, true, "open").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO class_schedules (id, class_id, capacity, day_of_week, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)", f.schedID, f.classID, 10, 1, "10:00:00", "11:00:00").Error; err != nil {
		t.Fatal(err)
	}
	tutorID := uuid.New()
	f.session = uuid.New()
	if err := db.Exec("INSERT INTO class_sessions (id, class_id, schedule_id, tutor_id, session_date, start_time, end_time, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", f.session, f.classID, f.schedID, tutorID, "2026-10-05", "10:00:00", "11:00:00", "scheduled").Error; err != nil {
		t.Fatal(err)
	}
	for range 3 {
		studentID := uuid.New()
		if err := db.Exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", studentID, uuid.New(), "KEL-134 student").Error; err != nil {
			t.Fatal(err)
		}
		enrollID := uuid.New()
		key := uuid.NewString()
		if err := db.Exec("INSERT INTO enrollments (id, tenant_id, student_id, class_id, schedule_id, status, billing_cycle, idempotency_key, payment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", enrollID, f.tenantID, studentID, f.classID, f.schedID, "active", "monthly", key, "paid").Error; err != nil {
			t.Fatal(err)
		}
		f.enrolls = append(f.enrolls, enrollID)
	}
	return f
}

func (f *bulkAttendanceFixture) items(status string) []domain.BulkAttendanceItem {
	out := make([]domain.BulkAttendanceItem, 0, len(f.enrolls))
	for _, id := range f.enrolls {
		out = append(out, domain.BulkAttendanceItem{EnrollmentID: id, Status: status})
	}
	return out
}

func (f *bulkAttendanceFixture) rowCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := f.db.Model(&domain.Attendance{}).Where("session_id = ?", f.session).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestBulkUpsertIsIdempotent(t *testing.T) {
	f := newBulkAttendanceFixture(t)
	ctx := context.Background()

	first, err := f.repo.UpsertBulk(ctx, f.tenantID, f.session, f.items("present"))
	if err != nil {
		t.Fatalf("first bulk: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("first bulk rows = %d, want 3", len(first))
	}

	second, err := f.repo.UpsertBulk(ctx, f.tenantID, f.session, f.items("late"))
	if err != nil {
		t.Fatalf("repeat bulk: %v", err)
	}
	if len(second) != 3 {
		t.Fatalf("repeat bulk rows = %d, want 3", len(second))
	}
	for _, row := range second {
		if row.Status != "late" {
			t.Fatalf("row %s status = %q, want late (repeat updates)", row.EnrollmentID, row.Status)
		}
	}
	if got := f.rowCount(t); got != 3 {
		t.Fatalf("stored rows = %d, want 3 (no duplicates)", got)
	}

	// Rows carry the session's own date so legacy date-scoped reads keep working.
	var date time.Time
	if err := f.db.Model(&domain.Attendance{}).Where("session_id = ?", f.session).Select("date").Limit(1).Scan(&date).Error; err != nil {
		t.Fatal(err)
	}
	if date.Format("2006-01-02") != "2026-10-05" {
		t.Fatalf("row date = %s, want 2026-10-05", date.Format("2006-01-02"))
	}
}

func TestBulkUpsertConcurrentIdenticalRequests(t *testing.T) {
	f := newBulkAttendanceFixture(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = f.repo.UpsertBulk(ctx, f.tenantID, f.session, f.items("present"))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent bulk %d: %v", i, err)
		}
	}
	if got := f.rowCount(t); got != 3 {
		t.Fatalf("stored rows after concurrent bulk = %d, want 3", got)
	}
}

func TestBulkUpsertOtherTenantWritesNothing(t *testing.T) {
	f := newBulkAttendanceFixture(t)
	ctx := context.Background()

	other := uuid.New()
	rows, err := f.repo.UpsertBulk(ctx, other, f.session, f.items("present"))
	if err == nil {
		t.Fatalf("cross-tenant bulk unexpectedly wrote %d rows", len(rows))
	}
	if got := f.rowCount(t); got != 0 {
		t.Fatalf("stored rows after cross-tenant bulk = %d, want 0", got)
	}
}
