package repository

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// KEL-184: report list/detail (tenant and parent scoped) carries the real
// student/class identities on the preloaded enrollment, while foreign
// tenant/parent lookups stay fail-closed (404 / scoped empty, no leak).
//
// Run with KEL184_TEST_DSN against a disposable PostgreSQL database, e.g.
// docker run -d --rm --name kel184-db-test -p 127.0.0.1::5432 postgres:16-alpine
//
//	KEL184_TEST_DSN="postgres://postgres:***@127.0.0.1:<port>/postgres?sslmode=disable" \
//	  go test -count=1 -race -run TestReportEnrollmentIdentities ./internal/repository/
type reportIdentityFixture struct {
	db         *gorm.DB
	tenantA    uuid.UUID
	tenantB    uuid.UUID
	owner      uuid.UUID
	other      uuid.UUID
	enrollA    uuid.UUID
	studentA   uuid.UUID
	studentB   uuid.UUID
	classAName string
	classBName string
	reportA    uuid.UUID
	reportB    uuid.UUID
	otherRep   uuid.UUID
}

func newReportIdentityFixture(t *testing.T) *reportIdentityFixture {
	t.Helper()
	dsn := os.Getenv("KEL184_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL184_TEST_DSN not set")
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
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock(184, 1)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(184, 1)") })
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

	exec := func(stmt string, args ...any) {
		t.Helper()
		if err := db.Exec(stmt, args...).Error; err != nil {
			t.Fatal(err)
		}
	}

	f := &reportIdentityFixture{
		db:         db,
		tenantA:    uuid.New(),
		tenantB:    uuid.New(),
		owner:      uuid.New(),
		other:      uuid.New(),
		enrollA:    uuid.New(),
		studentA:   uuid.New(),
		studentB:   uuid.New(),
		classAName: "KEL-184 Matematika A",
		classBName: "KEL-184 Fisika B",
		reportA:    uuid.New(),
		reportB:    uuid.New(),
		otherRep:   uuid.New(),
	}
	tutor := uuid.New()

	// Tenant A: class A + owner's child + enrollment + report.
	categoryA := uuid.New()
	classA := uuid.New()
	exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryA, f.tenantA, "KEL-184 A")
	exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		classA, f.tenantA, categoryA, f.classAName, "group", 100, true, "open")
	exec("INSERT INTO students (id, parent_id, first_name, last_name) VALUES (?, ?, ?, ?)", f.studentA, f.owner, "Siswa", "Kesatu")
	exec("INSERT INTO enrollments (id, tenant_id, student_id, class_id, status, billing_cycle, idempotency_key, payment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		f.enrollA, f.tenantA, f.studentA, classA, "active", "monthly", uuid.NewString(), "paid")
	exec("INSERT INTO reports (id, tenant_id, enrollment_id, reporter_id, title) VALUES (?, ?, ?, ?, ?)",
		f.reportA, f.tenantA, f.enrollA, tutor, "Progress A")

	// Tenant B: the same owner parent holds a second child in a
	// differently-named class, so a list response can tell the two apart.
	categoryB := uuid.New()
	classB := uuid.New()
	enrollB := uuid.New()
	exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryB, f.tenantB, "KEL-184 B")
	exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		classB, f.tenantB, categoryB, f.classBName, "group", 100, true, "open")
	exec("INSERT INTO students (id, parent_id, first_name, last_name) VALUES (?, ?, ?, ?)", f.studentB, f.owner, "Siswa", "Kedua")
	exec("INSERT INTO enrollments (id, tenant_id, student_id, class_id, status, billing_cycle, idempotency_key, payment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		enrollB, f.tenantB, f.studentB, classB, "active", "monthly", uuid.NewString(), "paid")
	exec("INSERT INTO reports (id, tenant_id, enrollment_id, reporter_id, title) VALUES (?, ?, ?, ?, ?)",
		f.reportB, f.tenantB, enrollB, tutor, "Progress B")

	// Tenant A again: another parent's child + report that must never leak
	// into the owner's reads.
	otherStudent, otherEnroll := uuid.New(), uuid.New()
	exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", otherStudent, f.other, "Other child")
	exec("INSERT INTO enrollments (id, tenant_id, student_id, class_id, status, billing_cycle, idempotency_key, payment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		otherEnroll, f.tenantA, otherStudent, classA, "active", "monthly", uuid.NewString(), "paid")
	exec("INSERT INTO reports (id, tenant_id, enrollment_id, reporter_id, title) VALUES (?, ?, ?, ?, ?)",
		f.otherRep, f.tenantA, otherEnroll, tutor, "Other progress")
	return f
}

func assertReportIdentity(t *testing.T, item domain.Report, wantStudentID uuid.UUID, wantClassName string) {
	t.Helper()
	if item.Enrollment == nil {
		t.Fatalf("report %s has nil enrollment", item.ID)
	}
	if item.Enrollment.Student == nil {
		t.Fatalf("report %s enrollment has nil student", item.ID)
	}
	if item.Enrollment.Class == nil {
		t.Fatalf("report %s enrollment has nil class", item.ID)
	}
	if item.Enrollment.Student.ID != wantStudentID {
		t.Fatalf("student id=%s, want %s", item.Enrollment.Student.ID, wantStudentID)
	}
	if item.Enrollment.Class.Name != wantClassName {
		t.Fatalf("class name=%q, want %q", item.Enrollment.Class.Name, wantClassName)
	}
	if item.Enrollment.Student.ParentID != uuid.Nil || item.Enrollment.Student.DateOfBirth != nil || item.Enrollment.Student.Gender != nil || item.Enrollment.Class.TenantID != uuid.Nil {
		t.Fatal("identity preload exposed fields outside the name/id projection")
	}
	if item.Enrollment.Student.FirstName == "" {
		t.Fatalf("report %s student first name is empty", item.ID)
	}
}

// Tenant-scoped list and detail carry the real identities.
func TestReportEnrollmentIdentitiesTenantScoped(t *testing.T) {
	f := newReportIdentityFixture(t)
	ctx := context.Background()
	reports := NewReportRepository(f.db)

	items, total, err := reports.List(ctx, f.tenantA, domain.ReportQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("tenant A total=%d len=%d, want 2 rows (both parents' rows, unchanged)", total, len(items))
	}
	byID := map[uuid.UUID]domain.Report{}
	for _, item := range items {
		byID[item.ID] = item
	}
	assertReportIdentity(t, byID[f.reportA], f.studentA, f.classAName)

	detail, err := reports.GetByIDForTenant(ctx, f.tenantA, f.reportA)
	if err != nil {
		t.Fatalf("GetByIDForTenant: %v", err)
	}
	assertReportIdentity(t, *detail, f.studentA, f.classAName)
	// Updating a report cannot persist its partial identity projections.
	detail.Enrollment.Student.FirstName = "Must not persist"
	detail.Enrollment.Class.Name = "Must not persist"
	detail.Title = "Updated report"
	if err := reports.Update(ctx, detail); err != nil {
		t.Fatal(err)
	}
	reloaded, err := reports.GetByIDForTenant(ctx, f.tenantA, f.reportA)
	if err != nil {
		t.Fatal(err)
	}
	assertReportIdentity(t, *reloaded, f.studentA, f.classAName)
	if reloaded.Title != "Updated report" || reloaded.Enrollment.Student.FirstName != "Siswa" {
		t.Fatal("report update changed an identity or did not persist title")
	}
}

// Parent-scoped list and detail carry the real identities across tenants, and
// distinguish the two enrollments of the same parent.
func TestReportEnrollmentIdentitiesParentScoped(t *testing.T) {
	f := newReportIdentityFixture(t)
	ctx := context.Background()
	reports := NewReportRepository(f.db)

	items, total, err := reports.ListForParent(ctx, f.owner, domain.ReportQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListForParent: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("owner total=%d len=%d, want 2 (one per tenant)", total, len(items))
	}
	students, classes := map[uuid.UUID]bool{}, map[string]bool{}
	for _, item := range items {
		if item.Enrollment == nil || item.Enrollment.Student == nil || item.Enrollment.Class == nil {
			t.Fatalf("report %s misses nested student/class", item.ID)
		}
		students[item.Enrollment.Student.ID] = true
		classes[item.Enrollment.Class.Name] = true
	}
	if !students[f.studentA] || !students[f.studentB] {
		t.Fatalf("parent list students=%v, want both %s and %s", students, f.studentA, f.studentB)
	}
	if !classes[f.classAName] || !classes[f.classBName] {
		t.Fatalf("parent list classes=%v, want %q and %q", classes, f.classAName, f.classBName)
	}

	detail, err := reports.GetForParent(ctx, f.owner, f.reportA)
	if err != nil {
		t.Fatalf("GetForParent: %v", err)
	}
	assertReportIdentity(t, *detail, f.studentA, f.classAName)
}

// Fail-closed scope: foreign tenant/parent stays 404 / scoped empty and no
// other row's identity leaks.
func TestReportEnrollmentIdentitiesFailClosed(t *testing.T) {
	f := newReportIdentityFixture(t)
	ctx := context.Background()
	reports := NewReportRepository(f.db)

	if _, err := reports.GetByIDForTenant(ctx, f.tenantB, f.reportA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("tenant B detail of tenant A report err=%v, want ErrRecordNotFound", err)
	}
	if items, total, err := reports.List(ctx, f.tenantB, domain.ReportQuery{Page: 1, PageSize: 20}); err != nil || total != 1 || len(items) != 1 || items[0].ID != f.reportB {
		t.Fatalf("tenant B list total=%d err=%v, want only tenant B's own row", total, err)
	} else {
		assertReportIdentity(t, items[0], f.studentB, f.classBName)
	}

	if _, err := reports.GetForParent(ctx, f.other, f.reportA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("other parent detail of owner report err=%v, want ErrRecordNotFound", err)
	}
	if _, total, err := reports.ListForParent(ctx, f.other, domain.ReportQuery{Page: 1, PageSize: 20}); err != nil || total != 1 {
		t.Fatalf("other parent list total=%d err=%v, want exactly 1 (their own)", total, err)
	}

	var foreignEnroll uuid.UUID
	var foreignEnrollStr string
	if err := f.db.Raw("SELECT enrollment_id FROM reports WHERE id = ?", f.otherRep).Scan(&foreignEnrollStr).Error; err != nil {
		t.Fatal(err)
	}
	foreignEnroll, err := uuid.Parse(foreignEnrollStr)
	if err != nil {
		t.Fatal(err)
	}
	if notes, total, err := reports.ListForParent(ctx, f.owner, domain.ReportQuery{Page: 1, PageSize: 20, EnrollmentID: &foreignEnroll}); err != nil || total != 0 || len(notes) != 0 {
		t.Fatalf("foreign enrollment filter total=%d err=%v, want 0", total, err)
	}
}
