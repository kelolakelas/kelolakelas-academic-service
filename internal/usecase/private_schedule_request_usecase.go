package usecase

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type PrivateScheduleRequestUsecase interface {
	Create(context.Context, uuid.UUID, uuid.UUID, *domain.CreatePrivateScheduleRequest) (*domain.PrivateScheduleRequest, error)
	Get(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) (*domain.PrivateScheduleRequest, error)
	List(context.Context, *uuid.UUID, *uuid.UUID, string) ([]domain.PrivateScheduleRequest, error)
	Reject(context.Context, uuid.UUID, uuid.UUID, *string) (*domain.PrivateScheduleRequest, error)
	Cancel(context.Context, uuid.UUID, uuid.UUID) (*domain.PrivateScheduleRequest, error)
}

type privateScheduleRequestUsecase struct {
	repo     repository.PrivateScheduleRequestRepository
	students repository.StudentRepository
	classes  repository.ClassRepository
	tx       repository.TransactionManager
}

func NewPrivateScheduleRequestUsecase(repo repository.PrivateScheduleRequestRepository, students repository.StudentRepository, classes repository.ClassRepository, tx repository.TransactionManager) PrivateScheduleRequestUsecase {
	return &privateScheduleRequestUsecase{repo: repo, students: students, classes: classes, tx: tx}
}

func validatePrivateSlots(slots []domain.PrivateScheduleSlot) error {
	if len(slots) == 0 || len(slots) > 50 {
		return domain.ErrPrivateRequestSlots
	}
	grouped := make(map[int][][2]time.Time)
	for _, slot := range slots {
		if slot.DayOfWeek < 1 || slot.DayOfWeek > 7 {
			return domain.ErrPrivateRequestSlots
		}
		start, err := time.Parse("15:04:05", slot.StartTime)
		if err != nil || start.Format("15:04:05") != slot.StartTime {
			return domain.ErrPrivateRequestSlots
		}
		end, err := time.Parse("15:04:05", slot.EndTime)
		if err != nil || end.Format("15:04:05") != slot.EndTime || !end.After(start) {
			return domain.ErrPrivateRequestSlots
		}
		grouped[slot.DayOfWeek] = append(grouped[slot.DayOfWeek], [2]time.Time{start, end})
	}
	for _, day := range grouped {
		sort.Slice(day, func(i, j int) bool { return day[i][0].Before(day[j][0]) })
		for i := 1; i < len(day); i++ {
			if day[i][0].Before(day[i-1][1]) {
				return domain.ErrPrivateRequestSlots
			}
		}
	}
	return nil
}

func (u *privateScheduleRequestUsecase) Create(ctx context.Context, parentID, classID uuid.UUID, req *domain.CreatePrivateScheduleRequest) (*domain.PrivateScheduleRequest, error) {
	if parentID == uuid.Nil {
		return nil, domain.ErrParentRequired
	}
	if req == nil || req.StudentID == uuid.Nil || classID == uuid.Nil {
		return nil, domain.ErrPrivateRequestSlots
	}
	if req.BillingCycle != "monthly" && req.BillingCycle != "quarterly" && req.BillingCycle != "yearly" {
		return nil, errors.New("invalid billing cycle")
	}
	if err := validatePrivateSlots(req.Slots); err != nil {
		return nil, err
	}
	if req.Note != nil && len([]rune(*req.Note)) > 2000 {
		return nil, errors.New("note is too long")
	}
	student, err := u.students.GetByID(ctx, req.StudentID)
	if err != nil {
		if errors.Is(err, domain.ErrStudentNotFound) {
			return nil, domain.ErrStudentOwnership
		}
		return nil, err
	}
	if student.ParentID != parentID {
		return nil, domain.ErrStudentOwnership
	}
	class, err := u.classes.GetByID(ctx, classID)
	if err != nil {
		if errors.Is(err, domain.ErrClassNotFound) {
			return nil, err
		}
		return nil, err
	}
	if class.Type != "private" {
		return nil, domain.ErrPrivateClassRequired
	}
	if !class.IsPublished || class.EnrollmentStatus != "open" {
		return nil, domain.ErrClassNotEnrollable
	}
	request := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: class.TenantID, ClassID: classID, StudentID: req.StudentID, ParentID: parentID, ParentEmail: strings.TrimSpace(req.ParentEmail), BillingCycle: req.BillingCycle, Slots: req.Slots, Note: req.Note, Status: "pending", CreatedAt: time.Now().UTC()}
	create := func(txCtx context.Context) error { return u.repo.Create(txCtx, request) }
	if u.tx != nil {
		err = u.tx.WithTransaction(ctx, create)
	} else {
		err = create(ctx)
	}
	if err != nil {
		return nil, err
	}
	return request, nil
}

func (u *privateScheduleRequestUsecase) Get(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	if (tenantID == nil) == (parentID == nil) {
		return nil, domain.ErrPrivateRequestNotFound
	}
	return u.repo.Get(ctx, id, tenantID, parentID)
}
func (u *privateScheduleRequestUsecase) List(ctx context.Context, tenantID, parentID *uuid.UUID, status string) ([]domain.PrivateScheduleRequest, error) {
	if (tenantID == nil) == (parentID == nil) {
		return nil, domain.ErrPrivateRequestNotFound
	}
	if status != "" && status != "pending" && status != "approved" && status != "rejected" && status != "cancelled" {
		return nil, errors.New("invalid request status")
	}
	return u.repo.List(ctx, tenantID, parentID, status)
}
func (u *privateScheduleRequestUsecase) Reject(ctx context.Context, tenantID, id uuid.UUID, reason *string) (*domain.PrivateScheduleRequest, error) {
	if tenantID == uuid.Nil {
		return nil, domain.ErrPrivateRequestNotFound
	}
	if reason != nil {
		trimmed := strings.TrimSpace(*reason)
		if len([]rune(trimmed)) > 2000 {
			return nil, errors.New("reason is too long")
		}
		reason = &trimmed
	}
	return u.repo.Transition(ctx, id, &tenantID, nil, "rejected", reason)
}
func (u *privateScheduleRequestUsecase) Cancel(ctx context.Context, parentID, id uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	if parentID == uuid.Nil {
		return nil, domain.ErrParentRequired
	}
	return u.repo.Transition(ctx, id, nil, &parentID, "cancelled", nil)
}
