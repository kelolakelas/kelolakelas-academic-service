package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type reviewRepoStub struct {
	writes  int
	rating  int
	comment string
}

func (s *reviewRepoStub) Upsert(_ context.Context, _, _ uuid.UUID, rating int, comment string) error {
	s.writes++
	s.rating, s.comment = rating, comment
	return nil
}
func (s *reviewRepoStub) List(context.Context, uuid.UUID, int, int) ([]domain.PublicReview, int64, error) {
	return nil, 0, nil
}

func TestReviewWriteValidatesParentAndPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.NewString()
	for _, tc := range []struct {
		name, body string
		parent     bool
		want       int
		writes     int
	}{
		{"tenant denied", `{"rating":5}`, false, http.StatusForbidden, 0},
		{"missing rating", `{"comment":"hello"}`, true, http.StatusBadRequest, 0},
		{"low rating", `{"rating":0}`, true, http.StatusBadRequest, 0},
		{"high rating", `{"rating":6}`, true, http.StatusBadRequest, 0},
		{"long unicode comment", `{"rating":5,"comment":"` + strings.Repeat("界", domain.MaxReviewCommentLength+1) + `"}`, true, http.StatusBadRequest, 0},
		{"malformed json", `{`, true, http.StatusBadRequest, 0},
		{"valid unicode comment", `{"rating":5,"comment":"` + strings.Repeat("界", domain.MaxReviewCommentLength) + `"}`, true, http.StatusOK, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &reviewRepoStub{}
			router := gin.New()
			router.PUT("/enrollments/:id/review", func(c *gin.Context) { c.Set("is_parent", tc.parent); c.Set("user_id", uuid.NewString()); c.Next() }, NewReviewHandler(repo, nil).Upsert)
			request := httptest.NewRequest(http.MethodPut, "/enrollments/"+id+"/review", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want || repo.writes != tc.writes {
				t.Fatalf("status=%d writes=%d body=%s", response.Code, repo.writes, response.Body.String())
			}
		})
	}
}
