package usecase

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

// Run with KEL21_TEST_DSN against a disposable PostgreSQL database.
func TestSessionGenerationWorkerPostgres(t *testing.T) {
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
	for _, file := range []string{
		"../../migrations/00000000000000_init_schema.up.sql",
		// Applied on top of the current init schema: it must be a no-op there.
		"../../migrations/00001790492411_session_generation_horizon.up.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(schema)).Error; err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}

	ctx := context.Background()
	// Other tests share this database; fence this test's rows off by marking every
	// schedule not owned by it as already generated far ahead.
	fenceOthers := func() {
		if err := db.Exec("UPDATE class_schedules SET sessions_generated_until = '2999-12-31' WHERE class_id NOT IN (SELECT id FROM classes WHERE name LIKE 'KEL90 %')").Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	txManager := repository.NewTransactionManager(db)
	newWorker := func() *SessionGenerationWorker {
		w := NewSessionGenerationWorker(txManager, repository.NewSessionGenerationRepository(db), 1, time.Hour)
		w.now = func() time.Time { return now }
		return w
	}
	tenant := uuid.New()
	category := uuid.New()
	if err := db.Exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, 'Math')", category, tenant).Error; err != nil {
		t.Fatal(err)
	}
	newClass := func(name, kind string) uuid.UUID {
		id := uuid.New()
		if err := db.Exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price) VALUES (?, ?, ?, ?, ?, 100)", id, tenant, category, "KEL90 "+name, kind).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	sessionDates := func(scheduleID uuid.UUID, unscoped bool) []string {
		q := db.Model(&domain.ClassSession{})
		if unscoped {
			q = q.Unscoped()
		}
		var dates []time.Time
		if err := q.Where("schedule_id = ?", scheduleID).Order("session_date").Pluck("session_date", &dates).Error; err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(dates))
		for i, d := range dates {
			out[i] = d.Format("2006-01-02")
		}
		return out
	}
	u := NewScheduleUsecase(txManager, repository.NewClassRepository(db), repository.NewScheduleRepository(db), repository.NewSessionRepository(db), repository.NewEnrollmentRepository(db))

	// A schedule created this month through the API gets this month's sessions only.
	groupClass := newClass("group", "group")
	created, err := u.CreateInitialSchedules(ctx, tenant, &domain.CreateInitialSchedulesRequest{
		ClassID:   groupClass,
		Schedules: []domain.ScheduleItemRequest{{Capacity: 3, DayOfWeek: 1, StartTime: "09:00", EndTime: "10:00", ValidFrom: datePtr(day(9, 7))}},
	})
	if err != nil {
		t.Fatal(err)
	}
	group := created.Schedules[0]
	if got := sessionDates(group.ID, false); len(got) != 4 || got[3] != "2026-09-28" {
		t.Fatalf("initial sessions=%v", got)
	}

	// The tenant deletes the 12 October session and reschedules 19 October once the
	// first pass has generated them; neither may come back.
	endsSoon := newClass("ends", "group")
	until := day(10, 6)
	endingSchedule := &domain.ClassSchedule{ID: uuid.New(), ClassID: endsSoon, Capacity: 1, DayOfWeek: 2, StartTime: "09:00:00", EndTime: "10:00:00", ValidUntil: &until, SessionsGeneratedUntil: datePtr(day(9, 30))}
	deletedSchedule := &domain.ClassSchedule{ID: uuid.New(), ClassID: endsSoon, Capacity: 1, DayOfWeek: 3, StartTime: "09:00:00", EndTime: "10:00:00", SessionsGeneratedUntil: datePtr(day(9, 30))}
	privateClass := newClass("private", "private")
	student := uuid.New()
	if err := db.Exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, 'Private')", student, uuid.New()).Error; err != nil {
		t.Fatal(err)
	}
	dropped := &domain.Enrollment{ID: uuid.New(), TenantID: tenant, StudentID: student, ClassID: privateClass, Status: "dropped", JoinedAt: day(9, 1)}
	live := &domain.Enrollment{ID: uuid.New(), TenantID: tenant, StudentID: student, ClassID: privateClass, Status: "active", JoinedAt: day(9, 1)}
	enrollments := repository.NewEnrollmentRepository(db)
	for _, e := range []*domain.Enrollment{dropped, live} {
		if err := enrollments.Create(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	droppedSchedule := &domain.ClassSchedule{ID: uuid.New(), ClassID: privateClass, EnrollmentID: &dropped.ID, Capacity: 1, DayOfWeek: 4, StartTime: "09:00:00", EndTime: "10:00:00", SessionsGeneratedUntil: datePtr(day(9, 30))}
	liveSchedule := &domain.ClassSchedule{ID: uuid.New(), ClassID: privateClass, EnrollmentID: &live.ID, Capacity: 1, DayOfWeek: 4, StartTime: "11:00:00", EndTime: "12:00:00", SessionsGeneratedUntil: datePtr(day(9, 30))}
	schedules := repository.NewScheduleRepository(db)
	for _, s := range []*domain.ClassSchedule{endingSchedule, deletedSchedule, droppedSchedule, liveSchedule} {
		if err := schedules.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := u.DeleteSchedule(ctx, tenant, deletedSchedule.ID); err != nil {
		t.Fatal(err)
	}
	fenceOthers()

	first := newWorker().RunOnce(ctx)
	if first.Failed != 0 {
		t.Fatalf("first pass=%+v", first)
	}
	wantGroup := []string{"2026-09-07", "2026-09-14", "2026-09-21", "2026-09-28", "2026-10-05", "2026-10-12", "2026-10-19", "2026-10-26"}
	if got := sessionDates(group.ID, false); len(got) != len(wantGroup) || got[4] != "2026-10-05" || got[7] != "2026-10-26" {
		t.Fatalf("group sessions after first pass=%v", got)
	}
	if got := sessionDates(endingSchedule.ID, false); len(got) != 1 || got[0] != "2026-10-06" {
		t.Fatalf("valid_until sessions=%v, want only 2026-10-06", got)
	}
	if got := sessionDates(deletedSchedule.ID, true); len(got) != 0 {
		t.Fatalf("deleted schedule got sessions %v", got)
	}
	if got := sessionDates(droppedSchedule.ID, true); len(got) != 0 {
		t.Fatalf("schedule of dropped enrollment got sessions %v", got)
	}
	var private []domain.ClassSession
	if err := db.Where("schedule_id = ?", liveSchedule.ID).Order("session_date").Find(&private).Error; err != nil || len(private) != 5 {
		t.Fatalf("private sessions=%d err=%v", len(private), err)
	}
	for _, s := range private {
		if s.EnrollmentID == nil || *s.EnrollmentID != live.ID {
			t.Fatalf("private session without its enrollment: %+v", s)
		}
	}

	var sessions []domain.ClassSession
	if err := db.Where("schedule_id = ? AND session_date IN ?", group.ID, []time.Time{day(10, 12), day(10, 19)}).Order("session_date").Find(&sessions).Error; err != nil || len(sessions) != 2 {
		t.Fatalf("october sessions=%v err=%v", sessions, err)
	}
	if err := u.DeleteSession(ctx, tenant, sessions[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := u.RescheduleSession(ctx, tenant, &domain.RescheduleSessionRequest{SessionID: sessions[1].ID, NewSessionDate: day(10, 20), NewStartTime: "09:00:00", NewEndTime: "10:00:00"}); err != nil {
		t.Fatal(err)
	}

	// Run again sequentially, then in parallel, and once more with the horizon moved
	// to the next month: nothing is duplicated and nothing removed comes back.
	countAll := func() int64 {
		var n int64
		if err := db.Unscoped().Model(&domain.ClassSession{}).Where("class_id IN ?", []uuid.UUID{groupClass, endsSoon, privateClass}).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := countAll()
	if second := newWorker().RunOnce(ctx); second.SessionsCreated != 0 || second.Schedules != 0 {
		t.Fatalf("second pass=%+v", second)
	}
	if err := db.Exec("UPDATE class_schedules SET sessions_generated_until = NULL WHERE id = ?", group.ID).Error; err != nil {
		t.Fatal(err)
	}
	// With the watermark lost, the rerun covers dates that already exist. The unique
	// index includes soft-deleted and rescheduled rows, so parallel workers still
	// create nothing and the deleted 12 October session stays deleted.
	var wg sync.WaitGroup
	results := make([]SessionGenerationRunResult, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = newWorker().RunOnce(ctx)
		}(i)
	}
	wg.Wait()
	for _, r := range results {
		if r.Failed != 0 || r.SessionsCreated != 0 {
			t.Fatalf("parallel pass created or failed: %+v", results)
		}
	}
	if after := countAll(); after != before {
		t.Fatalf("session rows %d -> %d", before, after)
	}
	var duplicates int64
	if err := db.Raw("SELECT COUNT(*) FROM (SELECT schedule_id, session_date FROM class_sessions WHERE schedule_id IS NOT NULL GROUP BY 1, 2 HAVING COUNT(*) > 1) d").Scan(&duplicates).Error; err != nil || duplicates != 0 {
		t.Fatalf("duplicates=%d err=%v", duplicates, err)
	}
	if got := sessionDates(group.ID, false); len(got) != 7 || contains(got, "2026-10-12") {
		t.Fatalf("deleted session reappeared: %v", got)
	}
	var rescheduled domain.ClassSession
	if err := db.First(&rescheduled, "id = ?", sessions[1].ID).Error; err != nil || rescheduled.Status != "rescheduled" {
		t.Fatalf("rescheduled session=%+v err=%v", rescheduled, err)
	}

	// Next month, parallel workers extend every live schedule exactly once.
	now = time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = newWorker().RunOnce(ctx)
		}(i)
	}
	wg.Wait()
	var createdNovember int64
	for _, r := range results {
		if r.Failed != 0 {
			t.Fatalf("november pass failed: %+v", results)
		}
		createdNovember += r.SessionsCreated
	}
	// Group Mondays in November 2026: 2, 9, 16, 23, 30. Private Thursdays: 5, 12, 19, 26.
	if createdNovember != 9 {
		t.Fatalf("november sessions created=%d, want 9 (%+v)", createdNovember, results)
	}
	if got := sessionDates(endingSchedule.ID, false); len(got) != 1 {
		t.Fatalf("ended schedule extended: %v", got)
	}
	var watermark time.Time
	if err := db.Raw("SELECT sessions_generated_until FROM class_schedules WHERE id = ?", group.ID).Scan(&watermark).Error; err != nil || !watermark.Equal(day(11, 30)) {
		t.Fatalf("watermark=%v err=%v", watermark, err)
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
