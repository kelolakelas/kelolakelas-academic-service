package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

func TestMapDuplicateEnrollmentOnlyMapsStudentClassIndex(t *testing.T) {
	if err := mapDuplicateEnrollment(&pgconn.PgError{Code: "23505", ConstraintName: "idx_student_class_active"}); !errors.Is(err, domain.ErrDuplicateEnrollment) {
		t.Fatalf("student/class unique violation = %v, want ErrDuplicateEnrollment", err)
	}
	idempotency := &pgconn.PgError{Code: "23505", ConstraintName: "idx_enrollments_idempotency_key"}
	if err := mapDuplicateEnrollment(idempotency); err != idempotency {
		t.Fatalf("idempotency unique violation = %v, want it unchanged", err)
	}
	foreignKey := &pgconn.PgError{Code: "23503", ConstraintName: "idx_student_class_active"}
	if err := mapDuplicateEnrollment(foreignKey); err != foreignKey {
		t.Fatalf("non-unique error = %v, want it unchanged", err)
	}
	if err := mapDuplicateEnrollment(nil); err != nil {
		t.Fatalf("nil = %v, want nil", err)
	}
}

type duplicateEnrollmentFixture struct {
	db        *gorm.DB
	repo      EnrollmentRepository
	tenantID  uuid.UUID
	privateID uuid.UUID
	groupID   uuid.UUID
	schedules [2]uuid.UUID
}

func (f *duplicateEnrollmentFixture) student(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := f.db.Exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", id, uuid.New(), "KEL-54 student").Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *duplicateEnrollmentFixture) enrollment(studentID, classID uuid.UUID, scheduleID *uuid.UUID) *domain.Enrollment {
	key := uuid.NewString()
	return &domain.Enrollment{ID: uuid.New(), TenantID: f.tenantID, StudentID: studentID, ClassID: classID, ScheduleID: scheduleID, Status: "pending", BillingCycle: "monthly", IdempotencyKey: &key, PaymentStatus: "pending"}
}

