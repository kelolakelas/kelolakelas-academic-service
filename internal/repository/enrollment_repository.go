package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type enrollmentRepository struct {
	db *gorm.DB
}

func NewEnrollmentRepository(db *gorm.DB) EnrollmentRepository {
	return &enrollmentRepository{db: db}
}

func (r *enrollmentRepository) getDB(ctx context.Context) *gorm.DB {
	return GetDB(ctx, r.db)
}

func (r *enrollmentRepository) Create(ctx context.Context, enrollment *domain.Enrollment) error {
	return r.getDB(ctx).Create(enrollment).Error
}

func (r *enrollmentRepository) GetByIdempotencyKey(ctx context.Context, parentID uuid.UUID, key string) (*domain.Enrollment, error) {
	var enrollment domain.Enrollment
	err := r.getDB(ctx).Joins("JOIN students s ON s.id = enrollments.student_id").Where("enrollments.idempotency_key = ? AND s.parent_id = ?", key, parentID).First(&enrollment).Error
	return &enrollment, err
}

func scheduleHasEnded(validUntil *time.Time) bool {
	if validUntil == nil {
		return false
	}
	today := time.Now()
	endDate := time.Date(validUntil.Year(), validUntil.Month(), validUntil.Day(), 0, 0, 0, 0, today.Location())
	startOfToday := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	return endDate.Before(startOfToday)
}

func (r *enrollmentRepository) CreateIfCapacityAvailable(ctx context.Context, enrollment *domain.Enrollment) error {
	db := r.getDB(ctx)
	var class domain.Class
	if err := db.First(&class, "id = ? AND deleted_at IS NULL", enrollment.ClassID).Error; err != nil {
		return err
	}
	if !class.IsPublished || class.EnrollmentStatus != "open" {
		return domain.ErrClassNotEnrollable
	}
	if enrollment.ScheduleID != nil {
		var schedule domain.ClassSchedule
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND class_id = ? AND deleted_at IS NULL", *enrollment.ScheduleID, enrollment.ClassID).First(&schedule).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrScheduleClassMismatch
			}
			return err
		}
		if scheduleHasEnded(schedule.ValidUntil) {
			return domain.ErrScheduleEnded
		}
		// Checked after the schedule lock and before capacity: a request that waited
		// on the lock behind the same student's signup then sees that committed row,
		// and the student's own seat must not be misreported as a full schedule.
		if err := rejectLiveEnrollment(db, enrollment); err != nil {
			return err
		}
		var count int64
		if err := db.Model(&domain.Enrollment{}).Where("schedule_id = ? AND status IN ? AND deleted_at IS NULL", *enrollment.ScheduleID, []string{"pending", "active"}).Count(&count).Error; err != nil {
			return err
		}
		if count >= int64(schedule.Capacity) {
			return domain.ErrScheduleFull
		}
	} else if class.Type == "group" {
		return domain.ErrScheduleRequired
	} else if err := rejectLiveEnrollment(db, enrollment); err != nil {
		return err
	}
	return mapDuplicateEnrollment(db.Create(enrollment).Error)
}

// studentClassActiveIndex is the partial unique index on (student_id, class_id)
// for pending/active, non-deleted enrollments (migrations/00000000000000_init_schema.up.sql).
const studentClassActiveIndex = "idx_student_class_active"

// rejectLiveEnrollment returns domain.ErrDuplicateEnrollment when the student already
// holds a pending or active enrollment in the class, using the same predicate as
// idx_student_class_active.
func rejectLiveEnrollment(db *gorm.DB, enrollment *domain.Enrollment) error {
	var count int64
	if err := db.Model(&domain.Enrollment{}).
		Where("student_id = ? AND class_id = ? AND status IN ? AND deleted_at IS NULL", enrollment.StudentID, enrollment.ClassID, []string{"pending", "active"}).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return domain.ErrDuplicateEnrollment
	}
	return nil
}

