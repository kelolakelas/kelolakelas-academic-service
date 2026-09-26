package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestScheduleHasEndedUsesApplicationCalendar(t *testing.T) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	otherLocation := time.FixedZone("other", -12*60*60)
	dateInOtherLocation := func(day time.Time) *time.Time {
		date := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, otherLocation)
		return &date
	}
	// DATE fields keep their calendar date; their zone is not converted as an instant.
	if !scheduleHasEnded(dateInOtherLocation(today.AddDate(0, 0, -1))) {
		t.Fatal("yesterday must be ended")
	}
	if scheduleHasEnded(dateInOtherLocation(today)) || scheduleHasEnded(nil) {
		t.Fatal("today and unbounded must remain valid")
	}
}

// Run with KEL52_TEST_DSN against a disposable PostgreSQL database.
func TestScheduleValidityPostgres(t *testing.T) {
	dsn := os.Getenv("KEL52_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL52_TEST_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock(52, 1)"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(52, 1)")
	schema, err := os.ReadFile("../../migrations/00000000000000_init_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatal(err)
	}

	today := time.Now()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	yesterday := today.AddDate(0, 0, -1)
	tenantID, categoryID, classID := uuid.New(), uuid.New(), uuid.New()
	if err := db.Exec("INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, ?)", categoryID, tenantID, "Validity test").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", classID, tenantID, categoryID, "Validity test", "group", 100, true, "open").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO tenant_location_snapshots (tenant_id, name, is_active) VALUES (?, ?, ?)", tenantID, "Validity tenant", true).Error; err != nil {
		t.Fatal(err)
	}

	scheduleIDs := map[string]uuid.UUID{"ended": uuid.New(), "today": uuid.New(), "unbounded": uuid.New(), "future_start": uuid.New()}
	validUntil := yesterday
	for name, id := range scheduleIDs {
		schedule := &domain.ClassSchedule{ID: id, ClassID: classID, Capacity: 4, DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}
		if name == "ended" {
			schedule.ValidUntil = &validUntil
		}
		if name == "today" {
			endsToday := today
			schedule.ValidUntil = &endsToday
		}
		if name == "future_start" {
			startsTomorrow := today.AddDate(0, 0, 1)
			schedule.ValidFrom = &startsTomorrow
		}
		if err := db.Create(schedule).Error; err != nil {
			t.Fatal(err)
		}
	}

	catalog := NewCatalogRepository(db)
	for name, load := range map[string]func() ([]byte, error){
		"list": func() ([]byte, error) {
			items, _, err := catalog.List(context.Background(), domain.CatalogQuery{ListQuery: domain.ListQuery{Page: 1, PageSize: 100}, TenantID: &tenantID, RadiusKM: 25, Sort: "newest"})
			if err != nil {
				return nil, err
			}
			if len(items) != 1 {
				return nil, fmt.Errorf("catalog items=%d, want 1", len(items))
			}
			if len(items[0].Schedules) == 0 {
				return nil, fmt.Errorf("catalog schedules are empty; item=%+v", items[0])
			}
			return items[0].Schedules, nil
		},
		"detail": func() ([]byte, error) {
			item, err := catalog.GetByID(context.Background(), classID)
			if err != nil {
				return nil, err
			}
			return item.Schedules, nil
		},
	} {
		t.Run("catalog "+name, func(t *testing.T) {
			encoded, err := load()
			if err != nil {
				t.Fatal(err)
			}
			var schedules []map[string]any
			if err := json.Unmarshal(encoded, &schedules); err != nil {
				t.Fatal(err)
			}
			found := map[string]bool{}
			for _, schedule := range schedules {
				found[schedule["id"].(string)] = true
			}
			if found[scheduleIDs["ended"].String()] || !found[scheduleIDs["today"].String()] || !found[scheduleIDs["unbounded"].String()] || !found[scheduleIDs["future_start"].String()] {
				t.Fatalf("returned schedules=%s; want today and unbounded, not ended", encoded)
			}
		})
	}

	repository := NewEnrollmentRepository(db)
	for name, scheduleID := range scheduleIDs {
		t.Run("enrollment "+name, func(t *testing.T) {
			studentID := uuid.New()
			if err := db.Exec("INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, ?)", studentID, uuid.New(), "Validity student").Error; err != nil {
				t.Fatal(err)
			}
			enrollment := &domain.Enrollment{ID: uuid.New(), TenantID: tenantID, StudentID: studentID, ClassID: classID, ScheduleID: &scheduleID, Status: "pending", BillingCycle: "monthly"}
			err := repository.CreateIfCapacityAvailable(context.Background(), enrollment)
			if name == "ended" {
				if err != domain.ErrScheduleEnded {
					t.Fatalf("error=%v, want ErrScheduleEnded", err)
				}
				var count int64
				if err := db.Model(&domain.Enrollment{}).Where("id = ?", enrollment.ID).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("enrollment count=%d err=%v, want no row", count, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateIfCapacityAvailable error: %v", err)
			}
		})
	}
}
