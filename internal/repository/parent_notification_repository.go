package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ParentNotification is an immutable delivery snapshot. API handlers never expose it.
type ParentNotification struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	ParentID       uuid.UUID
	Channel        string
	RecipientEmail string
	SenderEmail    string
	Body           string
	Attempts       int
	LeaseToken     uuid.UUID
	FirstAttemptAt time.Time
}

type ParentNotificationStore interface {
	Claim(context.Context, string) (*ParentNotification, error)
	Finish(context.Context, *ParentNotification, string, string, time.Duration) error
}

type parentNotificationRepository struct{ db *gorm.DB }

func NewParentNotificationRepository(db *gorm.DB) ParentNotificationStore {
	return &parentNotificationRepository{db: db}
}

// One atomic statement claims one row, so slow HTTP sends never hold DB locks.
// A 60s lease exceeds the 10s transport timeout; token fencing rejects stale completions.
func (r *parentNotificationRepository) Claim(ctx context.Context, from string) (*ParentNotification, error) {
	var n ParentNotification
	token := uuid.New()
	result := r.db.WithContext(ctx).Raw(`WITH stale_reminders AS (
 UPDATE parent_notification_outbox o SET status='failed',last_error='reminder_no_longer_eligible',lease_token=NULL
 WHERE o.status IN ('pending','sending') AND o.available_at<=now()
 AND EXISTS (SELECT 1 FROM session_reminder_enrollments m WHERE m.event_id=o.event_id)
 AND NOT EXISTS (
  SELECT 1 FROM session_reminder_enrollments m
  JOIN class_sessions cs ON cs.id=m.session_id AND cs.status='scheduled' AND cs.deleted_at IS NULL
  JOIN classes c ON c.id=cs.class_id AND c.tenant_id=o.tenant_id AND c.deleted_at IS NULL
  JOIN enrollments e ON e.id=m.enrollment_id AND e.class_id=c.id AND e.tenant_id=c.tenant_id AND e.status='active' AND e.deleted_at IS NULL
  JOIN students st ON st.id=e.student_id AND st.parent_id=o.parent_id AND st.deleted_at IS NULL
  WHERE m.event_id=o.event_id
   AND (o.channel='chat' OR nullif(btrim(e.parent_email),'') IS NOT NULL)
   AND (CASE WHEN cs.enrollment_id IS NOT NULL THEN e.id=cs.enrollment_id ELSE e.schedule_id=notification_cohort_schedule(cs.id) END)
 ) RETURNING o.id
 ), candidate AS (
 SELECT id FROM parent_notification_outbox
 WHERE status IN ('pending','sending') AND available_at<=now()
 AND id NOT IN (SELECT id FROM stale_reminders)
 ORDER BY available_at,created_at,id FOR UPDATE SKIP LOCKED LIMIT 1
 ) UPDATE parent_notification_outbox o SET status='sending',attempts=attempts+1,
 lease_token=?,available_at=now()+interval '60 seconds',
 first_attempt_at=coalesce(first_attempt_at,now()),
 sender_email=CASE WHEN sender_email='' THEN ? ELSE sender_email END
 FROM candidate WHERE o.id=candidate.id RETURNING o.*`, token, from).Scan(&n)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &n, nil
}

func (r *parentNotificationRepository) Finish(ctx context.Context, n *ParentNotification, status, code string, delay time.Duration) error {
	return r.db.WithContext(ctx).Exec(`UPDATE parent_notification_outbox SET status=?,last_error=?,
 available_at=now()+(? * interval '1 second'),lease_token=NULL,
 sent_at=CASE WHEN ?='sent' THEN now() ELSE sent_at END
 WHERE id=? AND status='sending' AND lease_token=?`, status, code, int(delay.Seconds()), status, n.ID, n.LeaseToken).Error
}
