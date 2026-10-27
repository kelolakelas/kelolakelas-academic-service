package usecase

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/privatechat"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func privateChatPG(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("KEL226_TEST_DSN")
	if dsn == "" {
		t.Skip("set KEL226_TEST_DSN to a disposable PostgreSQL database")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { admin.Close() })
	schema := "kel226_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	cfg.RuntimeParams["search_path"] = schema + ",public"
	cfg.RuntimeParams["timezone"] = "UTC"
	pool := stdlib.OpenDB(*cfg)
	pool.SetMaxOpenConns(12)
	t.Cleanup(func() { pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("../../migrations/*.up.sql")
	sort.Strings(files)
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sql := strings.ReplaceAll(string(raw), "CREATE EXTENSION IF NOT EXISTS pgcrypto;", "")
		if err = db.Exec(sql).Error; err != nil {
			t.Fatalf("migration%s: %v", file, err)
		}
	}
	return db
}

type privateChatBill struct{ calls atomic.Int32 }

func (b *privateChatBill) GenerateInvoice(_ context.Context, in billing.InvoiceRequest) (*billing.InvoiceResponse, error) {
	b.calls.Add(1)
	return &billing.InvoiceResponse{TransactionID: in.EnrollmentID, CheckoutSessionURL: "https://pay.example.test/private"}, nil
}
func (*privateChatBill) CancelEnrollmentPayment(context.Context, uuid.UUID) (*billing.CancelResponse, error) {
	return nil, errors.New("unused")
}

type privateInvoiceFixture struct {
	status string
	expiry time.Time
	err    error
}

func (f *privateInvoiceFixture) Invoice(_ context.Context, s privatechat.InvoiceScope) (*privatechat.Invoice, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &privatechat.Invoice{ID: s.TransactionID, EnrollmentID: s.EnrollmentID, TenantID: s.TenantID, ParentID: s.ParentID, StudentID: s.StudentID, Status: f.status, InvoiceExpiresAt: &f.expiry, CheckoutSessionURL: "https://pay.example.test/private"}, nil
}

type privateDeliveryFixture struct {
	fail  bool
	keys  map[uuid.UUID]bool
	mu    sync.Mutex
	calls int
}

