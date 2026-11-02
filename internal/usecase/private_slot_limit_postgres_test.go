package usecase

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPrivateSlotLimitPostgres(t *testing.T) {
	dsn := os.Getenv("KEL229_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL229_TEST_DSN not set")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	basePool, _ := base.DB()
	defer basePool.Close()
	schema := "kel229_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	defer base.Exec("DROP SCHEMA " + schema + " CASCADE")
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema+",public"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	must := func(query string, args ...any) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	migrate := func(file string) {
		t.Helper()
		raw, err := os.ReadFile("../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		must(string(raw))
	}
	for _, file := range []string{"00000000000000_init_schema.up.sql", "00001790812223_add_rescheduled_from_to_class_sessions.up.sql", "00001790600000_private_schedule_requests.up.sql", "00001790600001_private_schedule_recommendations.up.sql", "00001790900000_private_request_payment_method.up.sql", "00001790900004_private_request_chat.up.sql"} {
		migrate(file)
	}
	tenant, category, legacy, parent := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	must("INSERT INTO categories(id,tenant_id,name) VALUES(?,?,'Test')", category, tenant)
	must("INSERT INTO classes(id,tenant_id,category_id,name,type,price,is_published) VALUES(?,?,?,'Legacy','private',100,true)", legacy, tenant, category)
	migrate("00001790900006_private_slot_limit.up.sql")
	migrate("00001790900007_private_approved_slots.up.sql")
	classes, categories := repository.NewClassRepository(db), repository.NewCategoryRepository(db)
	ctx := context.Background()
	stored, err := classes.GetByID(ctx, legacy)
	if err != nil || stored.PrivateSlotLimit != nil {
		t.Fatal("legacy backfill", err)
	}
	tx := repository.NewTransactionManager(db)
	classUC := NewClassUsecase(classes, nil, nil, nil, &tenantClientStub{}, tx, categories)
	for _, limit := range []int{1, 3} {
		created, err := classUC.CreateClass(ctx, tenant, &domain.CreateClassRequest{CategoryID: category, Name: "Bounded", Type: "private", Price: 100, PrivateSlotLimit: &limit})
		if err != nil || created.PrivateSlotLimit == nil || *created.PrivateSlotLimit != limit {
			t.Fatalf("create/read %v %v", created, err)
		}
		read, err := classes.GetByID(ctx, created.ID)
		if err != nil || read.PrivateSlotLimit == nil || *read.PrivateSlotLimit != limit {
			t.Fatal("storage", err)
		}
	}
	defaulted, err := classUC.CreateClass(ctx, tenant, &domain.CreateClassRequest{CategoryID: category, Name: "Default", Type: "private", Price: 100})
	if err != nil || *defaulted.PrivateSlotLimit != 1 {
		t.Fatal("default", err)
	}
	limit := 2
	invalid, changedName := 0, "Must roll back"
	if _, err := classUC.UpdateClass(ctx, tenant, legacy, &domain.UpdateClassRequest{PrivateSlotLimit: &invalid, Name: &changedName}); !errors.Is(err, domain.ErrInvalidPrivateSlotLimit) {
		t.Fatal("invalid update accepted", err)
	}
	unchanged, err := classes.GetByID(ctx, legacy)
	if err != nil || unchanged.Name != "Legacy" || unchanged.PrivateSlotLimit != nil {
		t.Fatal("invalid update partially changed legacy", err)
	}
	if _, err := classUC.UpdateClass(ctx, uuid.New(), legacy, &domain.UpdateClassRequest{PrivateSlotLimit: &limit}); !errors.Is(err, domain.ErrClassNotFound) {
		t.Fatal("tenant bypass", err)
	}
	group := "group"
	if _, err := classUC.UpdateClass(ctx, tenant, legacy, &domain.UpdateClassRequest{Type: &group, PrivateSlotLimit: &limit}); !errors.Is(err, domain.ErrClassTypeImmutable) {
		t.Fatal("type changed", err)
	}
	requests, enrollments, schedules, sessions := repository.NewPrivateScheduleRequestRepository(db), repository.NewEnrollmentRepository(db), repository.NewScheduleRepository(db), repository.NewSessionRepository(db)
	student := uuid.New()
	must("INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'Student')", student, parent)
	slots := []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}, {DayOfWeek: 3, StartTime: "10:00:00", EndTime: "11:00:00"}}
	request := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: tenant, ParentID: parent, StudentID: student, ClassID: legacy, ParentEmail: "parent@example.test", Status: "pending", BillingCycle: "monthly", Slots: slots}
	if err := requests.Create(ctx, request); err != nil {
		t.Fatal(err)
	}
	invoice := &concurrentApprovalBilling{response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/checkout"}}
	uc := NewPrivateScheduleRequestUsecase(requests, repository.NewStudentRepository(db), classes, tx, enrollments, schedules, sessions, invoice)
	lockHeld, release := make(chan struct{}), make(chan struct{})
	editDone, approvalDone := make(chan error, 1), make(chan error, 1)
	go func() {
		editDone <- tx.WithTransaction(ctx, func(locked context.Context) error {
			if _, err := classes.(repository.ClassUpdateStore).GetByIDForUpdate(locked, legacy); err != nil {
				return err
			}
			if err := repository.GetDB(locked, db).Model(&domain.Class{}).Where("id = ?", legacy).Update("private_slot_limit", 1).Error; err != nil {
				return err
			}
			close(lockHeld)
			<-release
			return nil
		})
	}()
	select {
	case <-lockHeld:
	case err := <-editDone:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("edit lock timeout")
	}
	approval := &domain.ApprovePrivateScheduleRequest{SelectedSlotIndexes: []int{0, 1}, ProposalSnapshot: slots, ExpectedPrivateSlotLimit: &limit}
	go func() { _, err := uc.Approve(ctx, tenant, request.ID, approval); approvalDone <- err }()
	select {
	case err := <-approvalDone:
		close(release)
		t.Fatalf("approval bypassed class lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if err := <-editDone; err != nil {
		t.Fatal(err)
	}
	if err := <-approvalDone; !errors.Is(err, domain.ErrPrivateSelectionStale) {
		t.Fatal("stale approval", err)
	}
	for _, table := range []string{"enrollments", "class_schedules", "class_sessions"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("partial %s=%d %v", table, count, err)
		}
	}
	if invoice.keys != 0 {
		t.Fatal("invoice side effect")
	}
	if _, err := classUC.UpdateClass(ctx, tenant, legacy, &domain.UpdateClassRequest{PrivateSlotLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	result, err := uc.Approve(ctx, tenant, request.ID, approval)
	if err != nil {
		t.Fatal(err)
	}
	limit = 1
	if _, err := classUC.UpdateClass(ctx, tenant, legacy, &domain.UpdateClassRequest{PrivateSlotLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	if replay, err := uc.Approve(ctx, tenant, request.ID); err != nil || replay.Enrollment.ID != result.Enrollment.ID {
		t.Fatal("replay", err)
	}
	var count int64
	db.Table("class_schedules").Where("class_id = ?", legacy).Count(&count)
	if count != 2 {
		t.Fatal("materialized slots changed")
	}
	migrate("00001790900006_private_slot_limit.down.sql")
	db.Table("class_schedules").Where("class_id = ?", legacy).Count(&count)
	if count != 2 {
		t.Fatal("rollback changed slots")
	}
	migrate("00001790900006_private_slot_limit.up.sql")
	stored, err = classes.GetByID(ctx, legacy)
	if err != nil || stored.PrivateSlotLimit != nil {
		t.Fatal("roundtrip", err)
	}
}
