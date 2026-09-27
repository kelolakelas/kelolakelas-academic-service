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

// Run with KEL70_TEST_DSN against a disposable PostgreSQL database. It proves the
// KEL-70 schedule preload against the real schema: parent A's list and detail
// carry only the schedules of parent A's own enrollments, a private enrollment
// has none, and a soft-deleted schedule is not loaded while schedule_id stays.
func TestEnrollmentSchedulePreloadPostgres(t *testing.T) {
	dsn := os.Getenv("KEL70_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL70_TEST_DSN not set")
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
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock(70, 1)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(70, 1)") })
	schema, err := os.ReadFile("../../migrations/00000000000000_init_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	repo := NewEnrollmentRepository(db)
	tenantA, tenantB := uuid.New(), uuid.New()
	parentA, parentB := uuid.New(), uuid.New()

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	class := func(tenantID uuid.UUID, classType string) uuid.UUID {
		t.Helper()
		categoryID, classID := uuid.New(), uuid.New()
		mustExec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryID, tenantID, "KEL-70")
		mustExec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, true, 'open')", classID, tenantID, categoryID, "KEL-70 "+classType, classType, 100)
		return classID
	}
	schedule := func(classID uuid.UUID, day int, start, end string, location *string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if err := db.Create(&domain.ClassSchedule{ID: id, ClassID: classID, Capacity: 5, DayOfWeek: day, StartTime: start, EndTime: end, Location: location}).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	enroll := func(tenantID, parentID, classID uuid.UUID, scheduleID *uuid.UUID) uuid.UUID {
		t.Helper()
		studentID, enrollmentID := uuid.New(), uuid.New()
		mustExec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", studentID, parentID, "KEL-70 student")
		if err := db.Create(&domain.Enrollment{ID: enrollmentID, TenantID: tenantID, StudentID: studentID, ClassID: classID, ScheduleID: scheduleID, Status: "active", BillingCycle: "monthly", PaymentStatus: "paid"}).Error; err != nil {
			t.Fatal(err)
		}
		return enrollmentID
	}

	roomA, roomB := "Ruang A", "Ruang B"
	groupA := class(tenantA, "group")
	groupB := class(tenantB, "group")
	privateA := class(tenantA, "private")
	scheduleA := schedule(groupA, 1, "16:00:00", "17:30:00", &roomA)
	scheduleB := schedule(groupB, 3, "09:00:00", "10:00:00", &roomB)
	deletedA := schedule(groupA, 5, "13:00:00", "14:00:00", nil)

	ownGroup := enroll(tenantA, parentA, groupA, &scheduleA)
	ownPrivate := enroll(tenantA, parentA, privateA, nil)
	ownDeleted := enroll(tenantA, parentA, groupA, &deletedA)
	foreign := enroll(tenantB, parentB, groupB, &scheduleB)
	if err := db.Delete(&domain.ClassSchedule{}, "id = ?", deletedA).Error; err != nil {
		t.Fatal(err)
	}

	items, total, err := repo.List(ctx, nil, &parentA, domain.EnrollmentQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("parent A total=%d items=%d, want 3/3", total, len(items))
	}
	byID := map[uuid.UUID]*domain.Enrollment{}
	for _, item := range items {
		byID[item.ID] = item
		if item.ID == foreign {
			t.Fatalf("parent A list contains parent B enrollment %s", foreign)
		}
		if item.Schedule != nil && item.Schedule.ID == scheduleB {
			t.Fatalf("parent A list carries parent B schedule %s", scheduleB)
		}
	}
	if got := byID[ownGroup].Schedule; got == nil || got.ID != scheduleA || got.DayOfWeek != 1 || got.StartTime != "16:00:00" || got.EndTime != "17:30:00" || got.Location == nil || *got.Location != roomA {
		t.Fatalf("group enrollment schedule = %+v, want Senin 16:00-17:30 Ruang A", got)
	}
	if got := byID[ownPrivate]; got == nil || got.ScheduleID != nil || got.Schedule != nil {
		t.Fatalf("private enrollment = %+v, want no schedule", got)
	}
	if got := byID[ownDeleted]; got == nil || got.ScheduleID == nil || *got.ScheduleID != deletedA || got.Schedule != nil {
		t.Fatalf("enrollment on soft-deleted schedule = %+v, want schedule_id kept and Schedule nil", got)
	}

	// Detail: own enrollment carries the schedule; parent B's is not found for A.
	detail, err := repo.GetByIDForAccess(ctx, nil, &parentA, ownGroup)
	if err != nil {
		t.Fatalf("GetByIDForAccess own: %v", err)
	}
	if detail.Schedule == nil || detail.Schedule.ID != scheduleA {
		t.Fatalf("detail schedule = %+v, want %s", detail.Schedule, scheduleA)
	}
	if _, err := repo.GetByIDForAccess(ctx, nil, &parentA, foreign); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("parent A detail of parent B enrollment err = %v, want ErrRecordNotFound", err)
	}
	// Tenant scope: tenant A cannot read tenant B's enrollment either.
	if _, err := repo.GetByIDForAccess(ctx, &tenantA, nil, foreign); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("tenant A detail of tenant B enrollment err = %v, want ErrRecordNotFound", err)
	}
	// Parent B still sees its own schedule.
	itemsB, _, err := repo.List(ctx, nil, &parentB, domain.EnrollmentQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("List parent B: %v", err)
	}
	if len(itemsB) != 1 || itemsB[0].ID != foreign || itemsB[0].Schedule == nil || itemsB[0].Schedule.ID != scheduleB {
		t.Fatalf("parent B items = %+v, want only its enrollment with schedule %s", itemsB, scheduleB)
	}
}
