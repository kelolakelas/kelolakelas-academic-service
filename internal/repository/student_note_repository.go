package repository

import (
	"context"

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

func (r *studentNoteRepository) GetByIDForAccess(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID) (*domain.StudentNote, error) {
	var note domain.StudentNote
	db := GetDB(ctx, r.db).Joins("JOIN students ON students.id = student_notes.student_id").Where("student_notes.id = ?", id)
	db = applyStudentNoteAccess(db, tenantID, parentID)
	if err := db.First(&note).Error; err != nil {
		return nil, err
	}
	return &note, nil
}

func (r *studentNoteRepository) UpdateForAccess(ctx context.Context, note *domain.StudentNote, tenantID, parentID *uuid.UUID) error {
	db := applyStudentNoteAccess(GetDB(ctx, r.db).Model(&domain.StudentNote{}).Where("student_notes.id = ?", note.ID), tenantID, parentID)
	return db.Updates(map[string]interface{}{"note_type": note.NoteType, "content": note.Content, "updated_at": note.UpdatedAt}).Error
}

func (r *studentNoteRepository) DeleteForAccess(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID) error {
	db := applyStudentNoteAccess(GetDB(ctx, r.db).Where("student_notes.id = ?", id), tenantID, parentID)
	return db.Delete(&domain.StudentNote{}).Error
}

func applyStudentNoteAccess(db *gorm.DB, tenantID, parentID *uuid.UUID) *gorm.DB {
	if tenantID != nil && *tenantID != uuid.Nil {
		return db.Where("student_notes.tenant_id = ?", *tenantID)
	}
	if parentID != nil && *parentID != uuid.Nil {
		return db.Where("student_notes.tenant_id IS NULL AND student_notes.student_id IN (SELECT id FROM students WHERE parent_id = ?)", *parentID)
	}
	return db.Where("1 = 0")
}
