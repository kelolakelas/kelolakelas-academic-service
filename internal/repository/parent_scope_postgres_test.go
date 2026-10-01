package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// KEL-140 authorization matrix against real PostgreSQL: an owning parent reads
// the parent's own rows across two tenants; another parent reads nothing; a
// foreign enrollment/student filter narrows instead of widening; a tenant
// member keeps the tenant-scoped path; and another tenant's member reads
// nothing of the first tenant.
//
// Run with KEL140_TEST_DSN against a disposable PostgreSQL database, e.g.
// docker run -d --rm --name kel140-db-test -p 127.0.0.1::5432 postgres:16-alpine
//
//	KEL140_TEST_DSN="postgres://postgres:***@127.0.0.1:<port>/postgres?sslmode=disable" \
//	  go test -count=1 -race -run TestParentScope ./internal/repository/
type parentScopeFixture struct {
	db        *gorm.DB
	tenantA   uuid.UUID
	tenantB   uuid.UUID
	owner     uuid.UUID
	other     uuid.UUID
	enrollA   uuid.UUID
	enrollB   uuid.UUID
	studentA  uuid.UUID
	scheduleA uuid.UUID
	sessionA  uuid.UUID
	attendA   uuid.UUID
	reportA   uuid.UUID
	otherSess uuid.UUID
	otherAtt  uuid.UUID
	otherRep  uuid.UUID
}

