package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrClassCreationKeyConflict = errors.New("idempotency key already used with a different payload")

type ClassCreationStore interface {
	Begin(context.Context, uuid.UUID, string, string) (*domain.CreateClassWithCategoryResponse, error)
	Complete(context.Context, uuid.UUID, string, *domain.CreateClassWithCategoryResponse) error
	CategoryForCreation(context.Context, uuid.UUID, uuid.UUID) (*domain.Category, error)
}
type classCreationStore struct{ db *gorm.DB }

func NewClassCreationStore(db *gorm.DB) ClassCreationStore { return &classCreationStore{db: db} }
func (s *classCreationStore) Begin(ctx context.Context, tenant uuid.UUID, key, hash string) (*domain.CreateClassWithCategoryResponse, error) {
	db := GetDB(ctx, s.db)
	result := db.Exec("INSERT INTO class_creation_requests (tenant_id, request_key, payload_hash) VALUES (?, ?, ?) ON CONFLICT DO NOTHING", tenant, key, hash)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 1 {
		return nil, nil
	}
	var row struct {
		PayloadHash string
		Response    json.RawMessage
	}
	if err := db.Raw("SELECT payload_hash, response FROM class_creation_requests WHERE tenant_id = ? AND request_key = ? FOR UPDATE", tenant, key).Scan(&row).Error; err != nil {
		return nil, err
	}
	if row.PayloadHash != hash {
		return nil, ErrClassCreationKeyConflict
	}
	var response domain.CreateClassWithCategoryResponse
	if err := json.Unmarshal(row.Response, &response); err != nil {
		return nil, err
	}
	return &response, nil
}
func (s *classCreationStore) Complete(ctx context.Context, tenant uuid.UUID, key string, response *domain.CreateClassWithCategoryResponse) error {
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return GetDB(ctx, s.db).Exec("UPDATE class_creation_requests SET response = ?::jsonb WHERE tenant_id = ? AND request_key = ?", string(data), tenant, key).Error
}
func (s *classCreationStore) CategoryForCreation(ctx context.Context, tenant, id uuid.UUID) (*domain.Category, error) {
	var category domain.Category
	err := GetDB(ctx, s.db).Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND tenant_id = ?", id, tenant).First(&category).Error
	return &category, err
}
