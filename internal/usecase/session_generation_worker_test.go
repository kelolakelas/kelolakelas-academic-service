package usecase

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

func utcDay(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func datePtr(t time.Time) *time.Time { return &t }

type passthroughTx struct{ calls int }

func (m *passthroughTx) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.calls++
	return fn(ctx)
}

// fakeGenerationRepo keeps schedules and the (schedule, date) keys already stored,
// mirroring the unique index, so tests observe what the worker would persist.
type fakeGenerationRepo struct {
	schedules  []*domain.ClassSchedule
	stored     map[uuid.UUID]map[string]bool
	failInsert map[uuid.UUID]bool
	lockErr    error
	horizons   []time.Time
}

func newFakeGenerationRepo(schedules ...*domain.ClassSchedule) *fakeGenerationRepo {
	sort.Slice(schedules, func(i, j int) bool { return schedules[i].ID.String() < schedules[j].ID.String() })
	return &fakeGenerationRepo{schedules: schedules, stored: map[uuid.UUID]map[string]bool{}, failInsert: map[uuid.UUID]bool{}}
}

func (r *fakeGenerationRepo) LockNextScheduleDueForGeneration(_ context.Context, horizonEnd time.Time, afterID uuid.UUID) (*domain.ClassSchedule, error) {
	r.horizons = append(r.horizons, horizonEnd)
	if r.lockErr != nil {
		return nil, r.lockErr
	}
	for _, s := range r.schedules {
		if s.ID.String() <= afterID.String() {
			continue
		}
		if s.SessionsGeneratedUntil != nil && !s.SessionsGeneratedUntil.Before(horizonEnd) {
			continue
		}
		if s.ValidUntil != nil && s.SessionsGeneratedUntil != nil && !s.ValidUntil.After(*s.SessionsGeneratedUntil) {
			continue
		}
		copy := *s
		return &copy, nil
	}
	return nil, nil
}

func (r *fakeGenerationRepo) InsertMissingSessions(_ context.Context, sessions []*domain.ClassSession) (int64, error) {
	var inserted int64
	for _, session := range sessions {
		if r.failInsert[*session.ScheduleID] {
			return 0, errors.New("insert failed")
		}
	}
	for _, session := range sessions {
		dates := r.stored[*session.ScheduleID]
		if dates == nil {
			dates = map[string]bool{}
			r.stored[*session.ScheduleID] = dates
		}
		key := session.SessionDate.Format("2006-01-02")
		if !dates[key] {
			dates[key] = true
			inserted++
		}
	}
	return inserted, nil
}

func (r *fakeGenerationRepo) MarkSessionsGeneratedUntil(_ context.Context, scheduleID uuid.UUID, until time.Time) error {
	for _, s := range r.schedules {
		if s.ID == scheduleID && (s.SessionsGeneratedUntil == nil || s.SessionsGeneratedUntil.Before(until)) {
			s.SessionsGeneratedUntil = datePtr(until)
		}
	}
	return nil
}

func (r *fakeGenerationRepo) dates(scheduleID uuid.UUID) []string {
	var out []string
	for date := range r.stored[scheduleID] {
		out = append(out, date)
	}
	sort.Strings(out)
	return out
}

func newTestWorker(repo *fakeGenerationRepo, now time.Time, horizonMonths int) *SessionGenerationWorker {
	w := NewSessionGenerationWorker(&passthroughTx{}, repo, horizonMonths, time.Hour)
	w.now = func() time.Time { return now }
	return w
}

func weeklySchedule(day int) *domain.ClassSchedule {
	tutor := uuid.New()
	return &domain.ClassSchedule{ID: uuid.New(), ClassID: uuid.New(), TutorID: &tutor, Capacity: 1, DayOfWeek: day, StartTime: "10:00:00", EndTime: "11:00:00"}
}

