package usecase

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type failingPermanentSessionRepo struct {
	repository.SessionRepository
	failBatch bool
	failTutor bool
}

func (r failingPermanentSessionRepo) BatchCreate(ctx context.Context, sessions []*domain.ClassSession) error {
	if r.failBatch {
		return errors.New("session insert failed")
	}
	return r.SessionRepository.BatchCreate(ctx, sessions)
}

func (r failingPermanentSessionRepo) UpdateFutureSessionsTutor(ctx context.Context, tenant, schedule, tutor, replacement uuid.UUID, from time.Time) error {
	if r.failTutor {
		return errors.New("session update failed")
	}
	return r.SessionRepository.UpdateFutureSessionsTutor(ctx, tenant, schedule, tutor, replacement, from)
}

type failingPermanentEnrollmentRepo struct {
	repository.EnrollmentRepository
	fail bool
}

func (r failingPermanentEnrollmentRepo) TransferSchedule(ctx context.Context, tenant, class, old, replacement uuid.UUID) error {
	if r.fail {
		return errors.New("transfer failed")
	}
	return r.EnrollmentRepository.TransferSchedule(ctx, tenant, class, old, replacement)
}

func TestPermanentChangePostgres(t *testing.T) {
	dsn := os.Getenv("KEL21_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL21_TEST_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// Go runs packages concurrently; serialize shared-schema setup and fixtures.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock(51, 1)"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(51, 1)")
	schema, err := os.ReadFile("../../migrations/00000000000000_init_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	for _, tc := range []struct {
		name  string
		tutor bool
		fail  string
	}{
		{"schedule", false, ""}, {"tutor", true, ""},
		{"schedule transfer rollback", false, "transfer"},
		{"schedule session rollback", false, "batch"},
		{"tutor transfer rollback", true, "transfer"},
		{"tutor session rollback", true, "tutor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant, category, classID, student, oldID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			for _, sql := range []string{
				"INSERT INTO categories (id, tenant_id, name) VALUES ('" + category.String() + "', '" + tenant.String() + "', 'Math')",
				"INSERT INTO classes (id, tenant_id, category_id, name, type, price) VALUES ('" + classID.String() + "', '" + tenant.String() + "', '" + category.String() + "', 'Group', 'group', 100)",
				"INSERT INTO students (id, parent_id, first_name) VALUES ('" + student.String() + "', '" + uuid.NewString() + "', 'Student')",
			} {
				if err := db.Exec(sql).Error; err != nil {
					t.Fatal(err)
				}
			}
			oldTutor, newTutor := uuid.New(), uuid.New()
			until := day(30)
			old := &domain.ClassSchedule{ID: oldID, ClassID: classID, TutorID: &oldTutor, Capacity: 2, DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00", ValidUntil: &until}
			schedules := repository.NewScheduleRepository(db)
			sessions := repository.NewSessionRepository(db)
			enrollments := repository.NewEnrollmentRepository(db)
			if err := schedules.Create(ctx, old); err != nil {
				t.Fatal(err)
			}
			live := &domain.Enrollment{ID: uuid.New(), TenantID: tenant, StudentID: student, ClassID: classID, ScheduleID: &oldID, Status: "active", JoinedAt: day(1)}
			if err := enrollments.Create(ctx, live); err != nil {
				t.Fatal(err)
			}
			otherStudent := uuid.New()
			if err := db.Exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", otherStudent, uuid.New(), "Former").Error; err != nil {
				t.Fatal(err)
			}
			cancelled := &domain.Enrollment{ID: uuid.New(), TenantID: tenant, StudentID: otherStudent, ClassID: classID, ScheduleID: &oldID, Status: "cancelled", JoinedAt: day(1)}
			if err := enrollments.Create(ctx, cancelled); err != nil {
				t.Fatal(err)
			}
			future := &domain.ClassSession{ID: uuid.New(), ClassID: classID, ScheduleID: &oldID, TutorID: oldTutor, SessionDate: day(21), StartTime: "10:00:00", EndTime: "11:00:00", Status: "scheduled"}
			if err := sessions.Create(ctx, future); err != nil {
				t.Fatal(err)
			}
			u := NewScheduleUsecase(repository.NewTransactionManager(db), nil, schedules,
				failingPermanentSessionRepo{SessionRepository: sessions, failBatch: tc.fail == "batch", failTutor: tc.fail == "tutor"},
				failingPermanentEnrollmentRepo{EnrollmentRepository: enrollments, fail: tc.fail == "transfer"})
			if tc.tutor {
				res, err := u.ChangeTutorPermanent(ctx, tenant, &domain.PermanentTutorChangeRequest{ScheduleID: oldID, NewTutorID: newTutor, EffectiveDate: day(14)})
				if tc.fail == "" {
					if err != nil {
						t.Fatal(err)
					}
					if res.NewSchedule.Capacity != 2 || res.NewSchedule.ValidUntil == nil || !res.NewSchedule.ValidUntil.Equal(until) {
						t.Fatalf("replacement=%+v", res.NewSchedule)
					}
					updated, err := sessions.GetByID(ctx, future.ID)
					if err != nil || updated.TutorID != newTutor || updated.ScheduleID == nil || *updated.ScheduleID != res.NewSchedule.ID {
						t.Fatalf("future session=%+v err=%v", updated, err)
					}
				} else if err == nil {
					t.Fatal("expected rollback error")
				}
			} else {
				res, err := u.ChangeSchedulePermanent(ctx, tenant, &domain.PermanentScheduleChangeRequest{OldScheduleID: oldID, NewDayOfWeek: 3, NewStartTime: "12:00:00", NewEndTime: "13:00:00", EffectiveDate: day(14)})
				if tc.fail == "" {
					if err != nil {
						t.Fatal(err)
					}
					if res.NewSchedule.Capacity != 2 || res.NewSchedule.ValidUntil == nil || !res.NewSchedule.ValidUntil.Equal(until) || len(res.NewSessions) != 3 {
						t.Fatalf("replacement=%+v sessions=%d", res.NewSchedule, len(res.NewSessions))
					}
					for _, session := range res.NewSessions {
						if session.ScheduleID == nil || *session.ScheduleID != res.NewSchedule.ID || session.SessionDate.Before(day(14)) || session.SessionDate.After(day(30)) {
							t.Fatalf("session=%+v", session)
						}
					}
					attendees, err := u.GetSessionAttendees(ctx, tenant, res.NewSessions[0].ID)
					if err != nil || len(attendees) != 1 || attendees[0].ID != live.ID {
						t.Fatalf("attendees=%v err=%v", attendees, err)
					}
				} else if err == nil {
					t.Fatal("expected rollback error")
				}
			}
			former, err := enrollments.GetByID(ctx, cancelled.ID)
			if err != nil || former.ScheduleID == nil || *former.ScheduleID != oldID {
				t.Fatalf("cancelled enrollment changed: %+v err=%v", former, err)
			}
			stored, err := enrollments.GetByID(ctx, live.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.fail == "" {
				if stored.ScheduleID == nil || *stored.ScheduleID == oldID {
					t.Fatalf("enrollment not transferred: %+v", stored)
				}
			} else {
				var replacements int64
				if err := db.Model(&domain.ClassSchedule{}).Where("class_id = ?", classID).Count(&replacements).Error; err != nil || replacements != 1 {
					t.Fatalf("schedule count=%d err=%v", replacements, err)
				}
				original, err := schedules.GetByID(ctx, oldID)
				if err != nil || original.ValidUntil == nil || !original.ValidUntil.Equal(until) || stored.ScheduleID == nil || *stored.ScheduleID != oldID {
					t.Fatalf("rollback schedule=%+v enrollment=%+v err=%v", original, stored, err)
				}
				unchanged, err := sessions.GetByID(ctx, future.ID)
				if err != nil || unchanged.Status != "scheduled" || unchanged.TutorID != oldTutor || unchanged.ScheduleID == nil || *unchanged.ScheduleID != oldID {
					t.Fatalf("rollback session=%+v err=%v", unchanged, err)
				}
			}
		})
	}
}