func newParentScopeFixture(t *testing.T) *parentScopeFixture {
	t.Helper()
	dsn := os.Getenv("KEL140_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL140_TEST_DSN not set")
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
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock(140, 1)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(140, 1)") })
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

	exec := func(stmt string, args ...any) {
		t.Helper()
		if err := db.Exec(stmt, args...).Error; err != nil {
			t.Fatal(err)
		}
	}

	f := &parentScopeFixture{
		db:      db,
		tenantA: uuid.New(), tenantB: uuid.New(),
		owner: uuid.New(), other: uuid.New(),
		enrollA: uuid.New(), enrollB: uuid.New(), studentA: uuid.New(),
		scheduleA: uuid.New(), sessionA: uuid.New(),
		attendA: uuid.New(), reportA: uuid.New(),
		otherSess: uuid.New(), otherAtt: uuid.New(), otherRep: uuid.New(),
	}
	otherStudent, otherEnroll, otherSched := uuid.New(), uuid.New(), uuid.New()
	tutor := uuid.New()

	// Tenant A: category, class, schedule, session, owner student + enrollment,
	// one attendance row and one report on that enrollment.
	categoryA := uuid.New()
	exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryA, f.tenantA, "KEL-140 A")
	classA := uuid.New()
	exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		classA, f.tenantA, categoryA, "KEL-140 group A", "group", 100, true, "open")
	exec("INSERT INTO class_schedules (id, class_id, capacity, day_of_week, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)",
		f.scheduleA, classA, 10, 1, "10:00:00", "11:00:00")
	exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", f.studentA, f.owner, "Owner child")
	exec("INSERT INTO enrollments (id, tenant_id, student_id, class_id, schedule_id, status, billing_cycle, idempotency_key, payment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		f.enrollA, f.tenantA, f.studentA, classA, f.scheduleA, "active", "monthly", uuid.NewString(), "paid")
	exec("INSERT INTO class_sessions (id, class_id, schedule_id, tutor_id, session_date, start_time, end_time, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		f.sessionA, classA, f.scheduleA, tutor, "2026-10-05", "10:00:00", "11:00:00", "scheduled")
	exec("INSERT INTO attendances (id, enrollment_id, session_id, date, status) VALUES (?, ?, ?, ?, ?)",
		f.attendA, f.enrollA, f.sessionA, "2026-10-05", "present")
	exec("INSERT INTO reports (id, tenant_id, enrollment_id, reporter_id, title) VALUES (?, ?, ?, ?, ?)",
		f.reportA, f.tenantA, f.enrollA, tutor, "Owner progress")

	// Tenant B: a second tenant where the same owner parent holds another
	// child, so the cross-tenant read (AC 1) has something to find.
	categoryB := uuid.New()
	exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryB, f.tenantB, "KEL-140 B")
	classB := uuid.New()
	exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		classB, f.tenantB, categoryB, "KEL-140 group B", "group", 100, true, "open")
	schedB := uuid.New()
	exec("INSERT INTO class_schedules (id, class_id, capacity, day_of_week, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)",
		schedB, classB, 10, 2, "10:00:00", "11:00:00")
	studentB := uuid.New()
	exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", studentB, f.owner, "Owner second child")
	exec("INSERT INTO enrollments (id, tenant_id, student_id, class_id, schedule_id, status, billing_cycle, idempotency_key, payment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		f.enrollB, f.tenantB, studentB, classB, schedB, "active", "monthly", uuid.NewString(), "paid")
	sessB := uuid.New()
	exec("INSERT INTO class_sessions (id, class_id, schedule_id, tutor_id, session_date, start_time, end_time, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		sessB, classB, schedB, tutor, "2026-10-06", "10:00:00", "11:00:00", "scheduled")

	// Tenant A again: another parent's child, session, attendance, and report.
	// The owner must never see these (AC 2), even when naming their ids.
	exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", otherStudent, f.other, "Other child")
	exec("INSERT INTO class_schedules (id, class_id, capacity, day_of_week, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)",
		otherSched, classA, 10, 3, "12:00:00", "13:00:00")
	exec("INSERT INTO enrollments (id, tenant_id, student_id, class_id, schedule_id, status, billing_cycle, idempotency_key, payment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		otherEnroll, f.tenantA, otherStudent, classA, otherSched, "active", "monthly", uuid.NewString(), "paid")
	exec("INSERT INTO class_sessions (id, class_id, schedule_id, tutor_id, session_date, start_time, end_time, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		f.otherSess, classA, otherSched, tutor, "2026-10-05", "12:00:00", "13:00:00", "scheduled")
	exec("INSERT INTO attendances (id, enrollment_id, session_id, date, status) VALUES (?, ?, ?, ?, ?)",
		f.otherAtt, otherEnroll, f.otherSess, "2026-10-05", "present")
	exec("INSERT INTO reports (id, tenant_id, enrollment_id, reporter_id, title) VALUES (?, ?, ?, ?, ?)",
		f.otherRep, f.tenantA, otherEnroll, tutor, "Other progress")
	return f
}

// The owning parent reads the parent's own rows across both tenants (AC 1):
// two sessions, one attendance row, one report, and the schedule-A attendee
// list holding only the owner's enrollment.
func TestParentScopeOwnerReadsAcrossTenants(t *testing.T) {
	f := newParentScopeFixture(t)
	ctx := context.Background()
	sessions := NewSessionRepository(f.db)
	attendances := NewAttendanceRepository(f.db)
	reports := NewReportRepository(f.db)
	enrollments := NewEnrollmentRepository(f.db)

	items, total, err := sessions.ListSessionsForParent(ctx, f.owner, domain.SessionQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("sessions total=%d len=%d, want 2 (one per tenant)", total, len(items))
	}
	if _, err := sessions.GetSessionForParent(ctx, f.owner, f.sessionA); err != nil {
		t.Fatalf("own session: %v", err)
	}

	rows, total, err := attendances.ListForParent(ctx, f.owner, domain.AttendanceQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("attendance list: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ID != f.attendA {
		t.Fatalf("attendance total=%d, want exactly the owner's row", total)
	}
	if _, err := attendances.GetForParent(ctx, f.owner, f.attendA); err != nil {
		t.Fatalf("own attendance: %v", err)
	}

	notes, total, err := reports.ListForParent(ctx, f.owner, domain.ReportQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("report list: %v", err)
	}
	if total != 1 || len(notes) != 1 || notes[0].ID != f.reportA {
		t.Fatalf("report total=%d, want exactly the owner's row", total)
	}
	if _, err := reports.GetForParent(ctx, f.owner, f.reportA); err != nil {
		t.Fatalf("own report: %v", err)
	}

	cohort, err := enrollments.GetActiveByScheduleIDForParent(ctx, f.owner, f.scheduleA)
	if err != nil {
		t.Fatalf("attendees: %v", err)
	}
	if len(cohort) != 1 || cohort[0].ID != f.enrollA {
		t.Fatalf("attendees=%d rows, want only the owner's enrollment", len(cohort))
	}
}

// Another parent reads none of the owner's rows (AC 2): every list is empty
// and every single-row lookup answers not-found, never forbidden, so ids do
// not leak across parents.
func TestParentScopeOtherParentReadsNothing(t *testing.T) {
	f := newParentScopeFixture(t)
	ctx := context.Background()
	sessions := NewSessionRepository(f.db)
	attendances := NewAttendanceRepository(f.db)
	reports := NewReportRepository(f.db)
	enrollments := NewEnrollmentRepository(f.db)

	if _, total, err := sessions.ListSessionsForParent(ctx, f.other, domain.SessionQuery{Page: 1, PageSize: 20}); err != nil || total != 1 {
		t.Fatalf("other parent sessions total=%d err=%v, want exactly 1 (their own)", total, err)
	}
	if _, err := sessions.GetSessionForParent(ctx, f.other, f.sessionA); err == nil {
		t.Fatal("other parent resolved the owner's session")
	}
	if _, err := attendances.GetForParent(ctx, f.other, f.attendA); err == nil {
		t.Fatal("other parent resolved the owner's attendance")
	}
	if _, total, err := attendances.ListForParent(ctx, f.other, domain.AttendanceQuery{Page: 1, PageSize: 20}); err != nil || total != 1 {
		t.Fatalf("other parent attendance total=%d err=%v, want exactly 1 (their own)", total, err)
	}
	if _, err := reports.GetForParent(ctx, f.other, f.reportA); err == nil {
		t.Fatal("other parent resolved the owner's report")
	}
	if _, total, err := reports.ListForParent(ctx, f.other, domain.ReportQuery{Page: 1, PageSize: 20}); err != nil || total != 1 {
		t.Fatalf("other parent report total=%d err=%v, want exactly 1 (their own)", total, err)
	}
	if cohort, err := enrollments.GetActiveByScheduleIDForParent(ctx, f.other, f.scheduleA); err != nil || len(cohort) != 0 {
		t.Fatalf("other parent attendees=%d err=%v, want 0 on the owner's schedule", len(cohort), err)
	}
}

// A parent naming another parent's enrollment or student id (AC 2), or
// passing a foreign tenant's scope, only narrows the parent's own rows: the
// foreign filter matches nothing the parent owns, so the lists come back
// empty instead of widening.
func TestParentScopeForeignFiltersNarrowToNothing(t *testing.T) {
	f := newParentScopeFixture(t)
	ctx := context.Background()
	attendances := NewAttendanceRepository(f.db)
	reports := NewReportRepository(f.db)
	sessions := NewSessionRepository(f.db)

	var foreignEnroll uuid.UUID
	var foreignEnrollStr string
	if err := f.db.Raw("SELECT enrollment_id FROM attendances WHERE id = ?", f.otherAtt).Scan(&foreignEnrollStr).Error; err != nil {
		t.Fatal(err)
	}
	foreignEnroll, err := uuid.Parse(foreignEnrollStr)
	if err != nil {
		t.Fatal(err)
	}
	rows, total, err := attendances.ListForParent(ctx, f.owner, domain.AttendanceQuery{Page: 1, PageSize: 20, EnrollmentID: &foreignEnroll})
	if err != nil {
		t.Fatalf("attendance list: %v", err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("foreign enrollment filter total=%d, want 0", total)
	}
	notes, total, err := reports.ListForParent(ctx, f.owner, domain.ReportQuery{Page: 1, PageSize: 20, EnrollmentID: &foreignEnroll})
	if err != nil {
		t.Fatalf("report list: %v", err)
	}
	if total != 0 || len(notes) != 0 {
		t.Fatalf("foreign enrollment report total=%d, want 0", total)
	}
	var foreignStudent uuid.UUID
	var foreignStudentStr string
	if err := f.db.Raw("SELECT student_id FROM enrollments WHERE id = ?", foreignEnroll).Scan(&foreignStudentStr).Error; err != nil {
		t.Fatal(err)
	}
	foreignStudent, err = uuid.Parse(foreignStudentStr)
	if err != nil {
		t.Fatal(err)
	}
	rows, total, err = attendances.ListForParent(ctx, f.owner, domain.AttendanceQuery{Page: 1, PageSize: 20, StudentID: &foreignStudent})
	if err != nil {
		t.Fatalf("attendance list: %v", err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("foreign student filter total=%d, want 0", total)
	}
	items, total, err := sessions.ListSessionsForParent(ctx, f.owner, domain.SessionQuery{Page: 1, PageSize: 20, EnrollmentID: &foreignEnroll})
	if err != nil {
		t.Fatalf("session list: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("foreign enrollment session total=%d, want 0", total)
	}
}

// The tenant-scoped paths are unchanged (AC 4): a member of tenant A reads
// tenant A's rows (both parents' rows, as before), while a caller scoping to
// tenant B reads none of tenant A's rows.
func TestParentScopeTenantPathsUnchanged(t *testing.T) {
	f := newParentScopeFixture(t)
	ctx := context.Background()
	sessions := NewSessionRepository(f.db)
	attendances := NewAttendanceRepository(f.db)
	reports := NewReportRepository(f.db)

	items, total, err := sessions.ListByTenant(ctx, f.tenantA, domain.SessionQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("tenant sessions: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("tenant A sessions total=%d, want 2 (both parents' rows, unchanged)", total)
	}
	if _, err := sessions.GetByIDForTenant(ctx, f.tenantA, f.sessionA); err != nil {
		t.Fatalf("tenant session: %v", err)
	}
	rows, total, err := attendances.List(ctx, f.tenantA, domain.AttendanceQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("tenant attendance: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("tenant A attendance total=%d, want 2 (unchanged)", total)
	}
	notes, total, err := reports.List(ctx, f.tenantA, domain.ReportQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("tenant reports: %v", err)
	}
	if total != 2 || len(notes) != 2 {
		t.Fatalf("tenant A reports total=%d, want 2 (unchanged)", total)
	}
	bItems, bTotal, err := sessions.ListByTenant(ctx, f.tenantB, domain.SessionQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("tenant B sessions: %v", err)
	}
	if bTotal != 1 || len(bItems) != 1 {
		t.Fatalf("tenant B sessions total=%d, want 1 (no leak from tenant A)", bTotal)
	}
	if _, err := attendances.GetByIDForTenant(ctx, f.tenantB, f.attendA); err == nil {
		t.Fatal("tenant B scope resolved tenant A's attendance")
	}
	if _, err := reports.GetByIDForTenant(ctx, f.tenantB, f.reportA); err == nil {
		t.Fatal("tenant B scope resolved tenant A's report")
	}
}
