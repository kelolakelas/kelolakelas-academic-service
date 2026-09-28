package usecase

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

// Runs against an isolated KEL108_TEST_DSN database; no shared DB is mutated.
func TestPrivateApprovalPostgresParallel(t *testing.T) {
	dsn := os.Getenv("KEL108_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL108_TEST_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range []string{"00000000000000_init_schema.up.sql", "00001790600000_private_schedule_requests.up.sql"} {
		sql, readErr := os.ReadFile("../../migrations/" + migration)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := db.Exec(string(sql)).Error; err != nil {
			t.Fatal(err)
		}
	}
	tenant, parent, studentID, classID, categoryID, requestID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, stmt := range []struct {
		sql  string
		args []interface{}
	}{
		{"INSERT INTO categories(id,tenant_id,name) VALUES(?,?, 'test')", []interface{}{categoryID, tenant}},
		{"INSERT INTO classes(id,tenant_id,category_id,name,type,price,is_published,enrollment_status) VALUES(?,?,?,'private','private',100,true,'open')", []interface{}{classID, tenant, categoryID}},
		{"INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'test')", []interface{}{studentID, parent}},
	} {
		if err := db.Exec(stmt.sql, stmt.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	slots := []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}, {DayOfWeek: 3, StartTime: "10:00:00", EndTime: "11:00:00"}}
	req := &domain.PrivateScheduleRequest{ID: requestID, TenantID: tenant, ParentID: parent, StudentID: studentID, ClassID: classID, ParentEmail: "verified@example.test", BillingCycle: "monthly", Slots: slots, Status: "pending"}
	if err := repository.NewPrivateScheduleRequestRepository(db).Create(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	bill := &concurrentApprovalBilling{response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/session"}}
	u := NewPrivateScheduleRequestUsecase(repository.NewPrivateScheduleRequestRepository(db), repository.NewStudentRepository(db), repository.NewClassRepository(db), repository.NewTransactionManager(db), repository.NewEnrollmentRepository(db), repository.NewScheduleRepository(db), repository.NewSessionRepository(db), bill)
	const workers = 8
	var wg sync.WaitGroup
	out := make(chan *domain.PublicEnrollmentResponse, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := u.Approve(context.Background(), tenant, requestID)
			out <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(out)
	close(errs)
	var first uuid.UUID
	for err := range errs {
		if err != nil {
			t.Fatalf("parallel approval: %v", err)
		}
	}
	for result := range out {
		if result == nil || result.Payment.CheckoutSessionURL != "https://pay.example.test/session" {
			t.Fatalf("result=%+v", result)
		}
		if first == uuid.Nil {
			first = result.Enrollment.ID
		}
		if result.Enrollment.ID != first {
			t.Fatalf("different enrollment %s vs %s", first, result.Enrollment.ID)
		}
	}
	for _, q := range []struct {
		table string
		count int64
	}{{"enrollments", 1}, {"class_schedules", 2}} {
		var n int64
		if err := db.Table(q.table).Where("class_id = ?", classID).Count(&n).Error; err != nil || n != q.count {
			t.Fatalf("%s count=%d err=%v", q.table, n, err)
		}
	}
	var sessions int64
	if err := db.Table("class_sessions").Where("enrollment_id = ?", first).Count(&sessions).Error; err != nil || sessions < 1 {
		t.Fatalf("sessions=%d err=%v", sessions, err)
	}
	if bill.keys < 1 || bill.keys > workers {
		t.Fatalf("invoice attempts=%d", bill.keys)
	}
}

type concurrentApprovalBilling struct {
	sync.Mutex
	response billing.InvoiceResponse
	keys     int
}

func (b *concurrentApprovalBilling) GenerateInvoice(_ context.Context, req billing.InvoiceRequest) (*billing.InvoiceResponse, error) {
	b.Lock()
	defer b.Unlock()
	b.keys++
	return &b.response, nil
}
func (*concurrentApprovalBilling) CancelEnrollmentPayment(context.Context, uuid.UUID) (*billing.CancelResponse, error) {
	return nil, nil
}
