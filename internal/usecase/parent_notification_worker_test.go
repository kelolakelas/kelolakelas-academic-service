package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type notificationStoreFake struct {
	item         *repository.ParentNotification
	claimed      bool
	status, code string
	delay        time.Duration
	finishErr    error
}

func (s *notificationStoreFake) Claim(context.Context, string) (*repository.ParentNotification, error) {
	if s.claimed {
		return nil, nil
	}
	s.claimed = true
	s.item.Attempts++
	return s.item, nil
}
func (s *notificationStoreFake) Finish(_ context.Context, _ *repository.ParentNotification, status, code string, delay time.Duration) error {
	s.status, s.code, s.delay = status, code, delay
	return s.finishErr
}

type notificationSenderFake struct {
	calls int
	ids   []uuid.UUID
	err   error
}

func (s *notificationSenderFake) Send(_ context.Context, n *repository.ParentNotification) error {
	s.calls++
	s.ids = append(s.ids, n.ID)
	return s.err
}

func TestParentNotificationWorkerRestartRetriesSameDelivery(t *testing.T) {
	store := &notificationStoreFake{item: &repository.ParentNotification{ID: uuid.New(), Channel: "email", FirstAttemptAt: time.Now()}}
	sender := &notificationSenderFake{err: errors.New("network failure")}
	w := NewParentNotificationWorker(store, sender, "sender@example.test", time.Second)
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.status != "pending" || store.delay != 5*time.Second || store.code != "delivery_unconfirmed" {
		t.Fatal("missing retry/backoff")
	}
	store.claimed = false
	sender.err = nil
	restarted := NewParentNotificationWorker(store, sender, "sender@example.test", time.Second)
	if err := restarted.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.status != "sent" || sender.calls != 2 || sender.ids[0] != sender.ids[1] {
		t.Fatal("restart lost delivery identity")
	}
}

func TestParentNotificationWorkerStopsUnsafeEmailRetry(t *testing.T) {
	for _, tc := range []struct {
		name     string
		attempts int
		age      time.Duration
	}{{"provider key window", 1, 23 * time.Hour}, {"attempt budget", 12, time.Minute}} {
		t.Run(tc.name, func(t *testing.T) {
			store := &notificationStoreFake{item: &repository.ParentNotification{ID: uuid.New(), Channel: "email", Attempts: tc.attempts, FirstAttemptAt: time.Now().Add(-tc.age)}}
			sender := &notificationSenderFake{}
			if err := NewParentNotificationWorker(store, sender, "", time.Second).RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if store.status != "failed" || sender.calls != 0 {
				t.Fatal("unsafe retry sent")
			}
		})
	}
	store := &notificationStoreFake{item: &repository.ParentNotification{ID: uuid.New(), Channel: "chat"}, finishErr: errors.New("db write failed")}
	if NewParentNotificationWorker(store, &notificationSenderFake{}, "", time.Second).RunOnce(context.Background()) == nil {
		t.Fatal("lost completion failure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if NewParentNotificationWorker(store, &notificationSenderFake{}, "", time.Second).RunOnce(ctx) == nil {
		t.Fatal("cancel ignored")
	}
}
