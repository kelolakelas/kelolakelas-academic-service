package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SessionReminderBatch struct {
	Sessions            int
	EnrollmentsMarked   int64
	NotificationsQueued int64
}

type SessionReminderRepository interface {
	EnqueueBatch(context.Context, time.Time, time.Time, int) (SessionReminderBatch, error)
}

type sessionReminderRepository struct{ db *gorm.DB }

func NewSessionReminderRepository(db *gorm.DB) SessionReminderRepository {
	return &sessionReminderRepository{db: db}
}

// This scope is shared by candidate discovery and the atomic recipient snapshot.
// It matches KEL-155's active private/group/cohort and tenant ownership boundary.
const reminderEnrollmentScope = ` FROM enrollments e
 JOIN students st ON st.id=e.student_id AND st.deleted_at IS NULL
 WHERE e.class_id=cs.class_id AND e.tenant_id=c.tenant_id
 AND e.status='active' AND e.deleted_at IS NULL
 AND (CASE WHEN cs.enrollment_id IS NOT NULL THEN e.id=cs.enrollment_id
           ELSE e.schedule_id=notification_cohort_schedule(cs.id) END)
 AND NOT session_reminder_covered(cs.id,e.id)`

func (r *sessionReminderRepository) EnqueueBatch(ctx context.Context, from, until time.Time, limit int) (SessionReminderBatch, error) {
	var result SessionReminderBatch
	if limit < 1 || limit > 100 {
		limit = 100
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sessions []struct {
			ID   uuid.UUID
			Body string
		}
		// Session locks serialize capture against cancellation/reschedule and other
		// replicas. No HTTP delivery is performed while these locks are held.
		if err := tx.Raw(`SELECT cs.id,
   'Pengingat kelas ' || c.name || ', sesi ' || to_char(cs.session_date,'DD-MM-YYYY') ||
   ' ' || cs.start_time::text || '–' || cs.end_time::text || ' WIB.' AS body
   FROM class_sessions cs JOIN classes c ON c.id=cs.class_id AND c.deleted_at IS NULL
   WHERE cs.status='scheduled' AND cs.deleted_at IS NULL
   AND cs.session_date>=?::date AND cs.session_date<?::date
   AND EXISTS (SELECT 1 `+reminderEnrollmentScope+`)
   ORDER BY cs.session_date,cs.id LIMIT ? FOR UPDATE OF cs SKIP LOCKED`, from.Format("2006-01-02"), until.Format("2006-01-02"), limit).Scan(&sessions).Error; err != nil {
			return err
		}
		result.Sessions = len(sessions)
		for _, session := range sessions {
			// Stable session event identity gives one notification per parent/channel,
			// even if a new sibling enrollment appears after the first capture pass.
			eventID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("kelolakelas/session-reminder/"+session.ID.String()))
			var counts struct {
				Marked int64
				Queued int64
			}
			// One MATERIALIZED recipient set feeds BOTH writes: a new enrollment cannot
			// be marked covered without having participated in the outbox snapshot.
			if err := tx.Raw(`WITH eligible AS MATERIALIZED (
    SELECT recipient.*
    FROM class_sessions cs JOIN classes c ON c.id=cs.class_id AND c.deleted_at IS NULL
    JOIN LATERAL (SELECT e.id AS enrollment_id,c.tenant_id,st.parent_id,st.first_name,e.parent_email,e.joined_at `+reminderEnrollmentScope+`) recipient ON true
    WHERE cs.id=? AND cs.status='scheduled' AND cs.deleted_at IS NULL
   ), grouped AS (
    SELECT tenant_id,parent_id,channels.channel,
     string_agg(DISTINCT first_name,', ' ORDER BY first_name) AS names,
     coalesce((array_agg(parent_email ORDER BY joined_at DESC,enrollment_id)
      FILTER (WHERE nullif(btrim(parent_email),'') IS NOT NULL))[1],'') AS email
    FROM eligible CROSS JOIN (VALUES ('chat'),('email')) channels(channel)
    WHERE channels.channel='chat' OR nullif(btrim(parent_email),'') IS NOT NULL
    GROUP BY tenant_id,parent_id,channels.channel
   ), deliveries AS (
    INSERT INTO parent_notification_outbox(event_id,tenant_id,parent_id,channel,recipient_email,body)
    SELECT ?::uuid,tenant_id,parent_id,channel,CASE WHEN channel='email' THEN email ELSE '' END,
     left(? || E'\nSiswa: ' || left(names,1000),2000) FROM grouped
    ON CONFLICT (event_id,parent_id,channel) DO NOTHING RETURNING id
   ), covered AS (
    INSERT INTO session_reminder_enrollments(session_id,enrollment_id,event_id)
    SELECT ?::uuid,enrollment_id,?::uuid FROM eligible
    ON CONFLICT (session_id,enrollment_id) DO NOTHING RETURNING enrollment_id
   ) SELECT (SELECT count(*) FROM deliveries) AS queued,(SELECT count(*) FROM covered) AS marked`, session.ID, eventID, session.Body, session.ID, eventID).Scan(&counts).Error; err != nil {
				return err
			}
			result.EnrollmentsMarked += counts.Marked
			result.NotificationsQueued += counts.Queued
		}
		return nil
	})
	if err != nil {
		return SessionReminderBatch{}, err
	}
	return result, nil
}