func TestSessionsForScheduleBetweenCrossesYearAndHonoursValidity(t *testing.T) {
	schedule := weeklySchedule(5) // Friday
	sessions := sessionsForScheduleBetween(schedule, utcDay(2026, 12, 20), utcDay(2027, 1, 31))
	var got []string
	for _, s := range sessions {
		got = append(got, s.SessionDate.Format("2006-01-02"))
	}
	want := []string{"2026-12-25", "2027-01-01", "2027-01-08", "2027-01-15", "2027-01-22", "2027-01-29"}
	if len(got) != len(want) {
		t.Fatalf("dates=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dates=%v want %v", got, want)
		}
	}

	// valid_from and valid_until clip the range, read as calendar dates even when
	// they carry another zone.
	far := time.FixedZone("far", 13*60*60)
	schedule.ValidFrom = datePtr(time.Date(2027, 1, 2, 0, 0, 0, 0, far))
	schedule.ValidUntil = datePtr(time.Date(2027, 1, 15, 0, 0, 0, 0, far))
	sessions = sessionsForScheduleBetween(schedule, utcDay(2026, 12, 20), utcDay(2027, 1, 31))
	if len(sessions) != 2 || !sessions[0].SessionDate.Equal(utcDay(2027, 1, 8)) || !sessions[1].SessionDate.Equal(utcDay(2027, 1, 15)) {
		t.Fatalf("clipped sessions=%v", sessions)
	}
}

func TestGenerateSessionsForScheduleStillEndsAtMonthEnd(t *testing.T) {
	schedule := weeklySchedule(1) // Monday
	sessions := generateSessionsForSchedule(schedule, time.Date(2026, 9, 14, 15, 30, 0, 0, time.UTC))
	if len(sessions) != 3 || !sessions[0].SessionDate.Equal(utcDay(2026, 9, 14)) || !sessions[2].SessionDate.Equal(utcDay(2026, 9, 28)) {
		t.Fatalf("sessions=%v", sessions)
	}
	for _, s := range sessions {
		if s.ScheduleID == nil || *s.ScheduleID != schedule.ID || s.Status != "scheduled" {
			t.Fatalf("session=%+v", s)
		}
	}
}

func TestWorkerFillsNextMonthOnlyAfterGeneratedRange(t *testing.T) {
	schedule := weeklySchedule(1)
	schedule.SessionsGeneratedUntil = datePtr(utcDay(2026, 9, 30)) // created this month
	repo := newFakeGenerationRepo(schedule)
	w := newTestWorker(repo, time.Date(2026, 9, 27, 23, 0, 0, 0, time.UTC), 1)

	res := w.RunOnce(context.Background())
	want := []string{"2026-10-05", "2026-10-12", "2026-10-19", "2026-10-26"}
	if got := repo.dates(schedule.ID); len(got) != len(want) || got[0] != want[0] || got[3] != want[3] {
		t.Fatalf("dates=%v want %v", got, want)
	}
	if res.Schedules != 1 || res.SessionsCreated != 4 || res.Failed != 0 {
		t.Fatalf("result=%+v", res)
	}
	if !schedule.SessionsGeneratedUntil.Equal(utcDay(2026, 10, 31)) {
		t.Fatalf("watermark=%v", schedule.SessionsGeneratedUntil)
	}

	// A second pass finds nothing due and creates nothing.
	if again := w.RunOnce(context.Background()); again.Schedules != 0 || again.SessionsCreated != 0 {
		t.Fatalf("second pass=%+v", again)
	}
}

func TestWorkerHorizonCrossesYear(t *testing.T) {
	schedule := weeklySchedule(5)
	schedule.SessionsGeneratedUntil = datePtr(utcDay(2026, 12, 31))
	repo := newFakeGenerationRepo(schedule)
	w := newTestWorker(repo, time.Date(2026, 12, 15, 8, 0, 0, 0, time.UTC), 1)
	w.RunOnce(context.Background())
	got := repo.dates(schedule.ID)
	if len(got) != 5 || got[0] != "2027-01-01" || got[4] != "2027-01-29" {
		t.Fatalf("dates=%v", got)
	}
	if !schedule.SessionsGeneratedUntil.Equal(utcDay(2027, 1, 31)) {
		t.Fatalf("watermark=%v", schedule.SessionsGeneratedUntil)
	}
}

