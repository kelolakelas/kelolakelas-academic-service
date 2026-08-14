package usecase

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type StudentUsecase interface {
	List(ctx context.Context, tenantID, parentID *uuid.UUID, query domain.StudentQuery) (*domain.StudentListResponse, error)
	Create(ctx context.Context, tenantID, userID *uuid.UUID, req *domain.CreateStudentRequest) (*domain.Student, error)
	GetByID(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.Student, error)
	Update(ctx context.Context, tenantID, parentID, authorID *uuid.UUID, id uuid.UUID, req *domain.UpdateStudentRequest) (*domain.Student, error)
	Delete(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) error
}

type studentUsecase struct {
	repo     repository.StudentRepository
	noteRepo repository.StudentNoteRepository
	tx       repository.TransactionManager
}

func NewStudentUsecase(repo repository.StudentRepository, noteRepo repository.StudentNoteRepository, tx repository.TransactionManager) StudentUsecase {
	return &studentUsecase{repo: repo, noteRepo: noteRepo, tx: tx}
}

func (u *studentUsecase) List(ctx context.Context, tenantID, parentID *uuid.UUID, query domain.StudentQuery) (*domain.StudentListResponse, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 || query.PageSize > 100 {
		query.PageSize = 20
	}
	items, total, err := u.repo.List(ctx, tenantID, parentID, query)
	if err != nil {
		return nil, err
	}
	return &domain.StudentListResponse{Items: items, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}}, nil
}

func (u *studentUsecase) Create(ctx context.Context, tenantID, userID *uuid.UUID, req *domain.CreateStudentRequest) (*domain.Student, error) {
	if tenantID == nil && (userID == nil || *userID != req.ParentID) {
		return nil, domain.ErrStudentForbidden
	}
	dob, err := time.Parse("2006-01-02", req.DateOfBirth)
	if err != nil {
		return nil, err
	}
	firstName := strings.TrimSpace(req.FirstName)
	if firstName == "" {
		return nil, domain.ErrStudentFirstNameRequired
	}
	student := &domain.Student{ID: uuid.New(), ParentID: req.ParentID, FirstName: firstName, LastName: cleanOptional(req.LastName), Nickname: cleanOptional(req.Nickname), Gender: cleanOptional(req.Gender), DateOfBirth: &dob}
	notes, err := u.buildStudentNotes(tenantID, userID, student, req.StudentNotes)
	if err != nil {
		return nil, err
	}
	if err := u.tx.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := u.repo.Create(txCtx, student); err != nil {
			return err
		}
		for _, note := range notes {
			if err := u.noteRepo.Create(txCtx, note); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return student, nil
}

func (u *studentUsecase) GetByID(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.Student, error) {
	return u.repo.GetByIDForAccess(ctx, id, tenantID, parentID)
}

func (u *studentUsecase) Update(ctx context.Context, tenantID, parentID, authorID *uuid.UUID, id uuid.UUID, req *domain.UpdateStudentRequest) (*domain.Student, error) {
	student, err := u.GetByID(ctx, tenantID, parentID, id)
	if err != nil {
		return nil, err
	}
	dob, err := time.Parse("2006-01-02", req.DateOfBirth)
	if err != nil {
		return nil, err
	}
	firstName := strings.TrimSpace(req.FirstName)
	if firstName == "" {
		return nil, domain.ErrStudentFirstNameRequired
	}
	student.FirstName, student.LastName, student.Nickname, student.Gender = firstName, cleanOptional(req.LastName), cleanOptional(req.Nickname), cleanOptional(req.Gender)
	student.DateOfBirth, student.UpdatedAt = &dob, time.Now()
	notes, err := u.buildStudentNotes(tenantID, authorID, student, req.StudentNotes)
	if err != nil {
		return nil, err
	}
	if err := u.tx.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := u.repo.Update(txCtx, student); err != nil {
			return err
		}
		for _, note := range notes {
			if err := u.noteRepo.Create(txCtx, note); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return student, nil
}

func (u *studentUsecase) buildStudentNotes(tenantID, authorID *uuid.UUID, student *domain.Student, requests []domain.StudentNoteRequest) ([]*domain.StudentNote, error) {
	notes := make([]*domain.StudentNote, 0, len(requests))
	for _, request := range requests {
		note, err := u.buildStudentNote(tenantID, authorID, student, request)
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, nil
}

func (u *studentUsecase) buildStudentNote(tenantID, authorID *uuid.UUID, student *domain.Student, req domain.StudentNoteRequest) (*domain.StudentNote, error) {
	if authorID == nil || *authorID == uuid.Nil {
		return nil, domain.ErrStudentNoteInvalid
	}
	if tenantID == nil || *tenantID == uuid.Nil {
		if student == nil {
			return nil, domain.ErrStudentNoteInvalid
		}
		if *authorID != student.ParentID {
			return nil, domain.ErrStudentForbidden
		}
		tenantID = nil
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, domain.ErrStudentNoteContentRequired
	}
	if req.NoteType != "medical" && req.NoteType != "academic" && req.NoteType != "behavioral" {
		return nil, domain.ErrStudentNoteInvalid
	}
	return &domain.StudentNote{ID: uuid.New(), TenantID: tenantID, StudentID: student.ID, AuthorID: *authorID, NoteType: req.NoteType, Content: content}, nil
}

func cleanOptional(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func (u *studentUsecase) Delete(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) error {
	if _, err := u.GetByID(ctx, tenantID, parentID, id); err != nil {
		return err
	}
	count, err := u.repo.CountActiveEnrollments(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return domain.ErrStudentActiveEnroll
	}
	return u.repo.Delete(ctx, id)
}
