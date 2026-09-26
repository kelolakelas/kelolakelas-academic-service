package usecase

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"gorm.io/gorm"
)

var (
	ErrClassNotFound           = errors.New("class not found")
	ErrScheduleNotFound        = errors.New("class schedule not found")
	ErrSessionNotFound         = errors.New("class session not found")
	ErrEnrollmentRequired      = errors.New("enrollment_id is required for private classes")
	ErrEnrollmentNotFound      = errors.New("enrollment not found")
	ErrInvalidEnrollmentClass  = errors.New("enrollment does not belong to the specified class")
	ErrInvalidEnrollmentTenant = errors.New("enrollment does not belong to the specified tenant")
	ErrInvalidEffectiveDate    = errors.New("effective_date outside schedule validity")
)

type scheduleUsecase struct {
	txManager      repository.TransactionManager
	classRepo      repository.ClassRepository
	scheduleRepo   repository.ScheduleRepository
	sessionRepo    repository.SessionRepository
	enrollmentRepo repository.EnrollmentRepository
}

func (u *scheduleUsecase) ListSchedules(ctx context.Context, tenantID uuid.UUID, query domain.ListQuery) (*domain.ScheduleListResponse, error) {
	items, total, err := u.scheduleRepo.ListByTenant(ctx, tenantID, query)
	if err != nil {
		return nil, err
	}
	return &domain.ScheduleListResponse{Items: items, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}}, nil
}

func (u *scheduleUsecase) DeleteSchedule(ctx context.Context, tenantID, id uuid.UUID) error {
	return u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		schedule, err := u.scheduleRepo.GetByID(txCtx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) || schedule == nil {
			return ErrScheduleNotFound
		}
		if err != nil {
			return err
		}
		if schedule.Class == nil || schedule.Class.TenantID != tenantID {
			return domain.ErrScheduleForbidden
		}
		if err := u.scheduleRepo.DeleteByTenant(txCtx, tenantID, id); err != nil {
			return err
		}
		return u.sessionRepo.CancelFutureSessionsBySchedule(txCtx, tenantID, id, normalizeDate(time.Now()))
	})
}

func (u *scheduleUsecase) ListSessions(ctx context.Context, tenantID uuid.UUID, query domain.SessionQuery) (*domain.SessionListResponse, error) {
	items, total, err := u.sessionRepo.ListByTenant(ctx, tenantID, query)
	if err != nil {
		return nil, err
	}
	return &domain.SessionListResponse{Items: items, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}}, nil
}

func (u *scheduleUsecase) GetSession(ctx context.Context, tenantID, sessionID uuid.UUID) (*domain.ClassSession, error) {
	return u.sessionRepo.GetByIDForTenant(ctx, tenantID, sessionID)
}

func (u *scheduleUsecase) DeleteSession(ctx context.Context, tenantID, sessionID uuid.UUID) error {
	return u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		session, err := u.sessionRepo.GetByID(txCtx, sessionID)
		if errors.Is(err, gorm.ErrRecordNotFound) || session == nil {
			return ErrSessionNotFound
		}
		if err != nil {
			return err
		}
		if session.Class == nil || session.Class.TenantID != tenantID {
			return domain.ErrSessionForbidden
		}
		return u.sessionRepo.DeleteByTenant(txCtx, tenantID, sessionID)
	})
}

func NewScheduleUsecase(
	txManager repository.TransactionManager,
	classRepo repository.ClassRepository,
	scheduleRepo repository.ScheduleRepository,
	sessionRepo repository.SessionRepository,
	enrollmentRepo repository.EnrollmentRepository,
) ScheduleUsecase {
	return &scheduleUsecase{
		txManager:      txManager,
		classRepo:      classRepo,
		scheduleRepo:   scheduleRepo,
		sessionRepo:    sessionRepo,
		enrollmentRepo: enrollmentRepo,
	}
}

// Helper function to map Go's time.Weekday to ISO 8601 day of week (1 = Monday, ..., 7 = Sunday)
func getISOWeekday(t time.Time) int {
	wd := int(t.Weekday())
	if wd == 0 {
		return 7
	}
	return wd
}

