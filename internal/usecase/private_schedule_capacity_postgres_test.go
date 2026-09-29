package usecase

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

// KEL-132 integration proof against a disposable PostgreSQL database.
//
// Run with KEL132_TEST_DSN pointing at a disposable PostgreSQL database,
// for example a kel132-db-test container holding a kel132_test database.
// Example:
// KEL132_TEST_DSN="host=127.0.0.1 user=postgres password=postgres dbname=kel132_test port=<mapped> sslmode=disable" go test -race -count=1 -run TestPrivateCapacityPostgres ./internal/usecase/
//
// It proves, on the real schema and inside real transactions:
//  1. a new private schedule with capacity > 1 is rejected (direct use case
//     call, the same path the tenant API handler takes);
//  2. a group schedule with capacity > 1 is still accepted;
//  3. a legacy private row with capacity > 1 stays readable without any
//     destructive migration;
//  4. an approval of an existing private request still creates capacity-1
//     schedules;
//  5. a foreign tenant's class and request are rejected as missing/forbidden.
func TestPrivateCapacityPostgres(t *testing.T) {
	dsn := os.Getenv("KEL132_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL132_TEST_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range []string{
		"00000000000000_init_schema.up.sql",
		"00001790600000_private_schedule_requests.up.sql",
		"00001790600001_private_schedule_recommendations.up.sql",
	} {
		sql, readErr := os.ReadFile("../../migrations/" + migration)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := db.Exec(string(sql)).Error; err != nil {
			t.Fatalf("%s: %v", migration, err)
		}
	}

	ctx := context.Background()
	txManager := repository.NewTransactionManager(db)
	classRepo := repository.NewClassRepository(db)
	scheduleRepo := repository.NewScheduleRepository(db)
	sessionRepo := repository.NewSessionRepository(db)
	enrollmentRepo := repository.NewEnrollmentRepository(db)
	requestRepo := repository.NewPrivateScheduleRequestRepository(db)
	u := NewScheduleUsecase(txManager, classRepo, scheduleRepo, sessionRepo, enrollmentRepo)

	tenant, otherTenant := uuid.New(), uuid.New()
	parent := uuid.New()
	category := uuid.New()
	privateID, groupID, foreignID := uuid.New(), uuid.New(), uuid.New()
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	mustExec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, 'KEL-132')", category, tenant)
	for _, item := range []struct {
		id, cat, owner uuid.UUID
		name, kind     string
	}{
		{privateID, category, tenant, "KEL-132 private", "private"},
		{groupID, category, tenant, "KEL-132 group", "group"},
	} {
		mustExec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, 100, true, 'open')",
			item.id, item.owner, item.cat, item.name, item.kind)
	}
	foreignCategory := uuid.New()
	mustExec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, 'KEL-132 foreign')", foreignCategory, otherTenant)
	mustExec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, 'KEL-132 foreign private', 'private', 100, true, 'open')",
		foreignID, otherTenant, foreignCategory)

	student := uuid.New()
	mustExec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, 'KEL-132')", student, parent)
	enrollment := &domain.Enrollment{ID: uuid.New(), TenantID: tenant, StudentID: student, ClassID: privateID, Status: "active", BillingCycle: "monthly", PaymentStatus: "paid"}
	if err := enrollmentRepo.Create(ctx, enrollment); err != nil {
		t.Fatal(err)
	}
	firstOfMonth := func() time.Time {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	}
	countSchedules := func(classID uuid.UUID) int64 {
		var n int64
		if err := db.Model(&domain.ClassSchedule{}).Where("class_id = ?", classID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}

	// 1. Private capacity > 1 is rejected and persists nothing.
	from := firstOfMonth()
	if _, err := u.CreateInitialSchedules(ctx, tenant, &domain.CreateInitialSchedulesRequest{
		ClassID:   privateID,
		Schedules: []domain.ScheduleItemRequest{{EnrollmentID: &enrollment.ID, Capacity: 3, DayOfWeek: 1, StartTime: "09:00", EndTime: "10:00", ValidFrom: &from}},
	}); !errors.Is(err, ErrPrivateScheduleCapacity) {
		t.Fatalf("private capacity 3 err=%v, want ErrPrivateScheduleCapacity", err)
	}
	if n := countSchedules(privateID); n != 0 {
		t.Fatalf("rejected private write persisted %d schedules", n)
	}

	// 2. Group capacity > 1 is still accepted.
	created, err := u.CreateInitialSchedules(ctx, tenant, &domain.CreateInitialSchedulesRequest{
		ClassID:   groupID,
		Schedules: []domain.ScheduleItemRequest{{Capacity: 8, DayOfWeek: 1, StartTime: "09:00", EndTime: "10:00", ValidFrom: &from}},
	})
	if err != nil || len(created.Schedules) != 1 || created.Schedules[0].Capacity != 8 {
		t.Fatalf("group capacity 8 res=%+v err=%v", created, err)
	}
	if n := countSchedules(groupID); n != 1 {
		t.Fatalf("group schedules=%d, want 1", n)
	}

	// 3. Legacy private rows with capacity > 1 stay readable without migration.
	legacy := &domain.ClassSchedule{ID: uuid.New(), ClassID: privateID, EnrollmentID: &enrollment.ID, Capacity: 5, DayOfWeek: 3, StartTime: "10:00:00", EndTime: "11:00:00"}
	if err := scheduleRepo.Create(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	readBack, err := scheduleRepo.GetByIDForTenant(ctx, tenant, legacy.ID)
	if err != nil || readBack.Capacity != 5 {
		t.Fatalf("legacy private schedule read=%+v err=%v, want capacity 5", readBack, err)
	}

	// 4. Approving an existing private request still creates capacity-1 schedules.
	// The approval flow needs a student without a prior enrollment in the
	// private class, so it gets its own student under the same parent.
	approvalStudent := uuid.New()
	mustExec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, 'KEL-132 approval')", approvalStudent, parent)
	requestID := uuid.New()
	slots := []domain.PrivateScheduleSlot{{DayOfWeek: 2, StartTime: "10:00:00", EndTime: "11:00:00"}}
	if err := requestRepo.Create(ctx, &domain.PrivateScheduleRequest{ID: requestID, TenantID: tenant, ParentID: parent, StudentID: approvalStudent, ClassID: privateID, ParentEmail: "verified@example.test", BillingCycle: "monthly", Slots: slots, Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	bill := &approvalBilling{response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/kel132"}}
	approveUsecase := NewPrivateScheduleRequestUsecase(requestRepo, repository.NewStudentRepository(db), classRepo, txManager, enrollmentRepo, scheduleRepo, sessionRepo, bill)
	approved, err := approveUsecase.Approve(ctx, tenant, requestID)
	if err != nil || approved == nil {
		t.Fatalf("approve err=%v res=%+v", err, approved)
	}
	var approvalSchedules []domain.ClassSchedule
	if err := db.Where("enrollment_id = ?", approved.Enrollment.ID).Find(&approvalSchedules).Error; err != nil {
		t.Fatal(err)
	}
	if len(approvalSchedules) != len(slots) {
		t.Fatalf("approval schedules=%d, want %d", len(approvalSchedules), len(slots))
	}
	for _, s := range approvalSchedules {
		if s.Capacity != 1 {
			t.Fatalf("approval schedule=%+v, want capacity 1", s)
		}
	}

	// 5. Foreign-tenant resources are rejected without touching data.
	if _, err := u.CreateInitialSchedules(ctx, tenant, &domain.CreateInitialSchedulesRequest{
		ClassID:   foreignID,
		Schedules: []domain.ScheduleItemRequest{{Capacity: 1, DayOfWeek: 1, StartTime: "09:00", EndTime: "10:00", ValidFrom: &from}},
	}); !errors.Is(err, domain.ErrClassForbidden) {
		t.Fatalf("foreign class err=%v, want ErrClassForbidden", err)
	}
	if _, err := approveUsecase.Approve(ctx, otherTenant, requestID); !errors.Is(err, domain.ErrPrivateRequestNotFound) {
		t.Fatalf("foreign tenant approve err=%v, want ErrPrivateRequestNotFound", err)
	}
	if n := countSchedules(foreignID); n != 0 {
		t.Fatalf("foreign class write persisted %d schedules", n)
	}
}
