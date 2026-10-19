package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type ParentNotificationSender interface {
	Send(context.Context, *repository.ParentNotification) error
}

type ParentNotificationWorker struct {
	store    repository.ParentNotificationStore
	sender   ParentNotificationSender
	from     string
	interval time.Duration
	now      func() time.Time
}

func NewParentNotificationWorker(store repository.ParentNotificationStore, sender ParentNotificationSender, from string, interval time.Duration) *ParentNotificationWorker {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &ParentNotificationWorker{store: store, sender: sender, from: from, interval: interval, now: time.Now}
}

func (w *ParentNotificationWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Error("parent notification pass failed", "code", "outbox_unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *ParentNotificationWorker) RunOnce(ctx context.Context) error {
	// Bound each pass. Claim only when ready to send, avoiding expired queued leases.
	for range 100 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := w.store.Claim(ctx, w.from)
		if err != nil {
			return err
		}
		if n == nil {
			return nil
		}
		status, code, delay := "sent", "", time.Duration(0)
		// Resend remembers keys for 24h. Stop before that boundary after an uncertain
		// send; an operator must reconcile before re-driving. Chat keys persist.
		if n.Attempts > 12 || (n.Channel == "email" && w.now().Sub(n.FirstAttemptAt) >= 23*time.Hour) {
			status, code = "failed", "retry_exhausted"
		} else if err := w.sender.Send(ctx, n); err != nil {
			status, code = "pending", "delivery_unconfirmed"
			delay = 5 * time.Second * time.Duration(1<<min(n.Attempts-1, 10))
			if delay > time.Hour {
				delay = time.Hour
			}
			if n.Attempts >= 12 {
				status = "failed"
				code = "retry_exhausted"
			}
		}
		if err := w.store.Finish(ctx, n, status, code, delay); err != nil {
			return err
		}
		// No addresses, child names, body, credentials or provider error text in logs.
		slog.Info("parent notification delivery", "outbox_id", n.ID, "channel", n.Channel, "status", status, "attempt", n.Attempts, "code", code)
	}
	return nil
}
