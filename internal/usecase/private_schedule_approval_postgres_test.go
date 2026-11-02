package usecase

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

// Runs against an isolated KEL108_TEST_DSN database; no shared DB is mutated.
func TestPrivateApprovalPostgresParallel(t *testing.T) {
	db := privateWorkflowPG(t, "KEL108_TEST_DSN")
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
	err      error
}

func (b *concurrentApprovalBilling) GenerateInvoice(_ context.Context, req billing.InvoiceRequest) (*billing.InvoiceResponse, error) {
	b.Lock()
	defer b.Unlock()
	b.keys++
	if b.err != nil {
		return nil, b.err
	}
	return &b.response, nil
}
func (*concurrentApprovalBilling) CancelEnrollmentPayment(context.Context, uuid.UUID) (*billing.CancelResponse, error) {
	return nil, nil
}

func TestSelectedPrivateApprovalPostgres(t *testing.T) {
	for _, scenario := range []string{"same selection", "different selections", "invoice recovery"} {
		t.Run(scenario, func(t *testing.T) {
			db := privateWorkflowPG(t, "KEL108_TEST_DSN")
			tenant, parent, student, class, category := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			for _, statement := range []struct {
				query string
				args  []any
			}{
				{"INSERT INTO categories(id,tenant_id,name) VALUES(?,?,'Subset')", []any{category, tenant}},
				{"INSERT INTO classes(id,tenant_id,category_id,name,type,price,is_published,enrollment_status,private_slot_limit) VALUES(?,?,?,'Subset','private',100000,true,'open',2)", []any{class, tenant, category}},
				{"INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'Subset')", []any{student, parent}},
			} {
				if err := db.Exec(statement.query, statement.args...).Error; err != nil {
					t.Fatal(err)
				}
			}
			ctx := domain.WithPrivateChatActor(context.Background(), domain.PrivateChatActor{UserID: parent, IsParent: true})
			requests := repository.NewPrivateScheduleRequestRepositoryWithChat(db)
			request := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: tenant, ParentID: parent, StudentID: student, ClassID: class, ParentEmail: "parent@example.test", BillingCycle: "monthly", Status: "pending", Slots: []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}, {DayOfWeek: 3, StartTime: "12:00:00", EndTime: "13:00:00"}, {DayOfWeek: 5, StartTime: "14:00:00", EndTime: "15:00:00"}}}
			if err := requests.Create(ctx, request); err != nil {
				t.Fatal(err)
			}
			ctx = domain.WithPrivateChatActor(context.Background(), domain.PrivateChatActor{UserID: uuid.New(), TenantID: tenant, RoleID: uuid.New(), MemberID: uuid.New()})
			invoice := &concurrentApprovalBilling{response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/subset"}}
			uc := NewPrivateScheduleRequestUsecase(requests, repository.NewStudentRepository(db), repository.NewClassRepository(db), repository.NewTransactionManager(db), repository.NewEnrollmentRepository(db), repository.NewScheduleRepository(db), repository.NewSessionRepository(db), invoice)
			limit := 2
			approval := &domain.ApprovePrivateScheduleRequest{SelectedSlotIndexes: []int{1}, ProposalSnapshot: request.Slots, ExpectedPrivateSlotLimit: &limit}
			if scenario == "invoice recovery" {
				invoice.err = errors.New("timeout")
				if _, err := uc.Approve(ctx, tenant, request.ID, approval); err == nil {
					t.Fatal("missing invoice failure")
				}
				invoice.err = nil
			}
			var workers sync.WaitGroup
			errorsFound := make(chan error, 8)
			for index := 0; index < 8; index++ {
				choice := *approval
				if scenario == "different selections" && index%2 == 0 {
					choice.SelectedSlotIndexes = []int{0, 2}
				}
				workers.Add(1)
				go func() {
					defer workers.Done()
					_, err := uc.Approve(ctx, tenant, request.ID, &choice)
					errorsFound <- err
				}()
			}
			workers.Wait()
			close(errorsFound)
			conflicts := 0
			for err := range errorsFound {
				if errors.Is(err, domain.ErrPrivateSelectionConflict) {
					conflicts++
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "different selections" && conflicts != 4 {
				t.Fatalf("conflicts=%d", conflicts)
			}
			stored, err := requests.Get(ctx, request.ID, &tenant, nil)
			if err != nil || len(stored.Slots) != 3 || len(stored.ApprovedSlots) == 0 {
				t.Fatal("lost decision", err)
			}
			var enrollmentCount, scheduleCount, unselectedSessions int64
			db.Table("enrollments").Where("class_id=? AND gross_amount=100000", class).Count(&enrollmentCount)
			db.Table("class_schedules").Where("class_id=?", class).Count(&scheduleCount)
			if enrollmentCount != 1 || scheduleCount != int64(len(stored.ApprovedSlots)) {
				t.Fatal("duplicate or wrong artifacts", enrollmentCount, scheduleCount)
			}
			var event domain.PrivateChatEvent
			if err := db.Where("request_id=? AND kind='approved'", request.ID).First(&event).Error; err != nil || !samePrivateSlots(event.Slots, stored.ApprovedSlots) {
				t.Fatal("chat exposes discarded proposals", err)
			}
			days := make([]int, 0, len(stored.ApprovedSlots))
			for _, slot := range stored.ApprovedSlots {
				days = append(days, slot.DayOfWeek)
			}
			if err := db.Table("class_sessions").Where("class_id=? AND EXTRACT(ISODOW FROM session_date) NOT IN ?", class, days).Count(&unselectedSessions).Error; err != nil {
				t.Fatal(err)
			}
			if unselectedSessions != 0 {
				t.Fatal("unselected sessions generated")
			}
		})
	}
}
