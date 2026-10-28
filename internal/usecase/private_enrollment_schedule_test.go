package usecase

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestPrivateEnrollmentScheduleSummaryOwnershipAndStates(t *testing.T) {
	id, cls := uuid.New(), uuid.New()
	date := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	e := &domain.Enrollment{ID: id, ClassID: cls, PrivateSchedulesLoaded: true}
	own := domain.ClassSchedule{ID: uuid.New(), EnrollmentID: &id, ClassID: cls, DayOfWeek: 2, StartTime: "10:00:00", EndTime: "11:00:00", ValidFrom: &date, TutorID: ptrToUUID(uuid.New())}
	other := own
	other.ID = uuid.New()
	other.EnrollmentID = ptrToUUID(uuid.New())
	wrongClass := own
	wrongClass.ID = uuid.New()
	wrongClass.ClassID = uuid.New()
	deleted := own
	deleted.ID = uuid.New()
	deleted.DeletedAt = gorm.DeletedAt{Valid: true, Time: date}
	e.PrivateSchedules = []domain.ClassSchedule{own, own, other, wrongClass, deleted}
	response := enrollmentResponse(e)
	if response.Schedule != nil || response.ScheduleID != nil || response.PrivateSchedules == nil || len(*response.PrivateSchedules) != 1 {
		t.Fatalf("projection=%+v", response)
	}
	b, _ := json.Marshal(response)
	if strings.Contains(string(b), "tutor_id") || !strings.Contains(string(b), "valid_from") {
		t.Fatal(string(b))
	}
	e.PrivateSchedules = nil
	b, _ = json.Marshal(enrollmentResponse(e))
	if !strings.Contains(string(b), `"private_schedules":[]`) {
		t.Fatal(string(b))
	}
	e.PrivateSchedulesLoaded = false
	b, _ = json.Marshal(enrollmentResponse(e))
	if strings.Contains(string(b), "private_schedules") {
		t.Fatal("unloaded read looks empty")
	}
}
