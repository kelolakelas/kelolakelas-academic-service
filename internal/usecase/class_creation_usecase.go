package usecase

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
	"gorm.io/gorm"
)

var ErrPrivateSchedulesNotAllowed = errors.New("private classes cannot include initial schedules")

type classCreationUsecase struct {
	txManager        repository.TransactionManager
	categoryRepo     repository.CategoryRepository
	classRepo        repository.ClassRepository
	classTeacherRepo repository.ClassTeacherRepository
	scheduleRepo     repository.ScheduleRepository
	sessionRepo      repository.SessionRepository
	tenantClient     grpcclient.TenantClient
}

func NewClassCreationUsecase(
	txManager repository.TransactionManager,
	categoryRepo repository.CategoryRepository,
	classRepo repository.ClassRepository,
	classTeacherRepo repository.ClassTeacherRepository,
	scheduleRepo repository.ScheduleRepository,
	sessionRepo repository.SessionRepository,
	tenantClient grpcclient.TenantClient,
) ClassCreationUsecase {
	return &classCreationUsecase{
		txManager: txManager, categoryRepo: categoryRepo, classRepo: classRepo,
		classTeacherRepo: classTeacherRepo,
		scheduleRepo:     scheduleRepo, sessionRepo: sessionRepo, tenantClient: tenantClient,
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
	response := &domain.CreateClassWithCategoryResponse{}
	err = u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		category, err := u.categoryRepo.GetByID(txCtx, req.CategoryID)
		if errors.Is(err, gorm.ErrRecordNotFound) || category == nil {
			return domain.ErrCategoryNotFound
		}
		if err != nil {
			return err
		}
		if category.TenantID != tenantID {
			return domain.ErrCategoryForbidden
		}

		status := req.Class.EnrollmentStatus
		if status == "" {
			status = "open"
		}
		class := &domain.Class{ID: uuid.New(), TenantID: tenantID, CategoryID: category.ID, Name: req.Class.Name, Description: req.Class.Description, Type: req.Class.Type, Price: req.Class.Price, IsPublished: req.Class.IsPublished, EnrollmentStatus: status}
		if err := u.classRepo.Create(txCtx, class); err != nil {
			return err
		}
		for _, teacherID := range req.TeacherIDs {
			if err := u.classTeacherRepo.Assign(txCtx, &domain.ClassTeacher{ClassID: class.ID, TeacherID: teacherID}); err != nil {
				return err
			}
		}

		response.Class = class
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}
