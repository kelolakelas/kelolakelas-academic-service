package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type reminderRepoFake struct {
	calls       int
	from, until time.Time
	err         error
	batches     []repository.SessionReminderBatch
}

func (r *reminderRepoFake) EnqueueBatch(_ context.Context, from, until time.Time, limit int) (repository.SessionReminderBatch, error) {
	r.calls++
	r.from, r.until = from, until
	if limit != 100 {
		panic("unbounded batch")
	}
	if r.err != nil {
		return repository.SessionReminderBatch{}, r.err
	}
	if len(r.batches) == 0 {
		return repository.SessionReminderBatch{}, nil
	}
	batch := r.batches[0]
	r.batches = r.batches[1:]
	return batch, nil
}

func TestSessionReminderWIBWindow(t *testing.T) {
	for _, tc := range []struct {
		name, now, from, until string
		ahead, width           int
	}{
		{"before WIB midnight", "2026-10-19T16:59:59Z", "2026-10-20", "2026-10-21", 1, 1},
		{"at WIB midnight", "2026-10-19T17:00:00Z", "2026-10-21", "2026-10-22", 1, 1},
		{"month boundary", "2026-10-31T16:59:59Z", "2026-11-01", "2026-11-02", 1, 1},
		{"year boundary", "2026-12-31T16:59:59Z", "2027-01-01", "2027-01-02", 1, 1},
		{"leap day", "2028-02-28T23:00:00+07:00", "2028-02-29", "2028-03-01", 1, 1},
		{"process-zone independence", "2026-10-20T02:00:00-07:00", "2026-10-21", "2026-10-22", 1, 1},
		{"custom window", "2026-10-19T16:00:00Z", "2026-10-21", "2026-10-24", 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			repo := &reminderRepoFake{}
			w := NewSessionReminderWorker(repo, tc.ahead, tc.width, time.Second)
			w.now = func() time.Time { return now }
			if _, err = w.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if repo.from.Format("2006-01-02") != tc.from || repo.until.Format("2006-01-02") != tc.until {
				t.Fatalf("window %s to %s", repo.from, repo.until)
			}
		})
	}
}

func TestSessionReminderBatchingErrorsAndCancellation(t *testing.T) {
	repo := &reminderRepoFake{batches: []repository.SessionReminderBatch{{Sessions: 100, EnrollmentsMarked: 120, NotificationsQueued: 200}, {Sessions: 1, EnrollmentsMarked: 2, NotificationsQueued: 2}}}
	w := NewSessionReminderWorker(repo, 1, 1, time.Second)
	result, err := w.RunOnce(context.Background())
	if err != nil || repo.calls != 2 || result.Sessions != 101 || result.EnrollmentsMarked != 122 || result.NotificationsQueued != 202 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	repo.err = errors.New("db unavailable")
	if _, err = w.RunOnce(context.Background()); err == nil {
		t.Fatal("database error hidden")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := repo.calls
	if _, err = w.RunOnce(ctx); err != context.Canceled || repo.calls != before {
		t.Fatal("cancel ignored")
	}
	repo = &reminderRepoFake{}
	w = NewSessionReminderWorker(repo, 0, 0, 0)
	if w.daysAhead != 1 || w.windowDays != 1 || w.interval != 15*time.Minute {
		t.Fatal("defaults changed")
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
}
