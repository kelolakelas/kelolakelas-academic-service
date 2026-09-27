package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
)

func TestCatalogQueryValidateBounds(t *testing.T) {
	tests := []struct {
		name  string
		query CatalogQuery
		valid bool
	}{
		{name: "valid maximum page", query: CatalogQuery{ListQuery: ListQuery{Page: MaxPage, PageSize: MaxPageSize}, RadiusKM: 25, Sort: "newest"}, valid: true},
		{name: "page over maximum", query: CatalogQuery{ListQuery: ListQuery{Page: MaxPage + 1, PageSize: 20}, Sort: "newest"}, valid: false},
		{name: "search over maximum", query: CatalogQuery{ListQuery: ListQuery{Page: 1, PageSize: 20, Search: string(make([]rune, MaxSearchLength+1))}, Sort: "newest"}, valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.query.Validate() == nil; got != test.valid {
				t.Fatalf("valid=%v, want %v", got, test.valid)
			}
		})
	}
}

func TestBindingAcceptsAndRejectsVarchar255Fields(t *testing.T) {
	tests := []struct {
		name  string
		build func(string) interface{}
	}{
		{name: "create category name", build: func(value string) interface{} { return CreateCategoryRequest{Name: value} }},
		{name: "create class name", build: func(value string) interface{} {
			return CreateClassRequest{CategoryID: uuid.New(), Name: value, Type: "private", Price: 1}
		}},
		{name: "create class payload name", build: func(value string) interface{} { return CreateClassPayload{Name: value, Type: "private", Price: 0} }},
		{name: "create report title", build: func(value string) interface{} { return CreateReportRequest{EnrollmentID: uuid.New(), Title: value} }},
		{name: "update report title", build: func(value string) interface{} { return UpdateReportRequest{Title: value} }},
		{name: "schedule location", build: func(value string) interface{} {
			return ScheduleItemRequest{Capacity: 1, Location: &value, DayOfWeek: 1, StartTime: "09:00", EndTime: "10:00"}
		}},
		{name: "reschedule location", build: func(value string) interface{} {
			return RescheduleSessionRequest{SessionID: uuid.New(), NewSessionDate: validValidationDate(), NewStartTime: "09:00", NewEndTime: "10:00", NewLocation: &value}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := binding.Validator.ValidateStruct(test.build(strings.Repeat("x", 255))); err != nil {
				t.Fatalf("255 ASCII characters rejected: %v", err)
			}
			if err := binding.Validator.ValidateStruct(test.build(strings.Repeat("x", 256))); err == nil {
				t.Fatal("256 ASCII characters accepted")
			}
			if err := binding.Validator.ValidateStruct(test.build(strings.Repeat("é", 255))); err != nil {
				t.Fatalf("255 multibyte characters rejected: %v", err)
			}
			if err := binding.Validator.ValidateStruct(test.build(strings.Repeat("é", 256))); err == nil {
				t.Fatal("256 multibyte characters accepted")
			}
		})
	}
}

func TestScheduleCapacityBindingBounds(t *testing.T) {
	build := func(capacity int) ScheduleItemRequest {
		return ScheduleItemRequest{Capacity: capacity, DayOfWeek: 1, StartTime: "09:00", EndTime: "10:00"}
	}
	if err := binding.Validator.ValidateStruct(build(2147483647)); err != nil {
		t.Fatalf("maximum capacity rejected: %v", err)
	}
	if err := binding.Validator.ValidateStruct(build(2147483648)); err == nil {
		t.Fatal("capacity above integer maximum accepted")
	}
	if err := binding.Validator.ValidateStruct(build(0)); err == nil {
		t.Fatal("zero capacity accepted")
	}
}

func validValidationDate() time.Time {
	return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
}