func (f *duplicateEnrollmentFixture) liveCount(t *testing.T, studentID, classID uuid.UUID) int64 {
	t.Helper()
	var count int64
	if err := f.db.Model(&domain.Enrollment{}).Where("student_id = ? AND class_id = ? AND status IN ? AND deleted_at IS NULL", studentID, classID, []string{"pending", "active"}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func newDuplicateEnrollmentFixture(t *testing.T) *duplicateEnrollmentFixture {
	t.Helper()
	dsn := os.Getenv("KEL54_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL54_TEST_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// Go runs packages concurrently; serialize shared-schema setup and fixtures.
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock(54, 1)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(54, 1)") })
	schema, err := os.ReadFile("../../migrations/00000000000000_init_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatal(err)
	}
	f := &duplicateEnrollmentFixture{db: db, repo: NewEnrollmentRepository(db), tenantID: uuid.New(), privateID: uuid.New(), groupID: uuid.New(), schedules: [2]uuid.UUID{uuid.New(), uuid.New()}}
	categoryID := uuid.New()
	if err := db.Exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryID, f.tenantID, "KEL-54").Error; err != nil {
		t.Fatal(err)
	}
	for id, classType := range map[uuid.UUID]string{f.privateID: "private", f.groupID: "group"} {
		if err := db.Exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", id, f.tenantID, categoryID, "KEL-54 "+classType, classType, 100, true, "open").Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range f.schedules {
		if err := db.Create(&domain.ClassSchedule{ID: id, ClassID: f.groupID, Capacity: 5, DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// Run with KEL54_TEST_DSN against a disposable PostgreSQL database.
func TestDuplicateEnrollmentPostgres(t *testing.T) {
	f := newDuplicateEnrollmentFixture(t)
	ctx := context.Background()

	t.Run("sequential duplicate is rejected for private class without schedule", func(t *testing.T) {
		studentID := f.student(t)
		if err := f.repo.CreateIfCapacityAvailable(ctx, f.enrollment(studentID, f.privateID, nil)); err != nil {
			t.Fatalf("first enrollment: %v", err)
		}
		if err := f.repo.CreateIfCapacityAvailable(ctx, f.enrollment(studentID, f.privateID, nil)); !errors.Is(err, domain.ErrDuplicateEnrollment) {
			t.Fatalf("second enrollment = %v, want ErrDuplicateEnrollment", err)
		}
		if got := f.liveCount(t, studentID, f.privateID); got != 1 {
			t.Fatalf("live enrollments = %d, want 1", got)
		}
	})

	t.Run("duplicate on another schedule of the same class is rejected", func(t *testing.T) {
		studentID := f.student(t)
		if err := f.repo.CreateIfCapacityAvailable(ctx, f.enrollment(studentID, f.groupID, &f.schedules[0])); err != nil {
			t.Fatalf("first enrollment: %v", err)
		}
		if err := f.repo.CreateIfCapacityAvailable(ctx, f.enrollment(studentID, f.groupID, &f.schedules[1])); !errors.Is(err, domain.ErrDuplicateEnrollment) {
			t.Fatalf("second enrollment = %v, want ErrDuplicateEnrollment", err)
		}
	})

	t.Run("own seat in a full schedule is reported as duplicate, not full", func(t *testing.T) {
		fullID := uuid.New()
		if err := f.db.Create(&domain.ClassSchedule{ID: fullID, ClassID: f.groupID, Capacity: 1, DayOfWeek: 2, StartTime: "10:00:00", EndTime: "11:00:00"}).Error; err != nil {
			t.Fatal(err)
		}
		studentID := f.student(t)
		if err := f.repo.CreateIfCapacityAvailable(ctx, f.enrollment(studentID, f.groupID, &fullID)); err != nil {
			t.Fatalf("first enrollment: %v", err)
		}
		if err := f.repo.CreateIfCapacityAvailable(ctx, f.enrollment(studentID, f.groupID, &fullID)); !errors.Is(err, domain.ErrDuplicateEnrollment) {
			t.Fatalf("same student = %v, want ErrDuplicateEnrollment", err)
		}
		if err := f.repo.CreateIfCapacityAvailable(ctx, f.enrollment(f.student(t), f.groupID, &fullID)); !errors.Is(err, domain.ErrScheduleFull) {
			t.Fatalf("other student = %v, want ErrScheduleFull", err)
		}
	})

	t.Run("unique index violation maps to the sentinel", func(t *testing.T) {
		studentID := f.student(t)
		if err := f.repo.CreateIfCapacityAvailable(ctx, f.enrollment(studentID, f.privateID, nil)); err != nil {
			t.Fatalf("first enrollment: %v", err)
		}
		// A raw insert bypasses the pre-check, so only the index can refuse it.
		raw := f.db.Create(f.enrollment(studentID, f.privateID, nil)).Error
		var pgErr *pgconn.PgError
		if !errors.As(raw, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != studentClassActiveIndex {
			t.Fatalf("raw insert error = %v, want unique violation on %s", raw, studentClassActiveIndex)
		}
		if err := mapDuplicateEnrollment(raw); !errors.Is(err, domain.ErrDuplicateEnrollment) {
			t.Fatalf("mapped = %v, want ErrDuplicateEnrollment", err)
		}
	})

	t.Run("concurrent race decided by the unique index", func(t *testing.T) {
		for name, scheduleIDs := range map[string][2]*uuid.UUID{
			"private class without schedule": {nil, nil},
			"group class on two schedules":   {&f.schedules[0], &f.schedules[1]},
		} {
			t.Run(name, func(t *testing.T) {
				classID := f.privateID
				if scheduleIDs[0] != nil {
					classID = f.groupID
				}
				studentID := f.student(t)
				// The first transaction inserts and holds its row uncommitted, so the
				// second one passes the pre-check and must be stopped by the index.
				first := f.db.Begin()
				if err := f.repo.CreateIfCapacityAvailable(context.WithValue(ctx, txKey{}, first), f.enrollment(studentID, classID, scheduleIDs[0])); err != nil {
					first.Rollback()
					t.Fatalf("first enrollment: %v", err)
				}
				secondErr := make(chan error, 1)
				go func() {
					second := f.db.Begin()
					err := f.repo.CreateIfCapacityAvailable(context.WithValue(ctx, txKey{}, second), f.enrollment(studentID, classID, scheduleIDs[1]))
					if err != nil {
						second.Rollback()
					} else {
						err = second.Commit().Error
					}
					secondErr <- err
				}()
				select {
				case err := <-secondErr:
					first.Rollback()
					t.Fatalf("second enrollment finished before the first committed: %v", err)
				case <-time.After(300 * time.Millisecond):
				}
				if err := first.Commit().Error; err != nil {
					t.Fatalf("commit first: %v", err)
				}
				if err := <-secondErr; !errors.Is(err, domain.ErrDuplicateEnrollment) {
					t.Fatalf("second enrollment = %v, want ErrDuplicateEnrollment", err)
				}
				if got := f.liveCount(t, studentID, classID); got != 1 {
					t.Fatalf("live enrollments = %d, want 1", got)
				}
			})
		}
	})

	t.Run("dropped, completed and soft-deleted enrollments allow re-enrollment", func(t *testing.T) {
		for _, finish := range []string{"dropped", "completed", "deleted"} {
			t.Run(finish, func(t *testing.T) {
				studentID := f.student(t)
				previous := f.enrollment(studentID, f.privateID, nil)
				if err := f.repo.CreateIfCapacityAvailable(ctx, previous); err != nil {
					t.Fatalf("first enrollment: %v", err)
				}
				var err error
				if finish == "deleted" {
					err = f.db.Delete(&domain.Enrollment{}, "id = ?", previous.ID).Error
				} else {
					err = f.db.Model(&domain.Enrollment{}).Where("id = ?", previous.ID).Update("status", finish).Error
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := f.repo.CreateIfCapacityAvailable(ctx, f.enrollment(studentID, f.privateID, nil)); err != nil {
					t.Fatalf("re-enrollment after %s: %v", finish, err)
				}
			})
		}
	})
}
