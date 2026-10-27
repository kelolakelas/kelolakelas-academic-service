package domain

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
)

var ErrPrivateChatActor = errors.New("verified private request actor required")
var ErrPrivateChatNotReady = errors.New("private chat event not ready")

type privateChatActorKey struct{}
type PrivateChatActor struct {
	UserID, TenantID, RoleID, MemberID uuid.UUID
	IsParent                           bool
}

func WithPrivateChatActor(ctx context.Context, a PrivateChatActor) context.Context {
	return context.WithValue(ctx, privateChatActorKey{}, a)
}
func PrivateChatActorFrom(ctx context.Context) (PrivateChatActor, bool) {
	a, ok := ctx.Value(privateChatActorKey{}).(PrivateChatActor)
	return a, ok && a.UserID != uuid.Nil
}

// Immutable facts captured with the transition; delivery bookkeeping is separate.
type PrivateChatEvent struct {
	ID               uuid.UUID             `gorm:"type:uuid;primaryKey" json:"id"`
	RequestID        uuid.UUID             `gorm:"type:uuid" json:"request_id"`
	Version          int64                 `json:"version"`
	Kind             string                `json:"kind"`
	TenantID         uuid.UUID             `gorm:"type:uuid" json:"tenant_id"`
	ParentID         uuid.UUID             `gorm:"type:uuid" json:"parent_id"`
	StudentID        uuid.UUID             `gorm:"type:uuid" json:"student_id"`
	ClassID          uuid.UUID             `gorm:"type:uuid" json:"class_id"`
	ActorUserID      uuid.UUID             `gorm:"type:uuid" json:"actor_user_id"`
	ActorKind        string                `json:"actor_kind"`
	TransitionUserID uuid.UUID             `gorm:"type:uuid" json:"-"`
	ActorRoleID      *uuid.UUID            `gorm:"type:uuid" json:"-"`
	ActorMemberID    *uuid.UUID            `gorm:"type:uuid" json:"-"`
	ClassName        string                `json:"class_name"`
	StudentFirstName string                `json:"student_first_name"`
	Slots            []PrivateScheduleSlot `gorm:"serializer:json;type:jsonb" json:"slots"`
	RecommendedSlots []PrivateScheduleSlot `gorm:"serializer:json;type:jsonb" json:"recommended_slots,omitempty"`
	Reason           *string               `json:"reason,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
}

func (PrivateChatEvent) TableName() string { return "private_request_chat_events" }

type PrivateChatProjection struct {
	PrivateChatEvent
	Body             string     `json:"body"`
	TransactionID    *uuid.UUID `json:"transaction_id,omitempty"`
	InvoiceExpiresAt *time.Time `json:"invoice_expires_at,omitempty"`
}
