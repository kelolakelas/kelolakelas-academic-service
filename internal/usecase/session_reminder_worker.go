package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

// Reminder windows are calendar dates in Asia/Jakarta (WIB, UTC+7), never the
// process timezone or a rolling 24-hour window. DATE query bounds use UTC midnight
// only as a carrier for those date components, not as actual session instants.
var reminderWIB = time.FixedZone("Asia/Jakarta", 7*60*60)

type SessionReminderWorker struct {
	repo                  repository.SessionReminderRepository
	daysAhead, windowDays int
	interval              time.Duration
	now                   func() time.Time
}

func NewSessionReminderWorker(repo repository.SessionReminderRepository, daysAhead, windowDays int, interval time.Duration) *SessionReminderWorker {
	if daysAhead < 1 || daysAhead > 30 {
		daysAhead = 1
	}
	if windowDays < 1 || windowDays > 7 {
		windowDays = 1
	}
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	return &SessionReminderWorker{repo: repo, daysAhead: daysAhead, windowDays: windowDays, interval: interval, now: time.Now}
}

func (w *SessionReminderWorker) window() (time.Time, time.Time) {
	local := w.now().In(reminderWIB)
	date := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	from := date.AddDate(0, 0, w.daysAhead)
	return from, from.AddDate(0, 0, w.windowDays)
}

func (w *SessionReminderWorker) RunOnce(ctx context.Context) (repository.SessionReminderBatch, error) {
	var total repository.SessionReminderBatch
	from, until := w.window()
	for ctx.Err() == nil {
		batch, err := w.repo.EnqueueBatch(ctx, from, until, 100)
		if err != nil {
			return total, err
		}
		total.Sessions += batch.Sessions
		total.EnrollmentsMarked += batch.EnrollmentsMarked
		total.NotificationsQueued += batch.NotificationsQueued
		if batch.Sessions < 100 || batch.EnrollmentsMarked == 0 {
			return total, nil
		}
	}
	return total, ctx.Err()
}

func (w *SessionReminderWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		result, err := w.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("session reminder pass failed", "code", "reminder_outbox_unavailable")
		} else if result.EnrollmentsMarked > 0 {
			slog.Info("session reminder pass finished", "sessions", result.Sessions, "enrollments_marked", result.EnrollmentsMarked, "notifications_queued", result.NotificationsQueued)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
