package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// Production uses this repository to make event capture part of every mutation.
// The original constructor remains available for legacy in-process consumers.
type privateChatRequests struct {
	*privateScheduleRequestRepository
}

func NewPrivateScheduleRequestRepositoryWithChat(db *gorm.DB) PrivateScheduleRequestRepository {
	return &privateChatRequests{&privateScheduleRequestRepository{db: db}}
}
func (r *privateChatRequests) atomic(ctx context.Context, fn func(context.Context) error) error {
	if ctx.Value(txKey{}) != nil {
		return fn(ctx)
	}
	return NewTransactionManager(r.db).WithTransaction(ctx, fn)
}
func privateEventActor(ctx context.Context, req *domain.PrivateScheduleRequest, kind string) (domain.PrivateChatActor, error) {
	a, ok := domain.PrivateChatActorFrom(ctx)
	if !ok {
		return a, domain.ErrPrivateChatActor
	}
	if kind == "created" {
		if !a.IsParent || a.UserID != req.ParentID {
			return a, domain.ErrPrivateChatActor
		}
		return a, nil
	}
	if a.IsParent || a.TenantID != req.TenantID || a.MemberID == uuid.Nil || a.RoleID == uuid.Nil {
		return a, domain.ErrPrivateChatActor
	}
	return a, nil
}
func (r *privateChatRequests) capture(ctx context.Context, req *domain.PrivateScheduleRequest, kind string, bump bool) error {
	db := GetDB(ctx, r.db)
	a, ok := domain.PrivateChatActorFrom(ctx)
	if !ok {
		return domain.ErrPrivateChatActor
	}
	transition := a.UserID
	// Recommendation acceptance is the parent's own action, including legacy
	// recommendations without a persisted admin actor. Never invent an admin.
	if kind == "approved" && a.IsParent {
		if a.UserID != req.ParentID || len(req.RecommendedSlots) == 0 || req.Status != "rejected" {
			return domain.ErrPrivateChatActor
		}
		kind = "recommendation_accepted"
	} else {
		var err error
		a, err = privateEventActor(ctx, req, kind)
		if err != nil {
			return err
		}
	}

	if bump {
		var version int64
		if err := db.Raw("UPDATE private_schedule_requests SET chat_version=chat_version+1 WHERE id=? RETURNING chat_version", req.ID).Scan(&version).Error; err != nil {
			return err
		}
		req.ChatVersion = version
	}
	var class domain.Class
	var student domain.Student
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=?", req.ClassID, req.TenantID).First(&class).Error; err != nil {
		return err
	}
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND parent_id=?", req.StudentID, req.ParentID).First(&student).Error; err != nil {
		return err
	}
	slots := req.Slots
	if (kind == "approved" || kind == "recommendation_accepted") && len(req.RecommendedSlots) > 0 {
		slots = req.RecommendedSlots
	}
	event := domain.PrivateChatEvent{ID: uuid.New(), RequestID: req.ID, Version: req.ChatVersion, Kind: kind, TenantID: req.TenantID, ParentID: req.ParentID, StudentID: req.StudentID, ClassID: req.ClassID, ActorUserID: a.UserID, ActorKind: "member", TransitionUserID: transition, ClassName: class.Name, StudentFirstName: student.FirstName, Slots: slots, RecommendedSlots: req.RecommendedSlots, Reason: req.RejectionReason, CreatedAt: time.Now().UTC()}
	if kind == "created" || kind == "recommendation_accepted" {
		event.ActorKind = "parent"
	} else {
		event.ActorRoleID = &a.RoleID
		event.ActorMemberID = &a.MemberID
	}
	if err := db.Create(&event).Error; err != nil {
		return err
	}
	return db.Exec("INSERT INTO private_request_chat_outbox(event_id) VALUES(?)", event.ID).Error
}
func (r *privateChatRequests) Create(ctx context.Context, req *domain.PrivateScheduleRequest) error {
	return r.atomic(ctx, func(ctx context.Context) error {
		if _, err := privateEventActor(ctx, req, "created"); err != nil {
			return err
		}
		if err := r.privateScheduleRequestRepository.Create(ctx, req); err != nil {
			return err
		}
		return r.capture(ctx, req, "created", false)
	})
}
func (r *privateChatRequests) SetStatus(ctx context.Context, id, tenant uuid.UUID, status string) error {
	return r.atomic(ctx, func(ctx context.Context) error {
		req, err := r.LockForTenant(ctx, id, tenant)
		if err != nil {
			return err
		}
		if req.Status == status {
			return nil
		}
		if err = r.privateScheduleRequestRepository.SetStatus(ctx, id, tenant, status); err != nil {
			return err
		}
		if status == "approved" {
			return r.capture(ctx, req, "approved", true)
		}
		// A platform-fee refusal reopens the request. Its temporary approval must
		// never later announce a payable invoice or inherit another admin's actor.
		if req.Status == "approved" {
			if err = GetDB(ctx, r.db).Exec(`UPDATE private_request_chat_outbox o SET status='superseded',lease_token=NULL,last_error='approval_reopened' FROM private_request_chat_events e WHERE o.event_id=e.id AND e.request_id=? AND e.version=? AND o.status<>'sent'`, id, req.ChatVersion).Error; err != nil {
				return err
			}
			return GetDB(ctx, r.db).Exec("UPDATE private_schedule_requests SET chat_version=chat_version+1 WHERE id=?", id).Error
		}
		return nil
	})
}
func (r *privateChatRequests) RejectWithRecommendation(ctx context.Context, id, tenant uuid.UUID, reason *string, slots []domain.PrivateScheduleSlot) (result *domain.PrivateScheduleRequest, err error) {
	err = r.atomic(ctx, func(ctx context.Context) error {
		if _, err := r.LockForTenant(ctx, id, tenant); err != nil {
			return err
		}
		var err error
		result, err = r.privateScheduleRequestRepository.RejectWithRecommendation(ctx, id, tenant, reason, slots)
		if err != nil {
			return err
		}
		return r.capture(ctx, result, "rejected", true)
	})
	return
}
func (r *privateChatRequests) Transition(ctx context.Context, id uuid.UUID, tenant, parent *uuid.UUID, status string, reason *string) (result *domain.PrivateScheduleRequest, err error) {
	err = r.atomic(ctx, func(ctx context.Context) error {
		if tenant != nil {
			if _, err := r.LockForTenant(ctx, id, *tenant); err != nil {
				return err
			}
		} else if parent != nil {
			if _, err := r.LockForParent(ctx, id, *parent); err != nil {
				return err
			}
		}
		var err error
		result, err = r.privateScheduleRequestRepository.Transition(ctx, id, tenant, parent, status, reason)
		if err != nil {
			return err
		}
		if status == "rejected" {
			return r.capture(ctx, result, "rejected", true)
		}
		return nil
	})
	return
}
