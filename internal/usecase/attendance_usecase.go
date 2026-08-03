package usecase

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type AttendanceUsecase interface {
	List(ctx context.Context, tenantID uuid.UUID, query domain.AttendanceQuery) (*domain.AttendanceListResponse, error)
	Create(ctx context.Context, tenantID, memberID uuid.UUID, req *domain.CreateAttendanceRequest) (*domain.Attendance, error)
	Get(ctx context.Context, tenantID, id uuid.UUID) (*domain.Attendance, error)
	Update(ctx context.Context, tenantID, memberID, id uuid.UUID, req *domain.UpdateAttendanceRequest) (*domain.Attendance, error)
}

type attendanceUsecase struct {
	repo     repository.AttendanceRepository
	sessions repository.SessionRepository
}

func NewAttendanceUsecase(repo repository.AttendanceRepository, sessions repository.SessionRepository) AttendanceUsecase {
	return &attendanceUsecase{repo: repo, sessions: sessions}
}

func (u *attendanceUsecase) List(ctx context.Context, tenantID uuid.UUID, query domain.AttendanceQuery) (*domain.AttendanceListResponse, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 || query.PageSize > 100 {
		query.PageSize = 20
	}
	items, total, err := u.repo.List(ctx, tenantID, query)
	if err != nil {
		return nil, err
	}
	return &domain.AttendanceListResponse{Items: items, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}}, nil
}
func (u *attendanceUsecase) Create(ctx context.Context, tenantID, memberID uuid.UUID, req *domain.CreateAttendanceRequest) (*domain.Attendance, error) {
	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		return nil, err
	}
	session, err := u.sessions.FindForAttendance(ctx, tenantID, req.ScheduleID, req.EnrollmentID, date)
	if err != nil {
		return nil, err
	}
	assigned, err := u.sessions.IsTutorForSession(ctx, tenantID, session.ID, memberID)
	if err != nil {
		return nil, err
	}
	if !assigned {
		return nil, domain.ErrAttendanceForbidden
	}
	existing, err := u.repo.GetByUnique(ctx, req.EnrollmentID, session.ID, date)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, domain.ErrAttendanceDuplicate
	}
	item := &domain.Attendance{ID: uuid.New(), EnrollmentID: req.EnrollmentID, SessionID: session.ID, Date: date, Status: req.Status}
	if err := u.repo.Create(ctx, item); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, domain.ErrAttendanceDuplicate
		}
		return nil, err
	}
	return item, nil
}
func (u *attendanceUsecase) Get(ctx context.Context, tenantID, id uuid.UUID) (*domain.Attendance, error) {
	return u.repo.GetByIDForTenant(ctx, tenantID, id)
}
func (u *attendanceUsecase) Update(ctx context.Context, tenantID, memberID, id uuid.UUID, req *domain.UpdateAttendanceRequest) (*domain.Attendance, error) {
	item, err := u.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	assigned, err := u.sessions.IsTutorForSession(ctx, tenantID, item.SessionID, memberID)
	if err != nil {
		return nil, err
	}
	if !assigned {
		return nil, domain.ErrAttendanceForbidden
	}
	item.Status = req.Status
	item.UpdatedAt = time.Now()
	if err := u.repo.Update(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}
