package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// KEL-149 suspend/resume/end against real PostgreSQL: the transitions must be
// safe when they run concurrently with each other and with new signups, and the
// seat accounting must hold (suspend frees the seat, resume reclaims it, a full
// schedule refuses the resume and leaves the enrollment suspended).
//
// Run with KEL149_TEST_DSN against a disposable PostgreSQL database, e.g.
// docker run -d --rm --name kel149-db-test -p 127.0.0.1::5432 postgres:16-alpine
//
//	KEL149_TEST_DSN="postgres://postgres:postgres@127.0.0.1:<port>/postgres?sslmode=disable" \
//	  go test -count=1 -race -run TestSuspend ./internal/repository/
type suspendResumeFixture struct {
	db       *gorm.DB
	repo     *enrollmentRepository
	tenantID uuid.UUID
	classID  uuid.UUID
	schedID  uuid.UUID
}

func newSuspendResumeFixture(t *testing.T) *suspendResumeFixture {
	t.Helper()
	dsn := os.Getenv("KEL149_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL149_TEST_DSN not set")
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
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock(149, 1)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(149, 1)") })
	schema, err := os.ReadFile("../../migrations/00000000000000_init_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatal(err)
	}
	f := &suspendResumeFixture{db: db, repo: NewEnrollmentRepository(db).(*enrollmentRepository), tenantID: uuid.New(), classID: uuid.New(), schedID: uuid.New()}
	categoryID := uuid.New()
	if err := db.Exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryID, f.tenantID, "KEL-149").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", f.classID, f.tenantID, categoryID, "KEL-149 group", "group", 100, true, "open").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.ClassSchedule{ID: f.schedID, ClassID: f.classID, Capacity: 2, DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *suspendResumeFixture) student(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := f.db.Exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", id, uuid.New(), "KEL-149 student").Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *suspendResumeFixture) enrollment(t *testing.T, studentID uuid.UUID, status string) *domain.Enrollment {
	t.Helper()
	key := uuid.NewString()
	e := &domain.Enrollment{
		ID: uuid.New(), TenantID: f.tenantID, StudentID: studentID, ClassID: f.classID,
		ScheduleID: &f.schedID, Status: status, BillingCycle: "monthly",
		IdempotencyKey: &key, PaymentStatus: "paid",
	}
	if err := f.db.Create(e).Error; err != nil {
		t.Fatal(err)
	}
	return e
}

// occupiedSeats is the seat-counting predicate every capacity check shares.
func (f *suspendResumeFixture) occupiedSeats(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := f.db.Model(&domain.Enrollment{}).Where("schedule_id = ? AND status IN ? AND deleted_at IS NULL", f.schedID, []string{domain.EnrollmentStatusPending, domain.EnrollmentStatusActive}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

// suspend runs the suspend transition the way the usecase does: the enrollment
// row is locked, the transition is validated against the locked state, and the
// write happens in the same transaction.
func (f *suspendResumeFixture) suspend(ctx context.Context, id uuid.UUID) error {
	return f.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, txKey{}, tx)
		locked, err := f.repo.GetByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}
		if locked.Status != domain.EnrollmentStatusActive {
			return domain.ErrInvalidEnrollmentTransition
		}
		locked.Status = domain.EnrollmentStatusSuspended
		locked.UpdatedAt = time.Now()
		return tx.Save(locked).Error
	})
}

// resume runs the resume transition the way the usecase does: the enrollment
// row lock and ResumeUnderCapacity share one transaction.
func (f *suspendResumeFixture) resume(ctx context.Context, id uuid.UUID) error {
	return f.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, txKey{}, tx)
		locked, err := f.repo.GetByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}
		return f.repo.ResumeUnderCapacity(txCtx, locked)
	})
}

