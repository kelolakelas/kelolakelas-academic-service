package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

type failingCategoryUsecase struct {
	usecase.CategoryUsecase
	err error
}

func (m failingCategoryUsecase) DeleteCategory(context.Context, uuid.UUID, uuid.UUID) error {
	return m.err
}

func (m failingCategoryUsecase) CreateCategory(context.Context, uuid.UUID, *domain.CreateCategoryRequest) (*domain.CategoryResponse, error) {
	return nil, m.err
}

// Embedded usecase interfaces keep each failure stub focused on the handler method under test.
type failingClassUsecase struct {
	usecase.ClassUsecase
	err error
}

func (m failingClassUsecase) CreateClass(context.Context, uuid.UUID, *domain.CreateClassRequest) (*domain.ClassResponse, error) {
	return nil, m.err
}
func (m failingClassUsecase) DeleteClass(context.Context, uuid.UUID, uuid.UUID) error { return m.err }
func (m failingClassUsecase) UpdateClassPublication(context.Context, uuid.UUID, uuid.UUID, *domain.UpdateClassPublicationRequest) (*domain.ClassResponse, error) {
	return nil, m.err
}

type failingClassCreationUsecase struct{ err error }

func (m failingClassCreationUsecase) CreateClassWithCategory(context.Context, uuid.UUID, *domain.CreateClassWithCategoryRequest) (*domain.CreateClassWithCategoryResponse, error) {
	return nil, m.err
}

type failingScheduleMethodsUsecase struct {
	usecase.ScheduleUsecase
	err error
}

func (m failingScheduleMethodsUsecase) CreateInitialSchedules(context.Context, uuid.UUID, *domain.CreateInitialSchedulesRequest) (*domain.CreateInitialSchedulesResponse, error) {
	return nil, m.err
}
func (m failingScheduleMethodsUsecase) RescheduleSession(context.Context, uuid.UUID, *domain.RescheduleSessionRequest) (*domain.RescheduleSessionResponse, error) {
	return nil, m.err
}
func (m failingScheduleMethodsUsecase) ChangeSchedulePermanent(context.Context, uuid.UUID, *domain.PermanentScheduleChangeRequest) (*domain.PermanentScheduleChangeResponse, error) {
	return nil, m.err
}
func (m failingScheduleMethodsUsecase) ChangeTutorTemporary(context.Context, uuid.UUID, *domain.SubstituteTutorRequest) (*domain.SubstituteTutorResponse, error) {
	return nil, m.err
}
func (m failingScheduleMethodsUsecase) ChangeTutorPermanent(context.Context, uuid.UUID, *domain.PermanentTutorChangeRequest) (*domain.PermanentTutorChangeResponse, error) {
	return nil, m.err
}
func (m failingScheduleMethodsUsecase) GetSessionAttendees(context.Context, uuid.UUID, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, m.err
}

func (m failingEnrollmentUsecase) EnrollStudent(context.Context, uuid.UUID, *domain.EnrollStudentRequest) (*domain.EnrollmentResponse, error) {
	return nil, m.err
}
func (m failingEnrollmentUsecase) AssignSchedule(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, m.err
}
func (m failingEnrollmentUsecase) CancelPendingEnrollment(context.Context, uuid.UUID, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, m.err
}
func (m failingEnrollmentUsecase) ReleaseEnrollment(context.Context, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, m.err
}
func (m failingEnrollmentUsecase) ActivateEnrollment(context.Context, uuid.UUID) (*domain.EnrollmentResponse, error) {
	return nil, m.err
}

func (m failingClassUpdateUsecase) CreateClass(context.Context, uuid.UUID, *domain.CreateClassRequest) (*domain.ClassResponse, error) {
	return nil, m.err
}
func (m failingClassUpdateUsecase) DeleteClass(context.Context, uuid.UUID, uuid.UUID) error {
	return m.err
}
func (m failingClassUpdateUsecase) UpdateClassPublication(context.Context, uuid.UUID, uuid.UUID, *domain.UpdateClassPublicationRequest) (*domain.ClassResponse, error) {
	return nil, m.err
}

