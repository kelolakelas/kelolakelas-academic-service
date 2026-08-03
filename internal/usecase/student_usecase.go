package usecase

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type StudentUsecase interface {
	List(ctx context.Context, tenantID, parentID *uuid.UUID, query domain.StudentQuery) (*domain.StudentListResponse, error)
	Create(ctx context.Context, tenantID, userID *uuid.UUID, req *domain.CreateStudentRequest) (*domain.Student, error)
	GetByID(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.Student, error)
	Update(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID, req *domain.UpdateStudentRequest) (*domain.Student, error)
	Delete(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) error
}

type studentUsecase struct{ repo repository.StudentRepository }

func NewStudentUsecase(repo repository.StudentRepository) StudentUsecase {
	return &studentUsecase{repo: repo}
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
	student := &domain.Student{ID: uuid.New(), ParentID: req.ParentID, FullName: req.FullName, DateOfBirth: &dob}
	if err := u.repo.Create(ctx, student); err != nil {
		return nil, err
	}
	return student, nil
}

func (u *studentUsecase) GetByID(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.Student, error) {
	return u.repo.GetByIDForAccess(ctx, id, tenantID, parentID)
}

func (u *studentUsecase) Update(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID, req *domain.UpdateStudentRequest) (*domain.Student, error) {
	student, err := u.GetByID(ctx, tenantID, parentID, id)
	if err != nil {
		return nil, err
	}
	dob, err := time.Parse("2006-01-02", req.DateOfBirth)
	if err != nil {
		return nil, err
	}
	student.FullName, student.DateOfBirth, student.UpdatedAt = req.FullName, &dob, time.Now()
	if err := u.repo.Update(ctx, student); err != nil {
		return nil, err
	}
	return student, nil
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
