package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

type bindingScheduleUsecase struct {
	usecase.ScheduleUsecase
}

func (bindingScheduleUsecase) CreateInitialSchedules(context.Context, uuid.UUID, *domain.CreateInitialSchedulesRequest) (*domain.CreateInitialSchedulesResponse, error) {
	return nil, nil
}

func TestCreateInitialSchedulesRejectsBindingBoundsAsHTTP400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewScheduleHandler(bindingScheduleUsecase{})
	router := gin.New()
	router.POST("/schedules", func(c *gin.Context) {
		c.Set("tenant_id", uuid.NewString())
		handler.CreateInitialSchedules(c)
	})

	tests := []struct {
		name string
		body string
	}{
		{
			name: "location over varchar limit",
			body: `{"class_id":"` + uuid.NewString() + `","schedules":[{"capacity":1,"location":"` + strings.Repeat("x", 256) + `","day_of_week":1,"start_time":"09:00","end_time":"10:00"}]}`,
		},
		{
			name: "capacity over integer limit",
			body: `{"class_id":"` + uuid.NewString() + `","schedules":[{"capacity":2147483648,"day_of_week":1,"start_time":"09:00","end_time":"10:00"}]}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/schedules", bytes.NewBufferString(test.body)))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"status":"error"`) || !strings.Contains(response.Body.String(), `"data":null`) {
				t.Fatalf("response=%s, want existing error envelope", response.Body.String())
			}
		})
	}
}
