package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type PrivateScheduleRequestRepository interface {
	Create(ctx context.Context, request *domain.PrivateScheduleRequest) error
	Get(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID) (*domain.PrivateScheduleRequest, error)
	List(ctx context.Context, tenantID, parentID *uuid.UUID, status string) ([]domain.PrivateScheduleRequest, error)
	Transition(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID, status string, reason *string) (*domain.PrivateScheduleRequest, error)
	RejectWithRecommendation(ctx context.Context, id, tenantID uuid.UUID, reason *string, slots []domain.PrivateScheduleSlot) (*domain.PrivateScheduleRequest, error)
	DeclineRecommendation(ctx context.Context, id, parentID uuid.UUID) (*domain.PrivateScheduleRequest, error)
	LockForParent(ctx context.Context, id, parentID uuid.UUID) (*domain.PrivateScheduleRequest, error)
	LockForTenant(ctx context.Context, id, tenantID uuid.UUID) (*domain.PrivateScheduleRequest, error)
	SetStatus(ctx context.Context, id, tenantID uuid.UUID, status string) error
}

type privateScheduleRequestRepository struct{ db *gorm.DB }

func NewPrivateScheduleRequestRepository(db *gorm.DB) PrivateScheduleRequestRepository {
	return &privateScheduleRequestRepository{db: db}
}

func privateRequestScope(db *gorm.DB, tenantID, parentID *uuid.UUID) *gorm.DB {
	if tenantID != nil {
		db = db.Where("tenant_id = ?", *tenantID)
	}
	if parentID != nil {
		db = db.Where("parent_id = ?", *parentID)
	}
	return db
}

func (r *privateScheduleRequestRepository) Create(ctx context.Context, request *domain.PrivateScheduleRequest) error {
	db := GetDB(ctx, r.db)
	// Serialize against the student deletion path and verify the ownership using the
	// same locked row which supplies the persisted parent ID.
	var student domain.Student
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND parent_id = ? AND deleted_at IS NULL", request.StudentID, request.ParentID).First(&student).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrStudentOwnership
		}
		return err
	}
	var class domain.Class
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", request.ClassID, request.TenantID).First(&class).Error; err != nil {
		return err
	}
	if class.Type != "private" {
		return domain.ErrPrivateClassRequired
	}
	if !class.IsPublished || class.EnrollmentStatus != "open" {
		return domain.ErrClassNotEnrollable
	}
	var count int64
	if err := db.Model(&domain.Enrollment{}).Where("student_id = ? AND class_id = ? AND status IN ? AND deleted_at IS NULL", request.StudentID, request.ClassID, []string{"pending", "active"}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return domain.ErrDuplicateEnrollment
	}
	if err := db.Create(request).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == "idx_private_request_pending_student_class" {
			return domain.ErrPrivateRequestConflict
		}
		return err
	}
	return nil
}

func (r *privateScheduleRequestRepository) Get(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	var request domain.PrivateScheduleRequest
	err := privateRequestScope(GetDB(ctx, r.db).Where("id = ?", id), tenantID, parentID).First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrPrivateRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &request, nil
}

func (r *privateScheduleRequestRepository) LockForTenant(ctx context.Context, id, tenantID uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	var request domain.PrivateScheduleRequest
	err := GetDB(ctx, r.db).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrPrivateRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &request, nil
}

func (r *privateScheduleRequestRepository) LockForParent(ctx context.Context, id, parentID uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	var request domain.PrivateScheduleRequest
	err := GetDB(ctx, r.db).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND parent_id = ?", id, parentID).First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrPrivateRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &request, nil
}

func (r *privateScheduleRequestRepository) RejectWithRecommendation(ctx context.Context, id, tenantID uuid.UUID, reason *string, slots []domain.PrivateScheduleSlot) (*domain.PrivateScheduleRequest, error) {
	encoded, err := json.Marshal(slots)
	if err != nil {
		return nil, err
	}
	result := GetDB(ctx, r.db).Model(&domain.PrivateScheduleRequest{}).Where("id = ? AND tenant_id = ? AND status = 'pending'", id, tenantID).Updates(map[string]interface{}{
		"status": "rejected", "rejection_reason": reason, "recommended_slots": string(encoded), "decided_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if _, err := r.Get(ctx, id, &tenantID, nil); err != nil {
			return nil, err
		}
		return nil, domain.ErrPrivateRequestTransition
	}
	return r.Get(ctx, id, &tenantID, nil)
}

func (r *privateScheduleRequestRepository) DeclineRecommendation(ctx context.Context, id, parentID uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	result := GetDB(ctx, r.db).Model(&domain.PrivateScheduleRequest{}).Where("id = ? AND parent_id = ? AND status = 'rejected' AND recommended_slots IS NOT NULL", id, parentID).Update("status", "declined")
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if _, err := r.Get(ctx, id, nil, &parentID); err != nil {
			return nil, err
		}
		return nil, domain.ErrPrivateRequestTransition
	}
	return r.Get(ctx, id, nil, &parentID)
}

func (r *privateScheduleRequestRepository) SetStatus(ctx context.Context, id, tenantID uuid.UUID, status string) error {
	result := GetDB(ctx, r.db).Model(&domain.PrivateScheduleRequest{}).Where("id = ? AND tenant_id = ?", id, tenantID).Updates(map[string]interface{}{"status": status, "decided_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domain.ErrPrivateRequestNotFound
	}
	return nil
}

func (r *privateScheduleRequestRepository) List(ctx context.Context, tenantID, parentID *uuid.UUID, status string) ([]domain.PrivateScheduleRequest, error) {
	db := privateRequestScope(GetDB(ctx, r.db).Model(&domain.PrivateScheduleRequest{}), tenantID, parentID)
	if status != "" {
		db = db.Where("status = ?", status)
	}
	var requests []domain.PrivateScheduleRequest
	err := db.Order("created_at DESC, id DESC").Limit(100).Find(&requests).Error
	return requests, err
}

func (r *privateScheduleRequestRepository) Transition(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID, status string, reason *string) (*domain.PrivateScheduleRequest, error) {
	db := GetDB(ctx, r.db)
	now := time.Now().UTC()
	changes := map[string]interface{}{"status": status, "decided_at": now, "rejection_reason": reason}
	result := privateRequestScope(db.Model(&domain.PrivateScheduleRequest{}).Where("id = ? AND status = 'pending'", id), tenantID, parentID).Updates(changes)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if _, err := r.Get(ctx, id, tenantID, parentID); err != nil {
			return nil, err
		}
		return nil, domain.ErrPrivateRequestTransition
	}
	return r.Get(ctx, id, tenantID, parentID)
}