// mapDuplicateEnrollment turns a unique violation of idx_student_class_active into
// domain.ErrDuplicateEnrollment. The pre-check is not a lock, so two concurrent
// requests for the same student and class (on different schedules, or a private
// class with no schedule row to lock) can both pass it; the index decides that race.
// The mapping is local on purpose: GORM's global TranslateError would also rewrite
// errors other callers inspect. A unique violation on any other index (e.g. the
// idempotency key) is returned unchanged so the use case can resolve it as a
// same-key retry.
func mapDuplicateEnrollment(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == studentClassActiveIndex {
		return domain.ErrDuplicateEnrollment
	}
	return err
}

// uniqueViolationCode is PostgreSQL SQLSTATE unique_violation.
const uniqueViolationCode = "23505"

func (r *enrollmentRepository) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Enrollment, error) {
	var enrollment domain.Enrollment
	if err := r.getDB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&enrollment, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &enrollment, nil
}

func (r *enrollmentRepository) GetByIdempotencyKeyForTenant(ctx context.Context, tenantID uuid.UUID, key string) (*domain.Enrollment, error) {
	var enrollment domain.Enrollment
	err := r.getDB(ctx).Where("tenant_id = ? AND idempotency_key = ?", tenantID, key).First(&enrollment).Error
	return &enrollment, err
}

func (r *enrollmentRepository) GetByIdempotencyKeyAny(ctx context.Context, key string) (*domain.Enrollment, error) {
	var enrollment domain.Enrollment
	err := r.getDB(ctx).Where("idempotency_key = ?", key).First(&enrollment).Error
	return &enrollment, err
}