func (m failingScheduleUsecase) CreateInitialSchedules(context.Context, uuid.UUID, *domain.CreateInitialSchedulesRequest) (*domain.CreateInitialSchedulesResponse, error) {
	return nil, m.err
}
func (m failingScheduleUsecase) RescheduleSession(context.Context, uuid.UUID, *domain.RescheduleSessionRequest) (*domain.RescheduleSessionResponse, error) {
	return nil, m.err
}
func (m failingScheduleUsecase) ChangeSchedulePermanent(context.Context, uuid.UUID, *domain.PermanentScheduleChangeRequest) (*domain.PermanentScheduleChangeResponse, error) {
	return nil, m.err
}
func (m failingScheduleUsecase) ChangeTutorTemporary(context.Context, uuid.UUID, *domain.SubstituteTutorRequest) (*domain.SubstituteTutorResponse, error) {
	return nil, m.err
}
func (m failingScheduleUsecase) ChangeTutorPermanent(context.Context, uuid.UUID, *domain.PermanentTutorChangeRequest) (*domain.PermanentTutorChangeResponse, error) {
	return nil, m.err
}
func (m failingScheduleUsecase) GetSessionAttendees(context.Context, uuid.UUID, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, m.err
}

func TestRemainingInternalErrorBranchesAreLoggedAndRedacted(t *testing.T) {
	const sentinel = "private database details"
	tests := []struct {
		name      string
		operation string
		handler   func(error) *httptest.ResponseRecorder
	}{
		{"create category", "create category", func(err error) *httptest.ResponseRecorder {
			router := gin.New()
			h := NewCategoryHandler(failingCategoryUsecase{err: err})
			router.POST("/categories", func(c *gin.Context) { c.Set("tenant_id", uuid.NewString()); h.Create(c) })
			request := httptest.NewRequest(http.MethodPost, "/categories", strings.NewReader(`{"name":"X"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			return response
		}},
		{"create class", "create class", func(err error) *httptest.ResponseRecorder {
			router := gin.New()
			h := NewClassHandler(failingClassUpdateUsecase{err: err}, updateCreationUsecaseMock{})
			router.POST("/classes", func(c *gin.Context) { c.Set("tenant_id", uuid.NewString()); h.Create(c) })
			request := httptest.NewRequest(http.MethodPost, "/classes", strings.NewReader(`{"category_id":"`+uuid.NewString()+`","name":"X","type":"private","price":1}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			return response
		}},
		{"create class with category", "create class with category", func(err error) *httptest.ResponseRecorder {
			router := gin.New()
			h := NewClassHandler(failingClassUsecase{}, failingClassCreationUsecase{err: err})
			router.POST("/classes/with-category", func(c *gin.Context) { c.Set("tenant_id", uuid.NewString()); h.CreateWithCategory(c) })
			request := httptest.NewRequest(http.MethodPost, "/classes/with-category", strings.NewReader(`{"category_id":"`+uuid.NewString()+`","class":{"name":"X","type":"private","price":1},"teacher_ids":["`+uuid.NewString()+`"]}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			return response
		}},
		{"delete class", "delete class", func(err error) *httptest.ResponseRecorder {
			router := gin.New()
			h := NewClassHandler(failingClassUsecase{err: err}, updateCreationUsecaseMock{})
			router.DELETE("/classes/:id", func(c *gin.Context) { c.Set("tenant_id", uuid.NewString()); h.Delete(c) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/classes/"+uuid.NewString(), nil))
			return response
		}},
		{"update class publication status", "update class publication status", func(err error) *httptest.ResponseRecorder {
			router := gin.New()
			h := NewClassHandler(failingClassUsecase{err: err}, updateCreationUsecaseMock{})
			router.PATCH("/classes/:id/publication", func(c *gin.Context) { c.Set("tenant_id", uuid.NewString()); h.UpdatePublication(c) })
			request := httptest.NewRequest(http.MethodPatch, "/classes/"+uuid.NewString()+"/publication", strings.NewReader(`{"is_published":true}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			return response
		}},
		{"create initial schedules", "create initial schedules", scheduleFailureRequest(func(h *ScheduleHandler, c *gin.Context) { h.CreateInitialSchedules(c) }, http.MethodPost, "/schedules", `{"class_id":"`+uuid.NewString()+`","schedules":[{"capacity":1,"day_of_week":1,"start_time":"09:00","end_time":"10:00"}]}`)},
		{"reschedule session", "reschedule session", scheduleFailureRequest(func(h *ScheduleHandler, c *gin.Context) { h.RescheduleSession(c) }, http.MethodPost, "/sessions/"+uuid.NewString()+"/reschedule", `{"session_id":"`+uuid.NewString()+`","new_session_date":"2026-09-27T00:00:00Z","new_start_time":"09:00","new_end_time":"10:00"}`)},
		{"permanently change schedule", "permanently change schedule", scheduleFailureRequest(func(h *ScheduleHandler, c *gin.Context) { h.ChangeSchedulePermanent(c) }, http.MethodPut, "/schedules/"+uuid.NewString()+"/permanent", `{"old_schedule_id":"`+uuid.NewString()+`","new_day_of_week":2,"new_start_time":"09:00","new_end_time":"10:00","effective_date":"2026-09-27T00:00:00Z"}`)},
		{"update substitute tutor", "update substitute tutor", scheduleFailureRequest(func(h *ScheduleHandler, c *gin.Context) { h.ChangeTutorTemporary(c) }, http.MethodPatch, "/sessions/"+uuid.NewString()+"/substitute-tutor", `{"session_id":"`+uuid.NewString()+`","substitute_tutor_id":"`+uuid.NewString()+`"}`)},
		{"permanently change tutor", "permanently change tutor", scheduleFailureRequest(func(h *ScheduleHandler, c *gin.Context) { h.ChangeTutorPermanent(c) }, http.MethodPatch, "/schedules/"+uuid.NewString()+"/tutor-permanent", `{"schedule_id":"`+uuid.NewString()+`","new_tutor_id":"`+uuid.NewString()+`","effective_date":"2026-09-27T00:00:00Z"}`)},
		{"get session attendees", "get session attendees", scheduleFailureRequest(func(h *ScheduleHandler, c *gin.Context) { h.GetSessionAttendees(c) }, http.MethodGet, "/sessions/"+uuid.NewString()+"/attendees", "")},
		{"assign enrollment schedule", "assign enrollment schedule", enrollmentFailureRequest(func(h *EnrollmentHandler, c *gin.Context) { h.AssignSchedule(c) }, http.MethodPatch, "/enrollments/"+uuid.NewString()+"/schedule", `{"schedule_id":"`+uuid.NewString()+`"}`)},
		{"create tenant enrollment", "create tenant enrollment", enrollmentFailureRequest(func(h *EnrollmentHandler, c *gin.Context) { h.Create(c) }, http.MethodPost, "/tenants/"+uuid.NewString()+"/enrollments", `{"class_id":"`+uuid.NewString()+`","student_id":"`+uuid.NewString()+`","billing_cycle":"monthly"}`)},
		{"cancel enrollment", "cancel enrollment", enrollmentFailureRequest(func(h *EnrollmentHandler, c *gin.Context) { h.Cancel(c) }, http.MethodPost, "/enrollments/"+uuid.NewString()+"/cancel", "")},
		{"release enrollment", "release enrollment", enrollmentFailureRequest(func(h *EnrollmentHandler, c *gin.Context) { h.ReleaseInternal(c) }, http.MethodPut, "/internal/enrollments/"+uuid.NewString()+"/release", "")},
		{"activate enrollment", "activate enrollment", enrollmentFailureRequest(func(h *EnrollmentHandler, c *gin.Context) { h.ActivateInternal(c) }, http.MethodPut, "/internal/enrollments/"+uuid.NewString()+"/activate", "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previousLogger) })
			response := tt.handler(errors.New(sentinel))
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d want=500 body=%s", response.Code, response.Body.String())
			}
			var envelope struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if envelope.Status != "error" {
				t.Fatalf("unexpected 500 envelope: %+v", envelope)
			}
			if strings.Contains(response.Body.String(), sentinel) {
				t.Fatalf("response leaked error: %s", response.Body.String())
			}
			if !strings.Contains(logs.String(), sentinel) || !strings.Contains(logs.String(), tt.operation) {
				t.Fatalf("log missing error or operation: %s", logs.String())
			}
		})
	}
}

func scheduleFailureRequest(call func(*ScheduleHandler, *gin.Context), method, path, body string) func(error) *httptest.ResponseRecorder {
	return func(err error) *httptest.ResponseRecorder {
		router := gin.New()
		h := NewScheduleHandler(failingScheduleMethodsUsecase{err: err})
		router.Handle(method, path, func(c *gin.Context) {
			c.Set("tenant_id", uuid.NewString())
			for _, segment := range strings.Split(path, "/") {
				if id, err := uuid.Parse(segment); err == nil {
					c.Params = gin.Params{{Key: "id", Value: id.String()}}
					break
				}
			}
			call(h, c)
		})
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
}

func enrollmentFailureRequest(call func(*EnrollmentHandler, *gin.Context), method, path, body string) func(error) *httptest.ResponseRecorder {
	return func(err error) *httptest.ResponseRecorder {
		router := gin.New()
		h := NewEnrollmentHandler(failingEnrollmentUsecase{err: err})
		router.Handle(method, path, func(c *gin.Context) {
			c.Set("tenant_id", uuid.NewString())
			c.Set("user_id", uuid.NewString())
			c.Set("is_parent", !strings.Contains(path, "/tenants/"))
			c.Set("is_tenant", true)
			c.Set("email", "parent@example.com")
			for _, segment := range strings.Split(path, "/") {
				if id, err := uuid.Parse(segment); err == nil {
					c.Params = gin.Params{{Key: "id", Value: id.String()}, {Key: "tenant_id", Value: id.String()}}
					c.Set("tenant_id", id.String())
					break
				}
			}
			call(h, c)
		})
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "test-key")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
}

func TestReleaseAndActivateInternalConflictMessagesRemainUnchanged(t *testing.T) {
	for _, tt := range []struct {
		name, path, prefix string
		call               func(*EnrollmentHandler, *gin.Context)
	}{
		{"release", "/internal/enrollments/" + uuid.NewString() + "/release", "Failed to release enrollment: ", func(h *EnrollmentHandler, c *gin.Context) { h.ReleaseInternal(c) }},
		{"activate", "/internal/enrollments/" + uuid.NewString() + "/activate", "Failed to update enrollment status: ", func(h *EnrollmentHandler, c *gin.Context) { h.ActivateInternal(c) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			h := NewEnrollmentHandler(failingEnrollmentUsecase{err: domain.ErrInvalidEnrollmentTransition})
			router.Handle(http.MethodPut, "/internal/enrollments/:id/"+tt.name, func(c *gin.Context) { tt.call(h, c) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, tt.path, nil))
			var body struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Code != http.StatusConflict || body.Message != tt.prefix+domain.ErrInvalidEnrollmentTransition.Error() {
				t.Fatalf("status/message=%d %q", response.Code, body.Message)
			}
		})
	}
}

type failingClassUpdateUsecase struct {
	usecase.ClassUsecase
	err error
}

func (m failingClassUpdateUsecase) UpdateClass(context.Context, uuid.UUID, uuid.UUID, *domain.UpdateClassRequest) (*domain.ClassResponse, error) {
	return nil, m.err
}

type failingScheduleUsecase struct {
	usecase.ScheduleUsecase
	err error
}

func (m failingScheduleUsecase) DeleteSchedule(context.Context, uuid.UUID, uuid.UUID) error {
	return m.err
}

type failingEnrollmentUsecase struct {
	usecase.EnrollmentUsecase
	err error
}

func (m failingEnrollmentUsecase) EnrollPublic(context.Context, uuid.UUID, uuid.UUID, *domain.PublicEnrollmentRequest, string) (*domain.PublicEnrollmentResponse, error) {
	return nil, m.err
}

func TestCategoryDeleteInternalErrorIsLoggedAndRedacted(t *testing.T) {
	assertInternalErrorIsLoggedAndRedacted(t, "delete category", func(err error) *httptest.ResponseRecorder {
		router := gin.New()
		handler := NewCategoryHandler(failingCategoryUsecase{err: err})
		router.DELETE("/categories/:id", func(c *gin.Context) {
			c.Set("tenant_id", uuid.NewString())
			handler.Delete(c)
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/categories/"+uuid.NewString(), nil))
		return response
	})
}

func TestClassUpdateInternalErrorIsLoggedAndRedacted(t *testing.T) {
	assertInternalErrorIsLoggedAndRedacted(t, "update class", func(err error) *httptest.ResponseRecorder {
		router := gin.New()
		handler := NewClassHandler(failingClassUpdateUsecase{err: err}, updateCreationUsecaseMock{})
		router.PATCH("/classes/:id", func(c *gin.Context) {
			c.Set("tenant_id", uuid.NewString())
			handler.Update(c)
		})
		request := httptest.NewRequest(http.MethodPatch, "/classes/"+uuid.NewString(), strings.NewReader(`{"name":"X"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	})
}

func TestScheduleDeleteInternalErrorIsLoggedAndRedacted(t *testing.T) {
	assertInternalErrorIsLoggedAndRedacted(t, "delete schedule", func(err error) *httptest.ResponseRecorder {
		router := gin.New()
		handler := NewScheduleHandler(failingScheduleUsecase{err: err})
		router.DELETE("/schedules/:id", func(c *gin.Context) {
			c.Set("tenant_id", uuid.NewString())
			handler.Delete(c)
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/schedules/"+uuid.NewString(), nil))
		return response
	})
}

func TestCatalogEnrollmentInternalErrorIsLoggedAndRedacted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assertInternalErrorIsLoggedAndRedacted(t, "create catalog enrollment", func(err error) *httptest.ResponseRecorder {
		router := gin.New()
		handler := NewEnrollmentHandler(failingEnrollmentUsecase{err: err})
		router.POST("/catalog/classes/:class_id/enrollments", func(c *gin.Context) {
			c.Set("user_id", uuid.NewString())
			c.Set("is_parent", true)
			handler.CreateCatalogEnrollment(c)
		})
		request := httptest.NewRequest(http.MethodPost, "/catalog/classes/"+uuid.NewString()+"/enrollments", strings.NewReader(`{"student_id":"`+uuid.NewString()+`","billing_cycle":"monthly"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "test-key")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	})
}

func assertInternalErrorIsLoggedAndRedacted(t *testing.T, operation string, request func(error) *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const sentinel = "private database details"
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	response := request(errors.New(sentinel))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want=500 body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), sentinel) {
		t.Fatalf("response leaked internal error: %s", response.Body.String())
	}
	if !strings.Contains(logs.String(), sentinel) || !strings.Contains(logs.String(), operation) {
		t.Fatalf("log missing original error or operation context: %s", logs.String())
	}
}

func TestCatalogEnrollmentDomainErrorsRemainUnprocessable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, domainErr := range []error{domain.ErrStudentOwnership, domain.ErrClassNotEnrollable} {
		t.Run(domainErr.Error(), func(t *testing.T) {
			router := gin.New()
			handler := NewEnrollmentHandler(failingEnrollmentUsecase{err: domainErr})
			router.POST("/catalog/classes/:class_id/enrollments", func(c *gin.Context) {
				c.Set("user_id", uuid.NewString())
				c.Set("is_parent", true)
				handler.CreateCatalogEnrollment(c)
			})
			request := httptest.NewRequest(http.MethodPost, "/catalog/classes/"+uuid.NewString()+"/enrollments", strings.NewReader(`{"student_id":"`+uuid.NewString()+`","billing_cycle":"monthly"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "test-key")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status=%d want=422 body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), domainErr.Error()) {
				t.Fatalf("response lost domain message %q: %s", domainErr, response.Body.String())
			}
		})
	}
}