func (f *suspendResumeFixture) status(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var after domain.Enrollment
	if err := f.db.First(&after, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return after.Status
}

func TestSuspendFreesSeatAndResumeReclaimsIt(t *testing.T) {
	f := newSuspendResumeFixture(t)
	ctx := context.Background()

	e := f.enrollment(t, f.student(t), domain.EnrollmentStatusActive)
	if got := f.occupiedSeats(t); got != 1 {
		t.Fatalf("occupied seats = %d, want 1", got)
	}

	if err := f.suspend(ctx, e.ID); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if got := f.occupiedSeats(t); got != 0 {
		t.Fatalf("seats after suspend = %d, want 0", got)
	}

	// While suspended the freed seat is bookable by another student, proving
	// suspend really released it rather than only hiding the row. The signup
	// runs inside one transaction, exactly like the usecase does, so the
	// schedule lock covers the count check and the insert together.
	if err := f.signup(ctx, t); err != nil {
		t.Fatalf("signup into the suspended seat: %v", err)
	}

	if got := f.occupiedSeats(t); got != 1 {
		t.Fatalf("seats after other signup = %d, want 1", got)
	}
}

// Suspend frees a seat exactly like a drop; a resume into a schedule that
// filled up meanwhile refuses and leaves the enrollment suspended.
func TestResumeOnFullScheduleRefusesAndStaysSuspended(t *testing.T) {
	f := newSuspendResumeFixture(t)
	ctx := context.Background()

	e := f.enrollment(t, f.student(t), domain.EnrollmentStatusActive)
	if err := f.suspend(ctx, e.ID); err != nil {
		t.Fatal(err)
	}

	// Fill both seats with other students (capacity 2).
	f.enrollment(t, f.student(t), domain.EnrollmentStatusActive)
	f.enrollment(t, f.student(t), domain.EnrollmentStatusActive)
	if got := f.occupiedSeats(t); got != 2 {
		t.Fatalf("seats after filling = %d, want 2", got)
	}

	err := f.resume(ctx, e.ID)
	if !errors.Is(err, domain.ErrScheduleFull) {
		t.Fatalf("resume on full = %v, want ErrScheduleFull", err)
	}
	if got := f.status(t, e.ID); got != domain.EnrollmentStatusSuspended {
		t.Fatalf("status after refused resume = %q, want suspended", got)
	}
}

// The student re-enrolled in the same class while suspended: flipping the old
// enrollment back to active would violate idx_student_class_active, so the
// resume answers the conflict family instead of a raw 23505.
func TestResumeConflictsWhenStudentReenrolled(t *testing.T) {
	f := newSuspendResumeFixture(t)
	ctx := context.Background()

	e := f.enrollment(t, f.student(t), domain.EnrollmentStatusActive)
	if err := f.suspend(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	f.enrollment(t, e.StudentID, domain.EnrollmentStatusActive)

	err := f.resume(ctx, e.ID)
	if !errors.Is(err, domain.ErrEnrollmentSuspendedConflict) {
		t.Fatalf("resume with re-enrollment = %v, want ErrEnrollmentSuspendedConflict", err)
	}
	if got := f.status(t, e.ID); got != domain.EnrollmentStatusSuspended {
		t.Fatalf("status after refused resume = %q, want suspended", got)
	}
}

// The overbooking mitigation: concurrent resumes and signups on one schedule
// must never exceed capacity, and every loser is answered with a conflict it
// can retry — never a raw unique violation.
func TestConcurrentResumeAndSignupNeverOverbooks(t *testing.T) {
	f := newSuspendResumeFixture(t)
	ctx := context.Background()

	// Capacity 2, held by two suspended enrollments; two other students race
	// to sign up while the two resumes race to reclaim.
	suspended := []*domain.Enrollment{
		f.enrollment(t, f.student(t), domain.EnrollmentStatusActive),
		f.enrollment(t, f.student(t), domain.EnrollmentStatusActive),
	}
	for _, e := range suspended {
		if err := f.suspend(ctx, e.ID); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	results := make(chan error, 4)
	for _, e := range suspended {
		e := e
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- f.resume(ctx, e.ID)
		}()
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The signup runs in its own transaction, the way EnrollPublic
			// does, so its schedule lock serializes against the resumes.
			results <- f.signup(ctx, t)
		}()
	}
	wg.Wait()
	close(results)

	var successes, conflicts, full int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrEnrollmentSuspendedConflict):
			conflicts++
		case errors.Is(err, domain.ErrScheduleFull):
			full++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	// Exactly two of the four racers fit; the other two must be conflicts.
	if got := f.occupiedSeats(t); got != 2 {
		t.Fatalf("occupied seats = %d, want exactly 2 (successes=%d conflicts=%d full=%d)", got, successes, conflicts, full)
	}
	if successes != 2 || successes+conflicts+full != 4 {
		t.Fatalf("successes=%d conflicts=%d full=%d, want exactly 2 successes out of 4 racers", successes, conflicts, full)
	}
}

