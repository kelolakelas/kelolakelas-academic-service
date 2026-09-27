package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// KEL-70: the parent enrollment list and detail carry a read-only summary of the
// enrollment's weekly schedule. The repository decides which rows (and therefore
// which schedules) a caller may see; these tests pin the mapping on top of it:
// the summary only ever describes the enrollment's own schedule, and every case
// with nothing trustworthy to show leaves the field out.

// scheduleSummaryRepo answers List and GetByIDForAccess from per-parent rows, the
// same visibility the SQL scope produces, so a leak would have to come from the
// mapping itself.
type scheduleSummaryRepo struct {
	marketplaceEnrollmentRepo
	byParent map[uuid.UUID][]*domain.Enrollment
	listErr  error
}

func (r *scheduleSummaryRepo) List(_ context.Context, _ *uuid.UUID, parentID *uuid.UUID, _ domain.EnrollmentQuery) ([]*domain.Enrollment, int64, error) {
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	if parentID == nil {
		return nil, 0, nil
	}
	items := r.byParent[*parentID]
	return items, int64(len(items)), nil
}

func (r *scheduleSummaryRepo) GetByIDForAccess(_ context.Context, _ *uuid.UUID, parentID *uuid.UUID, id uuid.UUID) (*domain.Enrollment, error) {
	if parentID != nil {
		for _, item := range r.byParent[*parentID] {
			if item.ID == id {
				return item, nil
			}
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func summaryString(value string) *string { return &value }

func scheduledEnrollment(classID uuid.UUID, schedule *domain.ClassSchedule) *domain.Enrollment {
	enrollment := &domain.Enrollment{ID: uuid.New(), TenantID: uuid.New(), StudentID: uuid.New(), ClassID: classID, Status: "active", BillingCycle: "monthly", JoinedAt: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)}
	if schedule != nil {
		id := schedule.ID
		enrollment.ScheduleID = &id
		enrollment.Schedule = schedule
	}
	return enrollment
}

func TestEnrollmentListCarriesScheduleSummaryForGroupEnrollment(t *testing.T) {
	parentA, parentB := uuid.New(), uuid.New()
	classID := uuid.New()
	scheduleA := &domain.ClassSchedule{ID: uuid.New(), ClassID: classID, Capacity: 10, DayOfWeek: 1, StartTime: "16:00:00", EndTime: "17:30:00", Location: summaryString("  Ruang A  "), TutorID: ptrToUUID(uuid.New())}
	scheduleB := &domain.ClassSchedule{ID: uuid.New(), ClassID: classID, Capacity: 10, DayOfWeek: 3, StartTime: "09:00:00", EndTime: "10:00:00", Location: summaryString("Ruang B")}
	ownA := scheduledEnrollment(classID, scheduleA)
	ownB := scheduledEnrollment(classID, scheduleB)
	repo := &scheduleSummaryRepo{byParent: map[uuid.UUID][]*domain.Enrollment{parentA: {ownA}, parentB: {ownB}}}
	u := NewEnrollmentUsecase(repo, nil, nil, nil)

	result, err := u.List(context.Background(), nil, &parentA, domain.EnrollmentQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(result.Items))
	}
	item := result.Items[0]
	if item.Schedule == nil {
		t.Fatal("schedule summary missing for a group enrollment with a live schedule")
	}
	want := domain.EnrollmentScheduleSummary{DayOfWeek: 1, StartTime: "16:00:00", EndTime: "17:30:00", Location: summaryString("Ruang A")}
	if item.Schedule.DayOfWeek != want.DayOfWeek || item.Schedule.StartTime != want.StartTime || item.Schedule.EndTime != want.EndTime || item.Schedule.Location == nil || *item.Schedule.Location != *want.Location {
		t.Fatalf("schedule = %+v, want %+v (location %q)", item.Schedule, want, *want.Location)
	}
	// Existing fields are unchanged by the additive summary.
	if item.ScheduleID == nil || *item.ScheduleID != scheduleA.ID || item.ID != ownA.ID || item.Status != "active" || item.BillingCycle != "monthly" {
		t.Fatalf("existing fields changed: %+v", item)
	}

	// Parent A never sees parent B's schedule, in the list or through detail.
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "Ruang B") || strings.Contains(string(encoded), `"day_of_week":3`) || strings.Contains(string(encoded), scheduleB.ID.String()) {
		t.Fatalf("parent A response leaks parent B schedule: %s", encoded)
	}
	if _, err := u.GetByID(context.Background(), nil, &parentA, ownB.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("parent A detail of parent B enrollment err = %v, want ErrRecordNotFound", err)
	}
}