// Helper function to normalize date (strip time components)
func normalizeDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// Helper function to generate ClassSessions for a schedule from a start date through the end of the month
func generateSessionsForSchedule(schedule *domain.ClassSchedule, fromDate time.Time) []*domain.ClassSession {
	fromDate = normalizeDate(fromDate)

	// End of current month for initial schedule generation
	firstOfNextMonth := time.Date(fromDate.Year(), fromDate.Month()+1, 1, 0, 0, 0, 0, fromDate.Location())
	endOfMonth := firstOfNextMonth.AddDate(0, 0, -1)

	endDate := endOfMonth
	if schedule.ValidUntil != nil {
		validUntilNorm := normalizeDate(*schedule.ValidUntil)
		if validUntilNorm.Before(endDate) {
			endDate = validUntilNorm
		}
	}

	var sessions []*domain.ClassSession
	for d := fromDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		if getISOWeekday(d) == schedule.DayOfWeek {
			var tutorID uuid.UUID
			if schedule.TutorID != nil {
				tutorID = *schedule.TutorID
			}

			sessions = append(sessions, &domain.ClassSession{
				ID:           uuid.New(),
				ClassID:      schedule.ClassID,
				ScheduleID:   &schedule.ID,
				EnrollmentID: schedule.EnrollmentID,
				TutorID:      tutorID,
				SessionDate:  d,
				StartTime:    schedule.StartTime,
				EndTime:      schedule.EndTime,
				Status:       "scheduled",
			})
		}
	}
	return sessions
}