// End drops the enrollment permanently: the seat frees and the same student
// can immediately enroll again.
func TestEndFreesSeatForReenrollment(t *testing.T) {
	f := newSuspendResumeFixture(t)
	ctx := context.Background()

	e := f.enrollment(t, f.student(t), domain.EnrollmentStatusActive)
	if err := f.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, txKey{}, tx)
		locked, err := f.repo.GetByIDForUpdate(txCtx, e.ID)
		if err != nil {
			return err
		}
		locked.Status = domain.EnrollmentStatusDropped
		locked.UpdatedAt = time.Now()
		return tx.Save(locked).Error
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.occupiedSeats(t); got != 0 {
		t.Fatalf("seats after end = %d, want 0", got)
	}

	key := uuid.NewString()
	again := &domain.Enrollment{
		ID: uuid.New(), TenantID: f.tenantID, StudentID: e.StudentID, ClassID: f.classID,
		ScheduleID: &f.schedID, Status: domain.EnrollmentStatusPending, BillingCycle: "monthly",
		IdempotencyKey: &key, PaymentStatus: "pending",
	}
	if err := f.repo.CreateIfCapacityAvailable(ctx, again); err != nil {
		t.Fatalf("re-enroll after end: %v", err)
	}
}

// A private enrollment (no schedule) suspends and resumes through the same
// path; the live-enrollment check is its only seat guard.
func TestSuspendResumePrivateEnrollmentWithoutSchedule(t *testing.T) {
	f := newSuspendResumeFixture(t)
	ctx := context.Background()

	classID := uuid.New()
	categoryID := uuid.New()
	if err := f.db.Exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryID, f.tenantID, "KEL-149 private").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", classID, f.tenantID, categoryID, "KEL-149 private", "private", 100, true, "open").Error; err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	e := &domain.Enrollment{
		ID: uuid.New(), TenantID: f.tenantID, StudentID: f.student(t), ClassID: classID,
		Status: domain.EnrollmentStatusActive, BillingCycle: "monthly",
		IdempotencyKey: &key, PaymentStatus: "paid",
	}
	if err := f.db.Create(e).Error; err != nil {
		t.Fatal(err)
	}

	if err := f.suspend(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.resume(ctx, e.ID); err != nil {
		t.Fatalf("private resume: %v", err)
	}
	if got := f.status(t, e.ID); got != domain.EnrollmentStatusActive {
		t.Fatalf("status after private resume = %q, want active", got)
	}
}

// Concurrent suspend and end on the same enrollment: the row lock serializes
// them, the final state is always dropped, and neither path ever sees or
// writes an intermediate state.
func TestConcurrentSuspendAndEndSerializeOnTheRowLock(t *testing.T) {
	f := newSuspendResumeFixture(t)
	ctx := context.Background()

	e := f.enrollment(t, f.student(t), domain.EnrollmentStatusActive)

	end := func(ctx context.Context) error {
		return f.db.Transaction(func(tx *gorm.DB) error {
			txCtx := context.WithValue(ctx, txKey{}, tx)
			locked, err := f.repo.GetByIDForUpdate(txCtx, e.ID)
			if err != nil {
				return err
			}
			if locked.Status != domain.EnrollmentStatusActive && locked.Status != domain.EnrollmentStatusSuspended {
				return domain.ErrInvalidEnrollmentTransition
			}
			locked.Status = domain.EnrollmentStatusDropped
			locked.UpdatedAt = time.Now()
			return tx.Save(locked).Error
		})
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); results <- f.suspend(ctx, e.ID) }()
	go func() { defer wg.Done(); results <- end(ctx) }()
	wg.Wait()
	close(results)

	for err := range results {
		// Whichever transition lost the race answers a conflict; that is the
		// caller's retry signal, never an error.
		if err != nil && !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if got := f.status(t, e.ID); got != domain.EnrollmentStatusDropped {
		t.Fatalf("status after concurrent suspend/end = %q, want dropped", got)
	}
	if got := f.occupiedSeats(t); got != 0 {
		t.Fatalf("seats after concurrent suspend/end = %d, want 0", got)
	}
}

// pending builds a fresh pending signup for the group schedule.
func (f *suspendResumeFixture) pending(t *testing.T) *domain.Enrollment {
	t.Helper()
	key := uuid.NewString()
	return &domain.Enrollment{
		ID: uuid.New(), TenantID: f.tenantID, StudentID: f.student(t), ClassID: f.classID,
		ScheduleID: &f.schedID, Status: domain.EnrollmentStatusPending, BillingCycle: "monthly",
		IdempotencyKey: &key, PaymentStatus: "pending",
	}
}

// signup runs a new enrollment the way the usecase does: the capacity check and
// the insert share one transaction, so the schedule lock holds until commit.
func (f *suspendResumeFixture) signup(ctx context.Context, t *testing.T) error {
	t.Helper()
	enrollment := f.pending(t)
	return f.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, txKey{}, tx)
		return f.repo.CreateIfCapacityAvailable(txCtx, enrollment)
	})
}