func TestEnrollmentDetailCarriesScheduleSummary(t *testing.T) {
	parentID, classID := uuid.New(), uuid.New()
	schedule := &domain.ClassSchedule{ID: uuid.New(), ClassID: classID, Capacity: 4, DayOfWeek: 7, StartTime: "08:00:00", EndTime: "09:00:00"}
	own := scheduledEnrollment(classID, schedule)
	u := NewEnrollmentUsecase(&scheduleSummaryRepo{byParent: map[uuid.UUID][]*domain.Enrollment{parentID: {own}}}, nil, nil, nil)

	item, err := u.GetByID(context.Background(), nil, &parentID, own.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if item.Schedule == nil || item.Schedule.DayOfWeek != 7 || item.Schedule.StartTime != "08:00:00" || item.Schedule.EndTime != "09:00:00" {
		t.Fatalf("schedule = %+v, want Minggu 08:00-09:00", item.Schedule)
	}
	// An absent location stays absent instead of rendering as an empty string.
	if item.Schedule.Location != nil {
		t.Fatalf("location = %q, want nil", *item.Schedule.Location)
	}
	encoded, _ := json.Marshal(item.Schedule)
	if strings.Contains(string(encoded), "location") {
		t.Fatalf("summary JSON = %s, want location omitted", encoded)
	}
}

func TestEnrollmentScheduleSummaryOmittedWhenNothingTrustworthyToShow(t *testing.T) {
	classID := uuid.New()
	live := func() *domain.ClassSchedule {
		return &domain.ClassSchedule{ID: uuid.New(), ClassID: classID, Capacity: 3, DayOfWeek: 2, StartTime: "10:00:00", EndTime: "11:00:00"}
	}
	blankLocation := live()
	blankLocation.Location = summaryString("   ")

	tests := map[string]*domain.Enrollment{
		// Private class: no schedule_id at all.
		"private enrollment without schedule": scheduledEnrollment(classID, nil),
		// Soft-deleted schedule: GORM's soft-delete scope does not return it, so the
		// preload leaves Schedule nil while schedule_id is still set.
		"schedule not loaded (soft-deleted)": func() *domain.Enrollment {
			e := scheduledEnrollment(classID, live())
			e.Schedule = nil
			return e
		}(),
		"schedule marked deleted": func() *domain.Enrollment {
			s := live()
			s.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
			return scheduledEnrollment(classID, s)
		}(),
		"schedule id mismatch": func() *domain.Enrollment {
			e := scheduledEnrollment(classID, live())
			other := uuid.New()
			e.ScheduleID = &other
			return e
		}(),
		"schedule of another class": func() *domain.Enrollment {
			s := live()
			s.ClassID = uuid.New()
			return scheduledEnrollment(classID, s)
		}(),
	}
	for name, enrollment := range tests {
		t.Run(name, func(t *testing.T) {
			response := enrollmentResponse(enrollment)
			if response.Schedule != nil {
				t.Fatalf("schedule = %+v, want nil", response.Schedule)
			}
			encoded, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), `"schedule":`) {
				t.Fatalf("response JSON = %s, want schedule omitted", encoded)
			}
			// The pre-existing schedule_id field keeps its old behaviour.
			if (enrollment.ScheduleID != nil) != strings.Contains(string(encoded), `"schedule_id":`) {
				t.Fatalf("schedule_id presence changed: %s", encoded)
			}
		})
	}

	t.Run("blank location is dropped but the slot is kept", func(t *testing.T) {
		response := enrollmentResponse(scheduledEnrollment(classID, blankLocation))
		if response.Schedule == nil || response.Schedule.Location != nil {
			t.Fatalf("schedule = %+v, want slot without location", response.Schedule)
		}
	})
}

// The raw Enrollment is serialized by other endpoints (session attendees), so the
// preloaded schedule must not add a field to that contract.
func TestRawEnrollmentJSONDoesNotExposePreloadedSchedule(t *testing.T) {
	classID := uuid.New()
	schedule := &domain.ClassSchedule{ID: uuid.New(), ClassID: classID, Capacity: 9, DayOfWeek: 5, StartTime: "13:00:00", EndTime: "14:00:00", Location: summaryString("Aula")}
	encoded, err := json.Marshal(scheduledEnrollment(classID, schedule))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"Schedule", `"schedule":`, "Aula", `"capacity"`} {
		if strings.Contains(string(encoded), leak) {
			t.Fatalf("raw enrollment JSON contains %q: %s", leak, encoded)
		}
	}
}

// The summary exposes only the fields a parent needs; capacity, tutor and the
// validity window stay internal.
func TestEnrollmentScheduleSummaryJSONShape(t *testing.T) {
	classID := uuid.New()
	schedule := &domain.ClassSchedule{ID: uuid.New(), ClassID: classID, Capacity: 9, TutorID: ptrToUUID(uuid.New()), DayOfWeek: 4, StartTime: "15:00:00", EndTime: "16:00:00", Location: summaryString("Lab")}
	encoded, err := json.Marshal(enrollmentResponse(scheduledEnrollment(classID, schedule)).Schedule)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"day_of_week":4,"start_time":"15:00:00","end_time":"16:00:00","location":"Lab"}`; got != want {
		t.Fatalf("summary JSON = %s, want %s", got, want)
	}
}

func TestEnrollmentListPropagatesRepositoryError(t *testing.T) {
	parentID := uuid.New()
	dbErr := errors.New("database unavailable")
	u := NewEnrollmentUsecase(&scheduleSummaryRepo{listErr: dbErr}, nil, nil, nil)
	if _, err := u.List(context.Background(), nil, &parentID, domain.EnrollmentQuery{}); !errors.Is(err, dbErr) {
		t.Fatalf("List err = %v, want %v", err, dbErr)
	}
}
