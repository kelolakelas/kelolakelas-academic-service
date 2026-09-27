package usecase

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

// Run with KEL106_TEST_DSN against a disposable PostgreSQL database. It drives the
// real repositories and transaction manager, so the capacity count, the
// student/class unique index and the idempotency index are PostgreSQL's own.
func TestPlatformFeeRejectionPostgres(t *testing.T) {
	dsn := os.Getenv("KEL106_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL106_TEST_DSN not set")
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
	defer conn.Close()
	// Key 51 is the one the other schema-applying Postgres tests share, so the init
	// schema is never applied by two packages at once on a fresh database.
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

	ctx := context.Background()
	tenantID, categoryID, classID, scheduleID, parentID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := db.Exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryID, tenantID, "KEL-106").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", classID, tenantID, categoryID, "KEL-106 group", "group", 100, true, "open").Error; err != nil {
		t.Fatal(err)
	}
	// Capacity 1: a rejected attempt that kept its seat would fill the schedule.
	if err := db.Create(&domain.ClassSchedule{ID: scheduleID, ClassID: classID, Capacity: 1, DayOfWeek: 3, StartTime: "10:00:00", EndTime: "11:00:00"}).Error; err != nil {
		t.Fatal(err)
	}
	newStudent := func() uuid.UUID {
		id := uuid.New()
		if err := db.Exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", id, parentID, "KEL-106 student").Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	enrollmentRepo := repository.NewEnrollmentRepository(db)
	billingClient := &feeBilling{err: billing.ErrPlatformFeeExceedsGross}
	uc := NewEnrollmentUsecase(enrollmentRepo, repository.NewStudentRepository(db), repository.NewClassRepository(db), billingClient, repository.NewTransactionManager(db))
	enroll := func(studentID uuid.UUID, key string) error {
		_, err := uc.EnrollPublic(ctx, parentID, classID, &domain.PublicEnrollmentRequest{StudentID: studentID, BillingCycle: "monthly", ScheduleID: &scheduleID}, key)
		return err
	}
	count := func(query string, args ...any) int64 {
		var n int64
		if err := db.Model(&domain.Enrollment{}).Where(query, args...).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	liveSeats := func() int64 {
		return count("schedule_id = ? AND status IN ? AND deleted_at IS NULL", scheduleID, []string{"pending", "active"})
	}

	studentID := newStudent()
	rejectedKey := uuid.NewString()
	if err := enroll(studentID, rejectedKey); !errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
		t.Fatalf("rejected attempt = %v, want ErrPlatformFeeExceedsGross", err)
	}
	var rejected domain.Enrollment
	if err := db.Where("idempotency_key = ?", rejectedKey).First(&rejected).Error; err != nil {
		t.Fatal(err)
	}
	if rejected.Status != "dropped" || rejected.PaymentStatus != domain.PaymentStatusPlatformFeeRejected || rejected.PaymentTransactionID != nil {
		t.Fatalf("rejected row = status %q payment %q transaction %v", rejected.Status, rejected.PaymentStatus, rejected.PaymentTransactionID)
	}
	if seats := liveSeats(); seats != 0 {
		t.Fatalf("seats counted after rejection = %d, want 0", seats)
	}

	// Same-key replay: still the rejection, no new row, no new invoice request.
	if err := enroll(studentID, rejectedKey); !errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
		t.Fatalf("same-key replay = %v, want ErrPlatformFeeExceedsGross", err)
	}
	if rows := count("student_id = ? AND class_id = ?", studentID, classID); rows != 1 || billingClient.calls != 1 {
		t.Fatalf("after replay: enrollments = %d, billing calls = %d, want 1 and 1", rows, billingClient.calls)
	}

	// A new attempt for the same student and class is not a duplicate and takes
	// the only seat once billing accepts the invoice.
	billingClient.err = nil
	if err := enroll(studentID, uuid.NewString()); err != nil {
		t.Fatalf("new attempt after rejection = %v, want success", err)
	}
	if seats := liveSeats(); seats != 1 {
		t.Fatalf("seats after the accepted attempt = %d, want 1", seats)
	}
	if err := enroll(studentID, rejectedKey); !errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
		t.Fatalf("rejected key replayed after a success = %v, want ErrPlatformFeeExceedsGross", err)
	}
	// The seat is really taken now: capacity checks still work after the release.
	if err := enroll(newStudent(), uuid.NewString()); !errors.Is(err, domain.ErrScheduleFull) {
		t.Fatalf("other student on the full schedule = %v, want ErrScheduleFull", err)
	}
}
