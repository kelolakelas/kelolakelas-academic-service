package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
	options          ClassCreationOptions
}

func NewClassCreationUsecase(
	txManager repository.TransactionManager,
	categoryRepo repository.CategoryRepository,
	classRepo repository.ClassRepository,
	classTeacherRepo repository.ClassTeacherRepository,
	scheduleRepo repository.ScheduleRepository,
	sessionRepo repository.SessionRepository,
	tenantClient grpcclient.TenantClient,
	options ...ClassCreationOptions,
) ClassCreationUsecase {
	var opts ClassCreationOptions
	if len(options) > 0 {
		opts = options[0]
	}
	return &classCreationUsecase{
		options:   opts,
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
	if err := validateClassCreation(req); err != nil {
		return nil, err
	}
	isActive, _, err := u.tenantClient.ValidateTenantStatus(ctx, tenantID.String())
	if err != nil || !isActive {
		return nil, ErrTenantInactiveOrNotFound
	}
	response := &domain.CreateClassWithCategoryResponse{}
	err = u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		// The idempotency row is locked by PostgreSQL's unique insert. Both the
		// response and all domain writes commit together; failures leave no claim.
		var fingerprint string
		if req.IdempotencyKey != "" {
			if u.options.Store == nil {
				return errors.New("class creation store unavailable")
			}
			payload, err := json.Marshal(req)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(payload)
			fingerprint = hex.EncodeToString(sum[:])
			replay, err := u.options.Store.Begin(txCtx, tenantID, req.IdempotencyKey, fingerprint)
			if err != nil {
				return err
			}
			if replay != nil {
				response = replay
				return nil
			}
		}
		teachers := make(map[uuid.UUID]bool)
		for _, id := range req.TeacherIDs {
			if id == uuid.Nil || teachers[id] {
				return ErrInvalidClassCreation
			}
			teachers[id] = true
		}
		for _, item := range req.Schedules {
			if item.TutorID != nil && !teachers[*item.TutorID] {
				return ErrInvalidClassCreation
			}
		}
		for id := range teachers {
			if u.options.Membership == nil {
				return ErrSubstituteTutorUnavailable
			}
			active, err := u.options.Membership.CheckActiveMember(txCtx, tenantID.String(), id.String())
			if err != nil {
				return ErrSubstituteTutorUnavailable
			}
			if !active {
				return ErrSubstituteTutorNotEligible
			}
		}
		var category *domain.Category
		if req.Category != nil {
			category = &domain.Category{ID: uuid.New(), TenantID: tenantID, Name: req.Category.Name, Description: req.Category.Description}
			if err := u.categoryRepo.Create(txCtx, category); err != nil {
				return err
			}
		} else {
			var err error
			if u.options.Store != nil {
				category, err = u.options.Store.CategoryForCreation(txCtx, tenantID, req.CategoryID)
			} else {
				category, err = u.categoryRepo.GetByID(txCtx, req.CategoryID)
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if category == nil || errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrCategoryNotFound
			}
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
		response.Category = category
		for _, item := range req.Schedules {
			from := normalizeDate(time.Now())
			if item.ValidFrom != nil {
				from = normalizeDate(*item.ValidFrom)
			}
			until := endOfMonth(from)
			start, _ := parseScheduleTime(item.StartTime)
			end, _ := parseScheduleTime(item.EndTime)
			schedule := &domain.ClassSchedule{ID: uuid.New(), ClassID: class.ID, TutorID: item.TutorID, Capacity: item.Capacity, Location: item.Location, DayOfWeek: item.DayOfWeek, StartTime: start.Format("15:04:05"), EndTime: end.Format("15:04:05"), ValidFrom: &from, ValidUntil: item.ValidUntil, SessionsGeneratedUntil: &until}
			if err := u.scheduleRepo.Create(txCtx, schedule); err != nil {
				return err
			}
			sessions := generateSessionsForSchedule(schedule, from)
			if len(sessions) > 0 {
				if err := u.sessionRepo.BatchCreate(txCtx, sessions); err != nil {
					return err
				}
			}
			response.Schedules = append(response.Schedules, schedule)
			response.Sessions = append(response.Sessions, sessions...)
		}
		if req.IdempotencyKey != "" {
			return u.options.Store.Complete(txCtx, tenantID, req.IdempotencyKey, response)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

// ClassCreationOptions supplies identity validation and transaction-bound retry storage.
type ClassCreationOptions struct {
	Membership grpcclient.MembershipClient
	Store      repository.ClassCreationStore
}

var ErrInvalidClassCreation = errors.New("invalid class creation payload")

func validateClassCreation(req *domain.CreateClassWithCategoryRequest) error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidClassCreation, reason) }
	if req == nil {
		return invalid("payload required")
	}
	if (req.CategoryID == uuid.Nil) == (req.Category == nil) {
		return invalid("provide exactly one of category_id or category")
	}
	if req.Category != nil && (strings.TrimSpace(req.Category.Name) == "" || len([]rune(req.Category.Name)) > 255) {
		return invalid("category name required, maximum 255 characters")
	}
	if strings.TrimSpace(req.Class.Name) == "" || len([]rune(req.Class.Name)) > 255 || (req.Class.Type != "private" && req.Class.Type != "group") || req.Class.Price < 0 {
		return invalid("invalid class")
	}
	switch req.Class.EnrollmentStatus {
	case "", "open", "closed", "full", "archived":
	default:
		return invalid("invalid enrollment status")
	}
	if len(req.TeacherIDs) == 0 || len(req.Schedules) > 366 {
		return invalid("teachers required; maximum 366 slots")
	}
	if len(req.IdempotencyKey) > 128 || strings.TrimSpace(req.IdempotencyKey) != req.IdempotencyKey {
		return invalid("invalid idempotency key")
	}
	if (req.Category != nil || len(req.Schedules) > 0) && req.IdempotencyKey == "" {
		return invalid("Idempotency-Key is required for aggregate creation")
	}
	if req.Class.Type == "private" && len(req.Schedules) > 0 {
		return ErrPrivateSchedulesNotAllowed
	}
	for _, item := range req.Schedules {
		start, se := parseScheduleTime(item.StartTime)
		end, ee := parseScheduleTime(item.EndTime)
		if se != nil || ee != nil || !start.Before(end) || item.DayOfWeek < 1 || item.DayOfWeek > 7 || item.Capacity < 1 || int64(item.Capacity) > 2147483647 || item.EnrollmentID != nil || (item.Location != nil && len([]rune(*item.Location)) > 255) {
			return invalid("invalid group schedule")
		}
		from := normalizeDate(time.Now())
		if item.ValidFrom != nil {
			from = normalizeDate(*item.ValidFrom)
		}
		if item.ValidUntil != nil && from.After(normalizeDate(*item.ValidUntil)) {
			return invalid("invalid schedule date range")
		}
	}
	return nil
}
