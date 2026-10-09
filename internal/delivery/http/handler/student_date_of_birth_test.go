package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

type dobRepository struct {
	repository.StudentRepository
	student *domain.Student
	writes  int
}

func (r *dobRepository) Create(_ context.Context, s *domain.Student) error {
	r.writes++
	r.student = s
	return nil
}
func (r *dobRepository) Update(_ context.Context, s *domain.Student) error {
	r.writes++
	r.student = s
	return nil
}
func (r *dobRepository) GetByIDForAccess(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) (*domain.Student, error) {
	return r.student, nil
}

type dobTransaction struct{}

func (dobTransaction) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// Exercise JSON binding, the real usecase, and HTTP error mapping together.
func TestStudentDateOfBirthHTTP(t *testing.T) {
	today := time.Now().In(time.FixedZone("WIB", 7*60*60))
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		for _, test := range []struct {
			name, date string
			status     int
		}{
			{"tomorrow", today.AddDate(0, 0, 1).Format("2006-01-02"), http.StatusBadRequest},
			{"today", today.Format("2006-01-02"), http.StatusOK},
			{"past", "2015-01-01", http.StatusOK},
			{"invalid", "2026-02-30", http.StatusBadRequest},
		} {
			t.Run(method+"/"+test.name, func(t *testing.T) {
				parent, id := uuid.New(), uuid.New()
				repo := &dobRepository{student: &domain.Student{ID: id, ParentID: parent, FirstName: "Original"}}
				h := NewStudentHandler(usecase.NewStudentUsecase(repo, nil, dobTransaction{}))
				isParent := true
				c, recorder := newStudentHandlerContext(parent.String(), "", &isParent)
				c.Params = gin.Params{{Key: "id", Value: id.String()}}
				c.Request = httptest.NewRequest(method, "/api/v1/students/"+id.String(), strings.NewReader(`{"parent_id":"`+parent.String()+`","first_name":"Student","date_of_birth":"`+test.date+`"}`))
				c.Request.Header.Set("Content-Type", "application/json")
				want := test.status
				if method == http.MethodPost {
					h.Create(c)
					if want == http.StatusOK {
						want = http.StatusCreated
					}
				} else {
					h.Update(c)
				}
				if recorder.Code != want {
					t.Fatalf("status=%d want=%d body=%s", recorder.Code, want, recorder.Body.String())
				}
				if want == http.StatusBadRequest && repo.writes != 0 {
					t.Fatal("invalid date was persisted")
				}
				if want != http.StatusBadRequest && repo.writes != 1 {
					t.Fatal("valid date was not persisted")
				}
			})
		}
	}
}