func TestWorkerRespectsValidUntilAndStopsWhenEnded(t *testing.T) {
	schedule := weeklySchedule(1)
	schedule.SessionsGeneratedUntil = datePtr(utcDay(2026, 9, 13))
	schedule.ValidUntil = datePtr(utcDay(2026, 9, 21))
	repo := newFakeGenerationRepo(schedule)
	w := newTestWorker(repo, time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), 1)
	w.RunOnce(context.Background())
	if got := repo.dates(schedule.ID); len(got) != 2 || got[0] != "2026-09-14" || got[1] != "2026-09-21" {
		t.Fatalf("dates=%v", got)
	}
	// The watermark passes valid_until, so the schedule is never due again.
	if res := w.RunOnce(context.Background()); res.Schedules != 0 {
		t.Fatalf("ended schedule still due: %+v", res)
	}
}

func TestWorkerNeverBackfillsBeforeCurrentMonth(t *testing.T) {
	schedule := weeklySchedule(1) // no watermark: a schedule without any session yet
	repo := newFakeGenerationRepo(schedule)
	w := newTestWorker(repo, time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC), 1)
	w.RunOnce(context.Background())
	got := repo.dates(schedule.ID)
	if len(got) == 0 || got[0] != "2026-09-07" || got[len(got)-1] != "2026-10-26" {
		t.Fatalf("dates=%v", got)
	}
}

func TestWorkerSkipsFailingScheduleAndContinues(t *testing.T) {
	bad, good := weeklySchedule(1), weeklySchedule(2)
	for _, s := range []*domain.ClassSchedule{bad, good} {
		s.SessionsGeneratedUntil = datePtr(utcDay(2026, 9, 30))
	}
	repo := newFakeGenerationRepo(bad, good)
	repo.failInsert[bad.ID] = true
	w := newTestWorker(repo, time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC), 1)
	res := w.RunOnce(context.Background())
	if res.Schedules != 2 || res.Failed != 1 || len(repo.dates(good.ID)) != 4 || len(repo.dates(bad.ID)) != 0 {
		t.Fatalf("result=%+v good=%v bad=%v", res, repo.dates(good.ID), repo.dates(bad.ID))
	}
	// The failed schedule keeps its watermark and is retried on the next pass.
	if !bad.SessionsGeneratedUntil.Equal(utcDay(2026, 9, 30)) {
		t.Fatalf("failed watermark moved: %v", bad.SessionsGeneratedUntil)
	}
	repo.failInsert[bad.ID] = false
	if retry := w.RunOnce(context.Background()); retry.Schedules != 1 || retry.SessionsCreated != 4 {
		t.Fatalf("retry=%+v", retry)
	}
}

func TestWorkerStopsOnLookupErrorAndCancelledContext(t *testing.T) {
	repo := newFakeGenerationRepo(weeklySchedule(1))
	repo.lockErr = errors.New("db down")
	w := newTestWorker(repo, time.Now(), 1)
	if res := w.RunOnce(context.Background()); res.Schedules != 0 || len(repo.horizons) != 1 {
		t.Fatalf("result=%+v lookups=%d", res, len(repo.horizons))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo.lockErr = nil
	if res := w.RunOnce(ctx); res.Schedules != 0 {
		t.Fatalf("cancelled pass=%+v", res)
	}
	w.Run(ctx) // returns at once on a cancelled context
}

func TestWorkerHorizonAndDefaults(t *testing.T) {
	w := NewSessionGenerationWorker(&passthroughTx{}, newFakeGenerationRepo(), 0, 0)
	if w.horizonMonths != DefaultSessionGenerationHorizonMonths || w.interval != DefaultSessionGenerationInterval {
		t.Fatalf("defaults horizon=%d interval=%s", w.horizonMonths, w.interval)
	}
	w.horizonMonths = 3
	// The window uses the calendar date of now in its own zone (the application
	// calendar, as scheduleHasEnded does), placed at UTC midnight like DATE columns.
	ahead := time.FixedZone("ahead", 7*60*60)
	w.now = func() time.Time { return time.Date(2026, 12, 1, 3, 0, 0, 0, ahead) } // still Nov 30 in UTC
	start, end := w.window()
	if !start.Equal(utcDay(2026, 12, 1)) || !end.Equal(utcDay(2027, 3, 31)) {
		t.Fatalf("window=%v..%v", start, end)
	}
	var nilWorker *SessionGenerationWorker
	if res := nilWorker.RunOnce(context.Background()); res != (SessionGenerationRunResult{}) {
		t.Fatalf("nil worker=%+v", res)
	}
}
