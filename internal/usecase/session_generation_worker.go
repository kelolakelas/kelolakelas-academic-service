package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

const (
	// DefaultSessionGenerationHorizonMonths keeps sessions generated through the end
	// of the month after the current one.
	DefaultSessionGenerationHorizonMonths = 1
	// DefaultSessionGenerationInterval is how often a replica tops the horizon up.
	DefaultSessionGenerationInterval = time.Hour
)

// SessionGenerationWorker keeps every live schedule's sessions generated through a
// rolling horizon: the end of the month HorizonMonths after the current one (KEL-90).
//
// Each schedule remembers the last date already generated (sessions_generated_until)
// and the worker only creates sessions after it. A session the tenant deleted,
// cancelled or rescheduled is therefore never recreated, and a schedule that has
// ended, was deleted, or belongs to a deleted class or to an enrollment that is no
// longer live gets no new sessions. Every schedule is handled in its own transaction
// that locks the schedule row with FOR UPDATE SKIP LOCKED and inserts with ON CONFLICT
// DO NOTHING against the (schedule_id, session_date) unique index, so running the
// worker twice, or on several replicas at once, never duplicates a session.
type SessionGenerationWorker struct {
	txManager     repository.TransactionManager
	repo          repository.SessionGenerationRepository
	horizonMonths int
	interval      time.Duration
	now           func() time.Time
}

func NewSessionGenerationWorker(txManager repository.TransactionManager, repo repository.SessionGenerationRepository, horizonMonths int, interval time.Duration) *SessionGenerationWorker {
	if horizonMonths < 1 {
		horizonMonths = DefaultSessionGenerationHorizonMonths
	}
	if interval <= 0 {
		interval = DefaultSessionGenerationInterval
	}
	return &SessionGenerationWorker{txManager: txManager, repo: repo, horizonMonths: horizonMonths, interval: interval, now: time.Now}
}

// SessionGenerationRunResult reports one pass.
type SessionGenerationRunResult struct {
	Schedules       int
	SessionsCreated int64
	Failed          int
}

// window returns the first day of the current month, below which the worker never
// generates, and the last date it generates sessions for. "Today" is the calendar
// date of now in the process zone, the calendar the request paths use; both dates are
// placed at UTC midnight, the form the driver uses for DATE columns.
func (w *SessionGenerationWorker) window() (monthStart, horizonEnd time.Time) {
	today := calendarDateIn(w.now(), time.UTC)
	monthStart = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	horizonEnd = endOfMonth(monthStart.AddDate(0, w.horizonMonths, 0))
	return monthStart, horizonEnd
}

// RunOnce generates the missing sessions of every schedule that is due. A schedule
// whose generation fails is logged and skipped for this pass; it stays due and is
// retried on the next one.
func (w *SessionGenerationWorker) RunOnce(ctx context.Context) SessionGenerationRunResult {
	var result SessionGenerationRunResult
	if w == nil || w.repo == nil || w.txManager == nil {
		return result
	}
	monthStart, horizonEnd := w.window()
	cursor := uuid.Nil
	for ctx.Err() == nil {
		var scheduleID uuid.UUID
		var created int64
		err := w.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
			schedule, err := w.repo.LockNextScheduleDueForGeneration(txCtx, horizonEnd, cursor)
			if err != nil || schedule == nil {
				return err
			}
			scheduleID = schedule.ID

			// Sessions before the current month are history and are never backfilled.
			from := monthStart
			if schedule.SessionsGeneratedUntil != nil {
				if next := calendarDateIn(*schedule.SessionsGeneratedUntil, time.UTC).AddDate(0, 0, 1); next.After(from) {
					from = next
				}
			}
			sessions := sessionsForScheduleBetween(schedule, from, horizonEnd)
			if created, err = w.repo.InsertMissingSessions(txCtx, sessions); err != nil {
				return err
			}
			return w.repo.MarkSessionsGeneratedUntil(txCtx, schedule.ID, horizonEnd)
		})
		if scheduleID == uuid.Nil {
			// Nothing left to lock, or the lookup itself failed.
			if err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "session generation: failed to find due schedules", "error", err)
			}
			break
		}
		cursor = scheduleID
		result.Schedules++
		if err != nil {
			result.Failed++
			slog.ErrorContext(ctx, "session generation: failed to generate sessions", "schedule_id", scheduleID, "error", err)
			continue
		}
		result.SessionsCreated += created
	}
	if result.Schedules > 0 {
		slog.InfoContext(ctx, "session generation pass finished",
			"horizon_end", horizonEnd.Format("2006-01-02"),
			"schedules", result.Schedules,
			"sessions_created", result.SessionsCreated,
			"failed", result.Failed)
	}
	return result
}

// Run generates immediately and then on every interval until ctx is cancelled.
func (w *SessionGenerationWorker) Run(ctx context.Context) {
	if w == nil || ctx.Err() != nil {
		return
	}
	w.RunOnce(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.RunOnce(ctx)
		}
	}
}
