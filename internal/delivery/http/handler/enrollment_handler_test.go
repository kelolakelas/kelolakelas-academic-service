package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

func TestCatalogEnrollmentErrorStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "schedule full is conflict", err: domain.ErrScheduleFull, want: http.StatusConflict},
		{name: "idempotency conflict", err: domain.ErrIdempotencyConflict, want: http.StatusConflict},
		{name: "ownership is unprocessable", err: domain.ErrStudentOwnership, want: http.StatusUnprocessableEntity},
		{name: "schedule required is unprocessable", err: domain.ErrScheduleRequired, want: http.StatusUnprocessableEntity},
		{name: "ended schedule is unprocessable", err: domain.ErrScheduleEnded, want: http.StatusUnprocessableEntity},
		{name: "class not found", err: domain.ErrClassNotFound, want: http.StatusNotFound},
		{name: "billing failure is server error", err: errors.New("generate enrollment invoice: provider unavailable"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := catalogEnrollmentErrorStatus(tt.err); got != tt.want {
				t.Fatalf("catalogEnrollmentErrorStatus() = %d, want %d", got, tt.want)
			}
		})
	}
}
