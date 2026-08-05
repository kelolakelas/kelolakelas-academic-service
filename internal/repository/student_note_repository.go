package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type studentNoteRepository struct{ db *gorm.DB }

func NewStudentNoteRepository(db *gorm.DB) StudentNoteRepository {
	return &studentNoteRepository{db: db}
}

func (r *studentNoteRepository) Create(ctx context.Context, note *domain.StudentNote) error {
	return GetDB(ctx, r.db).Create(note).Error
}

func (r *studentNoteRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.StudentNote, error) {
	var note domain.StudentNote
	if err := GetDB(ctx, r.db).First(&note, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, err
	}
	return &note, nil
}

func (r *studentNoteRepository) Update(ctx context.Context, note *domain.StudentNote) error {
	return GetDB(ctx, r.db).Save(note).Error
}

func (r *studentNoteRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return GetDB(ctx, r.db).Delete(&domain.StudentNote{}, "id = ?", id).Error
}