func (s *privateDeliveryFixture) Deliver(_ context.Context, in privatechat.Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.fail {
		return errors.New("chat outage")
	}
	s.keys[in.EventID] = true
	return nil
}
func TestKEL226PrivateChatTransactionalPostgres(t *testing.T) {
	db := privateChatPG(t)
	ctx := context.Background()
	tenant, parent, adminID, role, member, category, class, student := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	must := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	must("INSERT INTO categories(id,tenant_id,name) VALUES(?,?,'Private')", category, tenant)
	must("INSERT INTO classes(id,tenant_id,category_id,name,type,price,is_published,enrollment_status) VALUES(?,?,?,'Private fixture','private',100000,true,'open')", class, tenant, category)
	must("INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'Fixture student')", student, parent)
	parentCtx := domain.WithPrivateChatActor(ctx, domain.PrivateChatActor{UserID: parent, IsParent: true})
	adminCtx := domain.WithPrivateChatActor(ctx, domain.PrivateChatActor{UserID: adminID, TenantID: tenant, RoleID: role, MemberID: member})
	requests := repository.NewPrivateScheduleRequestRepositoryWithChat(db)
	tx := repository.NewTransactionManager(db)
	bill := &privateChatBill{}
	u := NewPrivateScheduleRequestUsecase(requests, repository.NewStudentRepository(db), repository.NewClassRepository(db), tx, repository.NewEnrollmentRepository(db), repository.NewScheduleRepository(db), repository.NewSessionRepository(db), bill)
	input := &domain.CreatePrivateScheduleRequest{StudentID: student, BillingCycle: "monthly", PaymentMethod: "BC", ParentEmail: "parent@example.test", Slots: []domain.PrivateScheduleSlot{{DayOfWeek: 3, StartTime: "10:00:00", EndTime: "11:00:00"}, {DayOfWeek: 5, StartTime: "14:00:00", EndTime: "15:00:00"}}}
	if _, err := u.Create(ctx, parent, class, input); !errors.Is(err, domain.ErrPrivateChatActor) {
		t.Fatalf("unverified actor accepted: %v", err)
	}
	rollback := errors.New("fixture rollback")
	err := tx.WithTransaction(parentCtx, func(c context.Context) error {
		raw := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: tenant, ClassID: class, StudentID: student, ParentID: parent, ParentEmail: input.ParentEmail, BillingCycle: input.BillingCycle, PaymentMethod: input.PaymentMethod, Slots: input.Slots, Status: "pending"}
		if err := requests.Create(c, raw); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var count int64
	db.Table("private_request_chat_events").Count(&count)
	if count != 0 {
		t.Fatal("rolled back event survived")
	}
	db.Table("private_schedule_requests").Count(&count)
	if count != 0 {
		t.Fatal("rolled back request survived")
	}
	req, err := u.Create(parentCtx, parent, class, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = u.Create(parentCtx, parent, class, input); !errors.Is(err, domain.ErrPrivateRequestConflict) {
		t.Fatalf("request retry: %v", err)
	}
	var created domain.PrivateChatEvent
	db.Where("request_id=? AND kind='created'", req.ID).First(&created)
	if created.ActorUserID != parent || created.ActorKind != "parent" || len(created.Slots) != 2 {
		t.Fatal("parent attribution")
	}
	first, err := u.Approve(adminCtx, tenant, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	var approved domain.PrivateChatEvent
	db.Where("request_id=? AND kind='approved'", req.ID).First(&approved)
	if approved.ActorUserID != adminID || approved.ActorKind != "member" || approved.ActorMemberID == nil || *approved.ActorMemberID != member {
		t.Fatal("admin attribution")
	}
	otherCtx := domain.WithPrivateChatActor(ctx, domain.PrivateChatActor{UserID: uuid.New(), TenantID: tenant, RoleID: role, MemberID: uuid.New()})
	for range 3 {
		again, err := u.Approve(otherCtx, tenant, req.ID)
		if err != nil || again.Enrollment.ID != first.Enrollment.ID {
			t.Fatalf("approval replay: %v", err)
		}
	}
	db.Table("private_request_chat_events").Where("request_id=?", req.ID).Count(&count)
	if count != 2 {
		t.Fatal("duplicate lifecycle event")
	}
	db.Table("class_schedules").Where("enrollment_id=?", first.Enrollment.ID).Count(&count)
	if count != 2 {
		t.Fatal("duplicate schedules")
	}
	invoices := &privateInvoiceFixture{status: "pending", expiry: time.Now().UTC().Add(time.Hour)}
	store := repository.NewPrivateChatStore(db)
	events := &PrivateChatEvents{Store: store, Invoices: invoices}
	p, err := events.Projection(ctx, approved.ID)
	if err != nil || p.InvoiceExpiresAt == nil || !strings.Contains(p.Body, "UTC+07:00") || !strings.Contains(p.Body, "Rabu") || !strings.Contains(p.Body, "Jumat") {
		t.Fatalf("approval projection %+v %v", p, err)
	}
	for _, status := range []string{"paid", "expired", "cancelled", "refunded"} {
		invoices.status = status
		p, err := events.Projection(ctx, approved.ID)
		if err != nil || p.InvoiceExpiresAt != nil || strings.Contains(p.Body, "https://") {
			t.Fatalf("nonpayable %s: %+v %v", status, p, err)
		}
	}
	invoices.status = "pending"
	invoices.expiry = time.Now().UTC().Add(-time.Second)
	p, err = events.Projection(ctx, approved.ID)
	if err != nil || p.InvoiceExpiresAt != nil || strings.Contains(p.Body, "https://") {
		t.Fatal("expired pending invoice link")
	}
	invoices.err = privatechat.ErrMissingInvoice
	if _, err = events.Projection(ctx, approved.ID); !errors.Is(err, privatechat.ErrMissingInvoice) {
		t.Fatal("missing invoice silently accepted")
	}
	invoices.err = nil
	// A failed parent event holds later approval delivery until the owed event
	// recovers; a restarted worker reuses the same immutable event IDs.
	sender := &privateDeliveryFixture{fail: true, keys: map[uuid.UUID]bool{}}
	worker := &PrivateChatWorker{Store: store, Sender: sender}
	if err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 {
		t.Fatal("event ordering lost")
	}
	must("UPDATE private_request_chat_outbox SET available_at=now()-interval '1 second',attempts=11 WHERE event_id=?", created.ID)
	if err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Diagnostics(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatal("diagnostics")
	}
	if err = store.Requeue(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	sender.fail = false
	worker = &PrivateChatWorker{Store: store, Sender: sender}
	if err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sender.keys) != 2 {
		t.Fatal("owed events not recovered")
	}
	// Lease fencing: stale completion cannot overwrite a newer claim.
	must("UPDATE private_request_chat_outbox SET status='pending',available_at=now(),attempts=0 WHERE event_id=?", created.ID)
	claim, err := store.Claim(ctx)
	if err != nil || claim == nil {
		t.Fatal("claim")
	}
	must("UPDATE private_request_chat_outbox SET available_at=now()-interval '1 second' WHERE event_id=?", created.ID)
	newClaim, err := store.Claim(ctx)
	if err != nil || newClaim == nil || newClaim.LeaseToken == claim.LeaseToken {
		t.Fatal("restart claim")
	}
	if err = store.Finish(ctx, claim, "sent", "", 0); err != nil {
		t.Fatal(err)
	}
	var status string
	db.Table("private_request_chat_outbox").Where("event_id=?", created.ID).Select("status").Scan(&status)
	if status != "sending" {
		t.Fatal("stale lease won")
	}
	// Rejection retains the proposing admin; recommendation acceptance records
	// the actual parent (including legacy rows), a direct member cannot spoof a parent transition.
	student2 := uuid.New()
	must("INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'Second')", student2, parent)
	input2 := *input
	input2.StudentID = student2
	r2, err := u.Create(parentCtx, parent, class, &input2)
	if err != nil {
		t.Fatal(err)
	}
	reason := "Try the recommended slots"
	recommend := &domain.RejectPrivateScheduleRequest{Reason: &reason, RecommendedSlots: input.Slots}
	if _, err = u.Reject(adminCtx, tenant, r2.ID, recommend); err != nil {
		t.Fatal(err)
	}
	var rejected domain.PrivateChatEvent
	db.Where("request_id=? AND kind='rejected'", r2.ID).First(&rejected)
	rp, err := events.Projection(ctx, rejected.ID)
	if err != nil || strings.Contains(rp.Body, "https://") || !strings.Contains(rp.Body, reason) || !strings.Contains(rp.Body, "Rekomendasi") {
		t.Fatal("reject projection")
	}
	if _, err = u.AcceptRecommendation(parentCtx, parent, r2.ID); err != nil {
		t.Fatal(err)
	}
	var accepted domain.PrivateChatEvent
	db.Where("request_id=? AND kind='recommendation_accepted'", r2.ID).First(&accepted)
	if accepted.ActorUserID != parent || accepted.ActorKind != "parent" || accepted.TransitionUserID != parent {
		t.Fatal("recommendation actor changed")
	}
	// Legacy recommendations carry no historical event/admin snapshot. Acceptance
	// still records the authenticated parent without fabricating a member actor.
	legacyStudent := uuid.New()
	must("INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'Legacy')", legacyStudent, parent)
	legacy := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: tenant, ClassID: class, StudentID: legacyStudent, ParentID: parent, ParentEmail: input.ParentEmail, BillingCycle: input.BillingCycle, PaymentMethod: input.PaymentMethod, Slots: input.Slots, Status: "pending"}
	oldRepo := repository.NewPrivateScheduleRequestRepository(db)
	if err := oldRepo.Create(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := oldRepo.RejectWithRecommendation(ctx, legacy.ID, tenant, &reason, input.Slots); err != nil {
		t.Fatal(err)
	}
	if _, err := u.AcceptRecommendation(parentCtx, parent, legacy.ID); err != nil {
		t.Fatal(err)
	}
	var legacyEvent domain.PrivateChatEvent
	if err := db.Where("request_id=?", legacy.ID).First(&legacyEvent).Error; err != nil {
		t.Fatal(err)
	}
	if legacyEvent.ActorKind != "parent" || legacyEvent.ActorUserID != parent || legacyEvent.Kind != "recommendation_accepted" {
		t.Fatal("legacy actor fabricated")
	}

	// Concurrent decisions produce one persisted approval, enrollment and slot set.
	concurrentStudent := uuid.New()
	must("INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'Concurrent')", concurrentStudent, parent)
	concurrentInput := *input
	concurrentInput.StudentID = concurrentStudent
	concurrentReq, err := u.Create(parentCtx, parent, class, &concurrentInput)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errorsCh := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := u.Approve(adminCtx, tenant, concurrentReq.ID); errorsCh <- err }()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	db.Table("private_request_chat_events").Where("request_id=? AND kind='approved'", concurrentReq.ID).Count(&count)
	if count != 1 {
		t.Fatal("concurrent duplicate approval")
	}
	var concurrentEvent domain.PrivateChatEvent
	db.Where("request_id=? AND kind='approved'", concurrentReq.ID).First(&concurrentEvent)
	var concurrentEnrollment domain.Enrollment
	db.Where("idempotency_key=?", "private-request:"+concurrentReq.ID.String()).First(&concurrentEnrollment)
	db.Table("class_schedules").Where("enrollment_id=?", concurrentEnrollment.ID).Count(&count)
	if count != 2 {
		t.Fatal("concurrent duplicate schedules")
	}

	// Reopened provisional decisions cannot dispatch an old approval. A later
	// deciding admin receives a fresh event version and keeps the same enrollment.
	if err := requests.SetStatus(adminCtx, concurrentReq.ID, tenant, "pending"); err != nil {
		t.Fatal(err)
	}
	var priorStatus string
	db.Table("private_request_chat_outbox").Where("event_id=?", concurrentEvent.ID).Select("status").Scan(&priorStatus)
	if priorStatus != "superseded" {
		t.Fatal("reopened approval still dispatchable")
	}
	if err := requests.SetStatus(otherCtx, concurrentReq.ID, tenant, "approved"); err != nil {
		t.Fatal(err)
	}
	var latest domain.PrivateChatEvent
	db.Where("request_id=? AND kind='approved'", concurrentReq.ID).Order("version DESC").First(&latest)
	otherActor, _ := domain.PrivateChatActorFrom(otherCtx)
	if latest.Version <= concurrentEvent.Version || latest.ActorUserID != otherActor.UserID {
		t.Fatal("reapproval actor/version stale")
	}
	// Capture failures roll back the business transition as one transaction.
	student3 := uuid.New()
	must("INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'Third')", student3, parent)
	input3 := *input
	input3.StudentID = student3
	r3, err := u.Create(parentCtx, parent, class, &input3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = u.Reject(ctx, tenant, r3.ID, &domain.RejectPrivateScheduleRequest{}); !errors.Is(err, domain.ErrPrivateChatActor) {
		t.Fatalf("unverified decision %v", err)
	}
	saved, err := requests.Get(ctx, r3.ID, &tenant, nil)
	if err != nil || saved.Status != "pending" {
		t.Fatal("decision committed without event")
	}
}
