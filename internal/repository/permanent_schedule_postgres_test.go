package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Run with KEL21_TEST_DSN against a disposable PostgreSQL database.
func TestPermanentScheduleTransferPostgres(t *testing.T) {
	dsn := os.Getenv("KEL21_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL21_TEST_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// Go runs packages concurrently; serialize shared-schema setup and fixtures.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock(51, 1)"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(51, 1)")
	schema, err := os.ReadFile("../../migrations/00000000000000_init_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatal(err)
	}
	tenant, classID, category, student, oldID, newID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, sql := range []string{
		"INSERT INTO categories (id, tenant_id, name) VALUES ('" + category.String() + "', '" + tenant.String() + "', 'Math')",
		"INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published) VALUES ('" + classID.String() + "', '" + tenant.String() + "', '" + category.String() + "', 'Group', 'group', 100, true)",
		"INSERT INTO students (id, parent_id, first_name) VALUES ('" + student.String() + "', '" + uuid.NewString() + "', 'Student')",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	tutor := uuid.New()
	old := &domain.ClassSchedule{ID: oldID, ClassID: classID, TutorID: &tutor, Capacity: 2, DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}
	schedules := NewScheduleRepository(db)
	if err := schedules.Create(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	enrollments := NewEnrollmentRepository(db)
	live := &domain.Enrollment{ID: uuid.New(), TenantID: tenant, StudentID: student, ClassID: classID, ScheduleID: &oldID, Status: "active", BillingCycle: "monthly", JoinedAt: time.Now()}
	if err := enrollments.Create(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := NewTransactionManager(db).WithTransaction(ctx, func(tx context.Context) error {
		locked, err := schedules.GetByIDForTenantForUpdate(tx, tenant, oldID)
		if err != nil {
			return err
		}
		replacement := &domain.ClassSchedule{ID: newID, ClassID: classID, TutorID: locked.TutorID, Capacity: locked.Capacity, DayOfWeek: 2, StartTime: "10:00:00", EndTime: "11:00:00"}
		if err := schedules.Create(tx, replacement); err != nil {
			return err
		}
		return enrollments.TransferSchedule(tx, tenant, classID, oldID, newID)
	}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&domain.Enrollment{}).Where("schedule_id = ? AND status IN ?", newID, []string{"active", "pending"}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("replacement quota count=%d err=%v, want 1", count, err)
	}
	attendees, err := enrollments.GetActiveByScheduleID(ctx, tenant, newID)
	if err != nil || len(attendees) != 1 || attendees[0].ID != live.ID {
		t.Fatalf("replacement attendees=%v err=%v", attendees, err)
	}
}