func (r *enrollmentRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Enrollment, error) {
	var enrollment domain.Enrollment
	if err := r.getDB(ctx).Preload("Student").Preload("Class").First(&enrollment, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &enrollment, nil
}

func (r *enrollmentRepository) GetByIDForAccess(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.Enrollment, error) {
	db := r.getDB(ctx).Preload("Student").Preload("Class").Where("enrollments.id = ?", id)
	if tenantID != nil {
		db = db.Where("enrollments.tenant_id = ?", *tenantID)
	}
	if parentID != nil {
		db = db.Joins("JOIN students s ON s.id = enrollments.student_id").Where("s.parent_id = ?", *parentID)
	}
	var enrollment domain.Enrollment
	err := db.First(&enrollment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gorm.ErrRecordNotFound
	}
	return &enrollment, err
}

func (r *enrollmentRepository) List(ctx context.Context, tenantID, parentID *uuid.UUID, query domain.EnrollmentQuery) ([]*domain.Enrollment, int64, error) {
	db := r.getDB(ctx).Model(&domain.Enrollment{}).Preload("Student").Preload("Class")
	if tenantID != nil {
		db = db.Where("enrollments.tenant_id = ?", *tenantID)
	}
	if parentID != nil {
		db = db.Joins("JOIN students s ON s.id = enrollments.student_id").Where("s.parent_id = ?", *parentID)
	}
	if query.Status != "" {
		db = db.Where("enrollments.status = ?", query.Status)
	}
	if query.ClassID != nil {
		db = db.Where("enrollments.class_id = ?", *query.ClassID)
	}
	if query.StudentID != nil {
		db = db.Where("enrollments.student_id = ?", *query.StudentID)
	}
	if query.DateFrom != nil {
		db = db.Where("enrollments.joined_at >= ?", *query.DateFrom)
	}
	if query.DateTo != nil {
		db = db.Where("enrollments.joined_at <= ?", *query.DateTo)
	}
	if query.Search != "" {
		db = db.Joins("JOIN students ss ON ss.id = enrollments.student_id").
			Joins("JOIN classes cc ON cc.id = enrollments.class_id").
			Where("COALESCE(ss.first_name, '') ILIKE ? OR COALESCE(ss.last_name, '') ILIKE ? OR COALESCE(ss.nickname, '') ILIKE ? OR cc.name ILIKE ?", "%"+query.Search+"%", "%"+query.Search+"%", "%"+query.Search+"%", "%"+query.Search+"%")
	}
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []*domain.Enrollment
	err := db.Order("enrollments.joined_at DESC").
		Limit(query.PageSize).
		Offset((query.Page - 1) * query.PageSize).
		Find(&items).Error
	return items, total, err
}

func (r *enrollmentRepository) ExistsActive(ctx context.Context, studentID, classID uuid.UUID) (bool, error) {
	var count int64
	err := r.getDB(ctx).Model(&domain.Enrollment{}).
		Where("student_id = ? AND class_id = ? AND status = ?", studentID, classID, "active").
		Count(&count).Error
	return count > 0, err
}

func (r *enrollmentRepository) IsTutorForEnrollment(ctx context.Context, enrollmentID, memberID uuid.UUID) (bool, error) {
	var count int64
	err := r.getDB(ctx).Table("enrollments e").Joins("JOIN class_teachers ct ON ct.class_id = e.class_id").Where("e.id = ? AND ct.teacher_id = ?", enrollmentID, memberID).Count(&count).Error
	return count > 0, err
}

func (r *enrollmentRepository) GetActiveByClassID(ctx context.Context, classID uuid.UUID) ([]*domain.Enrollment, error) {
	var enrollments []*domain.Enrollment
	if err := r.getDB(ctx).Preload("Student").Where("class_id = ? AND status = ?", classID, "active").Find(&enrollments).Error; err != nil {
		return nil, err
	}
	return enrollments, nil
}

func (r *enrollmentRepository) GetActiveByScheduleID(ctx context.Context, tenantID, scheduleID uuid.UUID) ([]*domain.Enrollment, error) {
	var enrollments []*domain.Enrollment
	err := r.getDB(ctx).Preload("Student").Where("schedule_id = ? AND tenant_id = ? AND status IN ? AND deleted_at IS NULL", scheduleID, tenantID, []string{"pending", "active"}).Find(&enrollments).Error
	return enrollments, err
}

// TransferSchedule moves only live enrollments; the caller holds the old schedule
// row lock in the same transaction, serializing this with capacity-checked signups.
func (r *enrollmentRepository) TransferSchedule(ctx context.Context, tenantID, classID, oldScheduleID, newScheduleID uuid.UUID) error {
	return r.getDB(ctx).Model(&domain.Enrollment{}).
		Where("tenant_id = ? AND class_id = ? AND schedule_id = ? AND status IN ? AND deleted_at IS NULL", tenantID, classID, oldScheduleID, []string{"pending", "active"}).
		Update("schedule_id", newScheduleID).Error
}

func (r *enrollmentRepository) AssignSchedule(ctx context.Context, enrollmentID, scheduleID uuid.UUID) error {
	db := r.getDB(ctx)
	var enrollment domain.Enrollment
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&enrollment, "id = ? AND deleted_at IS NULL", enrollmentID).Error; err != nil {
		return err
	}
	if enrollment.Status != "pending" || enrollment.ScheduleID != nil {
		return domain.ErrInvalidEnrollmentTransition
	}
	var schedule domain.ClassSchedule
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&schedule, "id = ? AND deleted_at IS NULL", scheduleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrScheduleNotFound
		}
		return err
	}
	if schedule.ClassID != enrollment.ClassID {
		return domain.ErrScheduleClassMismatch
	}
	if scheduleHasEnded(schedule.ValidUntil) {
		return domain.ErrScheduleEnded
	}
	var count int64
	if err := db.Model(&domain.Enrollment{}).Where("schedule_id = ? AND status IN ? AND deleted_at IS NULL", scheduleID, []string{"pending", "active"}).Count(&count).Error; err != nil {
		return err
	}
	if count >= int64(schedule.Capacity) {
		return domain.ErrScheduleFull
	}
	enrollment.ScheduleID = &scheduleID
	return db.Save(&enrollment).Error
}

func (r *enrollmentRepository) Update(ctx context.Context, enrollment *domain.Enrollment) error {
	return r.getDB(ctx).Save(enrollment).Error
}

func (r *enrollmentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.getDB(ctx).Delete(&domain.Enrollment{}, "id = ?", id).Error
}
