package usecase

import (
	"context"
	"errors"
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

// KEL115_TEST_DSN must point to a disposable, isolated database.
func TestPrivateRecommendationPostgresParallelAndIsolation(t *testing.T) {
	dsn := os.Getenv("KEL115_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL115_TEST_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range []string{"00000000000000_init_schema.up.sql", "00001790600000_private_schedule_requests.up.sql", "00001790600001_private_schedule_recommendations.up.sql"} {
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
	ctx := context.Background()
	repo := repository.NewPrivateScheduleRequestRepository(db)
	requested := domain.PrivateScheduleSlot{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}
	recommended := domain.PrivateScheduleSlot{DayOfWeek: 3, StartTime: "14:00:00", EndTime: "15:00:00"}
	req := &domain.PrivateScheduleRequest{ID: requestID, TenantID: tenant, ParentID: parent, StudentID: studentID, ClassID: classID, ParentEmail: "verified@example.test", BillingCycle: "monthly", Slots: []domain.PrivateScheduleSlot{requested}, Status: "pending"}
	if err := repo.Create(ctx, req); err != nil {
		t.Fatal(err)
	}
	bill := &concurrentApprovalBilling{response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/session"}}
	u := NewPrivateScheduleRequestUsecase(repo, repository.NewStudentRepository(db), repository.NewClassRepository(db), repository.NewTransactionManager(db), repository.NewEnrollmentRepository(db), repository.NewScheduleRepository(db), repository.NewSessionRepository(db), bill)
	reason := "Try Wednesday"
	if _, err := u.Reject(ctx, uuid.New(), requestID, &domain.RejectPrivateScheduleRequest{RecommendedSlots: []domain.PrivateScheduleSlot{recommended}}); !errors.Is(err, domain.ErrPrivateRequestNotFound) {
		t.Fatalf("foreign tenant reject=%v", err)
	}
	if _, err := u.Reject(ctx, tenant, requestID, &domain.RejectPrivateScheduleRequest{Reason: &reason, RecommendedSlots: []domain.PrivateScheduleSlot{recommended}}); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Get(ctx, requestID, nil, &uuid.UUID{}); !errors.Is(err, domain.ErrPrivateRequestNotFound) {
		t.Fatalf("foreign parent get=%v", err)
	}
	for _, foreign := range []uuid.UUID{uuid.New(), uuid.Nil} {
		if _, err := u.AcceptRecommendation(ctx, foreign, requestID); !errors.Is(err, domain.ErrPrivateRequestNotFound) {
			t.Fatalf("foreign parent accept=%v", err)
		}
		if _, err := u.DeclineRecommendation(ctx, foreign, requestID); !errors.Is(err, domain.ErrPrivateRequestNotFound) {
			t.Fatalf("foreign parent decline=%v", err)
		}
	}
	got, err := u.Get(ctx, requestID, nil, &parent)
	if err != nil || got.Status != "rejected" || got.RejectionReason == nil || *got.RejectionReason != reason || len(got.RecommendedSlots) != 1 || got.RecommendedSlots[0] != recommended {
		t.Fatalf("parent read=%+v err=%v", got, err)
	}
	items, err := u.List(ctx, nil, &parent, "rejected")
	if err != nil || len(items) != 1 || len(items[0].RecommendedSlots) != 1 {
		t.Fatalf("parent list=%+v err=%v", items, err)
	}
	const workers = 8
	var wg sync.WaitGroup
	out := make(chan *domain.PublicEnrollmentResponse, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := u.AcceptRecommendation(ctx, parent, requestID)
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
			t.Fatalf("parallel accept: %v", err)
		}
	}
	for result := range out {
		if result == nil || result.Payment.CheckoutSessionURL != bill.response.CheckoutSessionURL {
			t.Fatalf("checkout=%+v", result)
		}
		if first == uuid.Nil {
			first = result.Enrollment.ID
		} else if result.Enrollment.ID != first {
			t.Fatalf("different enrollments %s and %s", first, result.Enrollment.ID)
		}
	}
	for _, q := range []struct {
		table string
		want  int64
	}{{"enrollments", 1}, {"class_schedules", 1}} {
		var n int64
		if err := db.Table(q.table).Where("class_id = ?", classID).Count(&n).Error; err != nil || n != q.want {
			t.Fatalf("%s count=%d err=%v", q.table, n, err)
		}
	}
	var schedule domain.ClassSchedule
	if err := db.Where("enrollment_id = ?", first).First(&schedule).Error; err != nil || schedule.DayOfWeek != recommended.DayOfWeek || schedule.StartTime != recommended.StartTime || schedule.EndTime != recommended.EndTime || schedule.Capacity != 1 {
		t.Fatalf("schedule=%+v err=%v", schedule, err)
	}
	if _, err := u.DeclineRecommendation(ctx, parent, requestID); !errors.Is(err, domain.ErrPrivateRequestTransition) {
		t.Fatalf("decline after accept=%v", err)
	}
	// Billing's confirmation calls the existing internal activation path for this enrollment.
	activator := NewEnrollmentUsecase(repository.NewEnrollmentRepository(db), repository.NewStudentRepository(db), repository.NewClassRepository(db), bill, repository.NewTransactionManager(db))
	if _, err := activator.ActivateEnrollment(ctx, first); err != nil {
		t.Fatal(err)
	}
	var active domain.Enrollment
	if err := db.First(&active, "id = ?", first).Error; err != nil || active.Status != "active" {
		t.Fatalf("activated enrollment=%+v err=%v", active, err)
	}
	if bill.keys < 1 || bill.keys > workers {
		t.Fatalf("invoice attempts=%d", bill.keys)
	}
}

func TestPrivateRecommendationPostgresDeclineRace(t *testing.T) {
	dsn := os.Getenv("KEL115_DECLINE_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL115_DECLINE_TEST_DSN not set")
	}
	// This second isolated database is used to exercise the decline/accept race independently.
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range []string{"00000000000000_init_schema.up.sql", "00001790600000_private_schedule_requests.up.sql", "00001790600001_private_schedule_recommendations.up.sql"} {
		sql, e := os.ReadFile("../../migrations/" + migration)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.Exec(string(sql)).Error; e != nil {
			t.Fatal(e)
		}
	}
	tenant, parent, student, class, category, id := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, stmt := range []struct {
		sql  string
		args []interface{}
	}{
		{"INSERT INTO categories(id,tenant_id,name) VALUES(?,?, 'test')", []interface{}{category, tenant}},
		{"INSERT INTO classes(id,tenant_id,category_id,name,type,price,is_published,enrollment_status) VALUES(?,?,?,'private','private',100,true,'open')", []interface{}{class, tenant, category}},
		{"INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'test')", []interface{}{student, parent}},
	} {
		if e := db.Exec(stmt.sql, stmt.args...).Error; e != nil {
			t.Fatal(e)
		}
	}
	repo := repository.NewPrivateScheduleRequestRepository(db)
	ctx := context.Background()
	if e := repo.Create(ctx, &domain.PrivateScheduleRequest{ID: id, TenantID: tenant, ParentID: parent, StudentID: student, ClassID: class, ParentEmail: "verified@example.test", BillingCycle: "monthly", Slots: []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}}, Status: "pending"}); e != nil {
		t.Fatal(e)
	}
	if _, e := repo.RejectWithRecommendation(ctx, id, tenant, nil, []domain.PrivateScheduleSlot{{DayOfWeek: 2, StartTime: "10:00:00", EndTime: "11:00:00"}}); e != nil {
		t.Fatal(e)
	}
	u := NewPrivateScheduleRequestUsecase(repo, repository.NewStudentRepository(db), repository.NewClassRepository(db), repository.NewTransactionManager(db), repository.NewEnrollmentRepository(db), repository.NewScheduleRepository(db), repository.NewSessionRepository(db), &concurrentApprovalBilling{response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/session"}})
	var wg sync.WaitGroup
	var accepted, declined error
	wg.Add(2)
	go func() { defer wg.Done(); _, accepted = u.AcceptRecommendation(ctx, parent, id) }()
	go func() { defer wg.Done(); _, declined = u.DeclineRecommendation(ctx, parent, id) }()
	wg.Wait()
	if accepted == nil && declined == nil || accepted != nil && declined != nil {
		t.Fatalf("exactly one decision required: accepted=%v declined=%v", accepted, declined)
	}
	if accepted != nil && !errors.Is(accepted, domain.ErrPrivateRequestTransition) || declined != nil && !errors.Is(declined, domain.ErrPrivateRequestTransition) {
		t.Fatalf("unexpected race errors: accept=%v decline=%v", accepted, declined)
	}
	if declined == nil {
		if _, e := u.AcceptRecommendation(ctx, parent, id); !errors.Is(e, domain.ErrPrivateRequestTransition) {
			t.Fatalf("accept after decline=%v", e)
		}
		newRequest := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: tenant, ParentID: parent, StudentID: student, ClassID: class, ParentEmail: "verified@example.test", BillingCycle: "monthly", Slots: []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}}, Status: "pending"}
		if e := repo.Create(ctx, newRequest); e != nil {
			t.Fatalf("parent resubmit after decline=%v", e)
		}
		if _, e := u.AcceptRecommendation(ctx, parent, newRequest.ID); !errors.Is(e, domain.ErrPrivateRequestTransition) {
			t.Fatalf("accept without recommendation=%v", e)
		}
	}
}