// 1. Create Initial Schedules for an Existing Class
func (u *scheduleUsecase) CreateInitialSchedules(
	ctx context.Context,
	tenantID uuid.UUID,
	req *domain.CreateInitialSchedulesRequest,
) (*domain.CreateInitialSchedulesResponse, error) {
	// Fetch Class details first
	class, err := u.classRepo.GetByID(ctx, req.ClassID)
	if err != nil || class == nil {
		return nil, ErrClassNotFound
	}
	if class.TenantID != tenantID {
		return nil, domain.ErrClassForbidden
	}

	var createdSchedules []*domain.ClassSchedule
	var createdSessions []*domain.ClassSession

	err = u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		now := normalizeDate(time.Now())

		for _, item := range req.Schedules {
			start, startErr := parseScheduleTime(item.StartTime)
			end, endErr := parseScheduleTime(item.EndTime)
			if startErr != nil || endErr != nil || !start.Before(end) {
				return errors.New("start_time must be before end_time")
			}
			validFrom := now
			if item.ValidFrom != nil {
				validFrom = normalizeDate(*item.ValidFrom)
			}
			if item.ValidUntil != nil && validFrom.After(normalizeDate(*item.ValidUntil)) {
				return errors.New("valid_from must not be after valid_until")
			}

			var enrollmentID *uuid.UUID

			// Validation logic based on Class.type
			if class.Type == "group" {
				// IF Class.type == "group": Explicitly set enrollment_id to NULL
				enrollmentID = nil
			} else if class.Type == "private" {
				// IF Class.type == "private": Require enrollment_id from the payload
				if item.EnrollmentID == nil {
					return ErrEnrollmentRequired
				}

				// Validate that the provided enrollment_id belongs to the correct class_id and tenant_id
				enrollment, err := u.enrollmentRepo.GetByID(txCtx, *item.EnrollmentID)
				if err != nil || enrollment == nil {
					return ErrEnrollmentNotFound
				}
				if enrollment.ClassID != req.ClassID {
					return ErrInvalidEnrollmentClass
				}
				if enrollment.TenantID != class.TenantID {
					return ErrInvalidEnrollmentTenant
				}

				enrollmentID = item.EnrollmentID
			}

			schedule := &domain.ClassSchedule{
				ID:           uuid.New(),
				ClassID:      req.ClassID,
				EnrollmentID: enrollmentID,
				TutorID:      item.TutorID,
				Capacity:     item.Capacity,
				Location:     item.Location,
				DayOfWeek:    item.DayOfWeek,
				StartTime:    item.StartTime,
				EndTime:      item.EndTime,
				ValidFrom:    &validFrom,
			}
			if item.ValidUntil != nil {
				validUntil := normalizeDate(*item.ValidUntil)
				schedule.ValidUntil = &validUntil
			}

			if err := u.scheduleRepo.Create(txCtx, schedule); err != nil {
				return err
			}
			createdSchedules = append(createdSchedules, schedule)

			// Automatically generate physical meetings in Class_Sessions for the current month based on day_of_week
			sessions := generateSessionsForSchedule(schedule, validFrom)
			if len(sessions) > 0 {
				if err := u.sessionRepo.BatchCreate(txCtx, sessions); err != nil {
					return err
				}
				createdSessions = append(createdSessions, sessions...)
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &domain.CreateInitialSchedulesResponse{
		Schedules: createdSchedules,
		Sessions:  createdSessions,
	}, nil
}

func parseScheduleTime(value string) (time.Time, error) {
	for _, layout := range []string{"15:04:05", "15:04"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid schedule time")
}

// 2. Temporary Schedule Change (One-off Reschedule / Make-up Class)
func (u *scheduleUsecase) RescheduleSession(
	ctx context.Context,
	tenantID uuid.UUID,
	req *domain.RescheduleSessionRequest,
) (*domain.RescheduleSessionResponse, error) {
	var targetSession *domain.ClassSession
	var newSession *domain.ClassSession

	err := u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		var err error
		// The ownership filter is part of the query, so a session owned by another
		// tenant is reported exactly like a missing one and no row is touched.
		targetSession, err = u.sessionRepo.GetByIDForTenant(txCtx, tenantID, req.SessionID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		if err != nil {
			return err
		}
		if targetSession == nil {
			return ErrSessionNotFound
		}

		// Update target Class_Sessions to status = 'rescheduled'
		targetSession.Status = "rescheduled"
		if err := u.sessionRepo.Update(txCtx, targetSession); err != nil {
			return err
		}

		// Insert new Class_Sessions: keeping same class_id and tutor_id, schedule_id = null, status = 'scheduled'
		newSession = &domain.ClassSession{
			ID:           uuid.New(),
			ClassID:      targetSession.ClassID,
			ScheduleID:   nil, // schedule_id = null
			EnrollmentID: targetSession.EnrollmentID,
			TutorID:      targetSession.TutorID,
			SessionDate:  normalizeDate(req.NewSessionDate),
			StartTime:    req.NewStartTime,
			EndTime:      req.NewEndTime,
			Status:       "scheduled",
		}

		if err := u.sessionRepo.Create(txCtx, newSession); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &domain.RescheduleSessionResponse{
		OriginalSession: targetSession,
		NewSession:      newSession,
	}, nil
}

// A replacement must cover a nonempty portion of the original validity window.
func validateReplacementDate(schedule *domain.ClassSchedule, effectiveDate time.Time) error {
	if schedule.ValidFrom != nil && effectiveDate.Before(normalizeDate(*schedule.ValidFrom)) {
		return ErrInvalidEffectiveDate
	}
	if schedule.ValidUntil != nil && effectiveDate.After(normalizeDate(*schedule.ValidUntil)) {
		return ErrInvalidEffectiveDate
	}
	return nil
}

// 3. Permanent Schedule Change
func (u *scheduleUsecase) ChangeSchedulePermanent(
	ctx context.Context,
	tenantID uuid.UUID,
	req *domain.PermanentScheduleChangeRequest,
) (*domain.PermanentScheduleChangeResponse, error) {
	var oldSchedule *domain.ClassSchedule
	var newSchedule *domain.ClassSchedule
	var newSessions []*domain.ClassSession

	effectiveDate := normalizeDate(req.EffectiveDate)
	prevDay := effectiveDate.AddDate(0, 0, -1)

	err := u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		var err error
		oldSchedule, err = u.scheduleRepo.GetByIDForTenantForUpdate(txCtx, tenantID, req.OldScheduleID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrScheduleNotFound
		}
		if err != nil {
			return err
		}
		if oldSchedule == nil {
			return ErrScheduleNotFound
		}

		if err := validateReplacementDate(oldSchedule, effectiveDate); err != nil {
			return err
		}
		originalValidUntil := oldSchedule.ValidUntil

		// 1. Update old Class_Schedules setting valid_until = effective_date - 1 day
		oldSchedule.ValidUntil = &prevDay
		if err := u.scheduleRepo.Update(txCtx, oldSchedule); err != nil {
			return err
		}

		// 2. Create new Class_Schedules row with valid_from = effective_date
		newSchedule = &domain.ClassSchedule{
			ID:           uuid.New(),
			ClassID:      oldSchedule.ClassID,
			EnrollmentID: oldSchedule.EnrollmentID,
			TutorID:      oldSchedule.TutorID,
			Capacity:     oldSchedule.Capacity,
			Location:     oldSchedule.Location,
			DayOfWeek:    req.NewDayOfWeek,
			StartTime:    req.NewStartTime,
			EndTime:      req.NewEndTime,
			ValidFrom:    &effectiveDate,
			ValidUntil:   originalValidUntil,
		}
		if err := u.scheduleRepo.Create(txCtx, newSchedule); err != nil {
			return err
		}
		if err := u.enrollmentRepo.TransferSchedule(txCtx, tenantID, oldSchedule.ClassID, oldSchedule.ID, newSchedule.ID); err != nil {
			return err
		}

		// 3. Delete all future Class_Sessions linked to old schedule (from effective date onwards)
		if err := u.sessionRepo.CancelFutureSessionsBySchedule(txCtx, tenantID, oldSchedule.ID, effectiveDate); err != nil {
			return err
		}

		// 4. Generate new Class_Sessions based on the new schedule
		newSessions = generateSessionsForSchedule(newSchedule, effectiveDate)
		if len(newSessions) > 0 {
			if err := u.sessionRepo.BatchCreate(txCtx, newSessions); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &domain.PermanentScheduleChangeResponse{
		OldSchedule: oldSchedule,
		NewSchedule: newSchedule,
		NewSessions: newSessions,
	}, nil
}

// 4. Temporary Tutor Change (Substitute Teacher)
func (u *scheduleUsecase) ChangeTutorTemporary(
	ctx context.Context,
	tenantID uuid.UUID,
	req *domain.SubstituteTutorRequest,
) (*domain.SubstituteTutorResponse, error) {
	var session *domain.ClassSession

	err := u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		var err error
		session, err = u.sessionRepo.GetByIDForTenant(txCtx, tenantID, req.SessionID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		if err != nil {
			return err
		}
		if session == nil {
			return ErrSessionNotFound
		}

		// Do NOT mutate Class_Schedules. Simply update tutor_id in the specific Class_Sessions row.
		session.TutorID = req.SubstituteTutorID
		return u.sessionRepo.Update(txCtx, session)
	})
	if err != nil {
		return nil, err
	}

	return &domain.SubstituteTutorResponse{
		Session: session,
	}, nil
}

// 5. Permanent Tutor Change
func (u *scheduleUsecase) ChangeTutorPermanent(
	ctx context.Context,
	tenantID uuid.UUID,
	req *domain.PermanentTutorChangeRequest,
) (*domain.PermanentTutorChangeResponse, error) {
	var oldSchedule *domain.ClassSchedule
	var newSchedule *domain.ClassSchedule

	effectiveDate := normalizeDate(req.EffectiveDate)
	prevDay := effectiveDate.AddDate(0, 0, -1)

	err := u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		var err error
		oldSchedule, err = u.scheduleRepo.GetByIDForTenantForUpdate(txCtx, tenantID, req.ScheduleID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrScheduleNotFound
		}
		if err != nil {
			return err
		}
		if oldSchedule == nil {
			return ErrScheduleNotFound
		}

		if err := validateReplacementDate(oldSchedule, effectiveDate); err != nil {
			return err
		}
		originalValidUntil := oldSchedule.ValidUntil

		// 1. End validity of old schedule (valid_until = effective_date - 1 day)
		oldSchedule.ValidUntil = &prevDay
		if err := u.scheduleRepo.Update(txCtx, oldSchedule); err != nil {
			return err
		}

		// 2. Create newly duplicated schedule with new tutor_id and valid_from = effective_date
		newTutorID := req.NewTutorID
		newSchedule = &domain.ClassSchedule{
			ID:           uuid.New(),
			ClassID:      oldSchedule.ClassID,
			EnrollmentID: oldSchedule.EnrollmentID,
			TutorID:      &newTutorID,
			Capacity:     oldSchedule.Capacity,
			Location:     oldSchedule.Location,
			DayOfWeek:    oldSchedule.DayOfWeek,
			StartTime:    oldSchedule.StartTime,
			EndTime:      oldSchedule.EndTime,
			ValidFrom:    &effectiveDate,
			ValidUntil:   originalValidUntil,
		}
		if err := u.scheduleRepo.Create(txCtx, newSchedule); err != nil {
			return err
		}

		if err := u.enrollmentRepo.TransferSchedule(txCtx, tenantID, oldSchedule.ClassID, oldSchedule.ID, newSchedule.ID); err != nil {
			return err
		}

		// 3. Update future Class_Sessions to reflect new tutor_id and new schedule_id from effective_date onwards
		if err := u.sessionRepo.UpdateFutureSessionsTutor(txCtx, tenantID, oldSchedule.ID, newTutorID, newSchedule.ID, effectiveDate); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &domain.PermanentTutorChangeResponse{
		OldSchedule: oldSchedule,
		NewSchedule: newSchedule,
	}, nil
}

// 6. Attendance/Session Read Logic
func (u *scheduleUsecase) GetSessionAttendees(
	ctx context.Context,
	tenantID uuid.UUID,
	sessionID uuid.UUID,
) ([]*domain.Enrollment, error) {
	// Attendees are student records, so the session has to be resolved through the
	// tenant filter before any enrollment is read: a session owned by another tenant
	// is answered as missing and leaks no student data.
	session, err := u.sessionRepo.GetByIDForTenant(ctx, tenantID, sessionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}

	// IF the session has a non-null enrollment_id (Private Class): Return only the single student associated with that enrollment_id.
	if session.EnrollmentID != nil {
		enrollment, err := u.enrollmentRepo.GetByIDForAccess(ctx, &tenantID, nil, *session.EnrollmentID)
		if err != nil || enrollment == nil {
			return nil, ErrEnrollmentNotFound
		}
		return []*domain.Enrollment{enrollment}, nil
	}

	// Group sessions only include enrollments assigned to this schedule.
	if session.ScheduleID == nil {
		return nil, ErrScheduleNotFound
	}
	enrollments, err := u.enrollmentRepo.GetActiveByScheduleID(ctx, tenantID, *session.ScheduleID)
	if err != nil {
		return nil, err
	}

	return enrollments, nil
}
