package repository

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/gorm"
	"time"
)

type PrivateChatDelivery struct {
	EventID    uuid.UUID
	LeaseToken uuid.UUID
	Attempts   int
}
type PrivateChatStore struct{ db *gorm.DB }

func NewPrivateChatStore(db *gorm.DB) *PrivateChatStore { return &PrivateChatStore{db} }
func (s *PrivateChatStore) Event(ctx context.Context, id uuid.UUID) (*domain.PrivateChatEvent, error) {
	var e domain.PrivateChatEvent
	if err := s.db.WithContext(ctx).Where("id=?", id).First(&e).Error; err != nil {
		return nil, err
	}
	// Revalidate current subject ownership without rewriting the immutable actor
	// and content snapshot of the already committed business transition.
	var count int64
	err := s.db.WithContext(ctx).Table("private_schedule_requests r").Joins("JOIN classes c ON c.id=r.class_id AND c.tenant_id=r.tenant_id AND c.deleted_at IS NULL").Joins("JOIN students st ON st.id=r.student_id AND st.parent_id=r.parent_id AND st.deleted_at IS NULL").Where("r.id=? AND r.tenant_id=? AND r.parent_id=? AND r.class_id=? AND r.student_id=?", e.RequestID, e.TenantID, e.ParentID, e.ClassID, e.StudentID).Count(&count).Error
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, domain.ErrChatContextNotFound
	}
	if e.Kind == "approved" || e.Kind == "recommendation_accepted" {
		var req domain.PrivateScheduleRequest
		if err = s.db.WithContext(ctx).First(&req, "id=?", e.RequestID).Error; err != nil {
			return nil, err
		}
		if req.Status != "approved" || req.ChatVersion != e.Version {
			return nil, domain.ErrPrivateChatNotReady
		}
	}
	return &e, nil
}
func (s *PrivateChatStore) Enrollment(ctx context.Context, e *domain.PrivateChatEvent) (*domain.Enrollment, error) {
	var enrollment domain.Enrollment
	err := s.db.WithContext(ctx).Where("idempotency_key=? AND tenant_id=? AND student_id=? AND class_id=?", "private-request:"+e.RequestID.String(), e.TenantID, e.StudentID, e.ClassID).First(&enrollment).Error
	if err != nil {
		return nil, err
	}
	return &enrollment, nil
}
func (s *PrivateChatStore) Claim(ctx context.Context) (*PrivateChatDelivery, error) {
	var d PrivateChatDelivery
	lease := uuid.New()
	result := s.db.WithContext(ctx).Raw(`WITH next AS (SELECT o.event_id FROM private_request_chat_outbox o JOIN private_request_chat_events e ON e.id=o.event_id WHERE o.status IN ('pending','sending') AND o.available_at<=now() AND NOT EXISTS (SELECT 1 FROM private_request_chat_events older JOIN private_request_chat_outbox prior ON prior.event_id=older.id WHERE older.request_id=e.request_id AND older.version<e.version AND prior.status IN ('pending','sending','failed')) ORDER BY o.created_at,o.event_id FOR UPDATE OF o SKIP LOCKED LIMIT 1) UPDATE private_request_chat_outbox o SET status='sending',attempts=attempts+1,lease_token=?,available_at=now()+interval '90 seconds' FROM next WHERE o.event_id=next.event_id RETURNING o.event_id,o.lease_token,o.attempts`, lease).Scan(&d)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &d, nil
}
func (s *PrivateChatStore) Finish(ctx context.Context, d *PrivateChatDelivery, status, code string, delay time.Duration) error {
	return s.db.WithContext(ctx).Exec(`UPDATE private_request_chat_outbox SET status=?,last_error=?,available_at=now()+(? * interval '1 second'),lease_token=NULL,sent_at=CASE WHEN ?='sent' THEN now() ELSE sent_at END WHERE event_id=? AND status='sending' AND lease_token=?`, status, code, int(delay.Seconds()), status, d.EventID, d.LeaseToken).Error
}

type PrivateChatDiagnostic struct {
	EventID     uuid.UUID `json:"event_id"`
	RequestID   uuid.UUID `json:"request_id"`
	Kind        string    `json:"kind"`
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	LastError   string    `json:"last_error"`
	AvailableAt time.Time `json:"available_at"`
}

func (s *PrivateChatStore) Diagnostics(ctx context.Context) ([]PrivateChatDiagnostic, error) {
	return s.DiagnosticsPage(ctx, "", 1)
}
func (s *PrivateChatStore) DiagnosticsPage(ctx context.Context, status string, page int) ([]PrivateChatDiagnostic, error) {
	rows := []PrivateChatDiagnostic{}
	q := s.db.WithContext(ctx).Table("private_request_chat_outbox o").Select("o.event_id,e.request_id,e.kind,o.status,o.attempts,o.last_error,o.available_at").Joins("JOIN private_request_chat_events e ON e.id=o.event_id")
	if status == "" {
		q = q.Where("o.status IN ('pending','sending','failed')")
	} else {
		q = q.Where("o.status=?", status)
	}
	err := q.Order("o.created_at,o.event_id").Offset((page - 1) * 100).Limit(100).Scan(&rows).Error
	return rows, err
}

func (s *PrivateChatStore) Requeue(ctx context.Context, id uuid.UUID) error {
	result := s.db.WithContext(ctx).Exec("UPDATE private_request_chat_outbox SET status='pending',attempts=0,last_error='',available_at=now(),lease_token=NULL WHERE event_id=? AND status='failed'", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("failed event not found")
	}
	return nil
}
