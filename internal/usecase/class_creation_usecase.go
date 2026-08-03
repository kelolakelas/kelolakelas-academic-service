package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

var ErrPrivateSchedulesNotAllowed = errors.New("private classes cannot include initial schedules")

type classCreationUsecase struct {
	txManager    repository.TransactionManager
	categoryRepo repository.CategoryRepository
	classRepo    repository.ClassRepository
	scheduleRepo repository.ScheduleRepository
	sessionRepo  repository.SessionRepository
	tenantClient grpcclient.TenantClient
}

func NewClassCreationUsecase(
	txManager repository.TransactionManager,
	categoryRepo repository.CategoryRepository,
	classRepo repository.ClassRepository,
	scheduleRepo repository.ScheduleRepository,
	sessionRepo repository.SessionRepository,
	tenantClient grpcclient.TenantClient,
) ClassCreationUsecase {
	return &classCreationUsecase{
		txManager: txManager, categoryRepo: categoryRepo, classRepo: classRepo,
		scheduleRepo: scheduleRepo, sessionRepo: sessionRepo, tenantClient: tenantClient,
	}
}

func (u *classCreationUsecase) CreateClassWithCategory(
	ctx context.Context,
	tenantID uuid.UUID,
	req *domain.CreateClassWithCategoryRequest,
) (*domain.CreateClassWithCategoryResponse, error) {
	isActive, _, err := u.tenantClient.ValidateTenantStatus(ctx, tenantID.String())
	if err != nil || !isActive {
		return nil, ErrTenantInactiveOrNotFound
	}
	if req.Class.Type == "private" && len(req.Schedules) > 0 {
		return nil, ErrPrivateSchedulesNotAllowed
	}
	if req.Class.Type == "group" && len(req.Schedules) == 0 {
		return nil, errors.New("group classes require at least one schedule")
	}

	response := &domain.CreateClassWithCategoryResponse{}
	err = u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		category := &domain.Category{ID: uuid.New(), TenantID: tenantID, Name: req.Category.Name, Description: req.Category.Description}
		if err := u.categoryRepo.Create(txCtx, category); err != nil {
			return err
		}

		class := &domain.Class{ID: uuid.New(), TenantID: tenantID, CategoryID: category.ID, Name: req.Class.Name, Description: req.Class.Description, Type: req.Class.Type, Price: req.Class.Price, Capacity: req.Class.Capacity}
		if err := u.classRepo.Create(txCtx, class); err != nil {
			return err
		}

		response.Category = category
		response.Class = class
		now := normalizeDate(time.Now())
		for _, item := range req.Schedules {
			validFrom := now
			if item.ValidFrom != nil {
				validFrom = normalizeDate(*item.ValidFrom)
			}
			schedule := &domain.ClassSchedule{ID: uuid.New(), ClassID: class.ID, TutorID: item.TutorID, Location: item.Location, DayOfWeek: item.DayOfWeek, StartTime: item.StartTime, EndTime: item.EndTime, ValidFrom: &validFrom}
			if err := u.scheduleRepo.Create(txCtx, schedule); err != nil {
				return err
			}
			response.Schedules = append(response.Schedules, schedule)
			sessions := generateSessionsForSchedule(schedule, validFrom)
			if err := u.sessionRepo.BatchCreate(txCtx, sessions); err != nil {
				return err
			}
			response.Sessions = append(response.Sessions, sessions...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}
