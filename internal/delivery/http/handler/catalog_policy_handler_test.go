package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type catalogPolicyUsecaseStub struct {
	list *domain.CatalogListResponse
	err  error
}

func (s *catalogPolicyUsecaseStub) ListClasses(context.Context, domain.CatalogQuery) (*domain.CatalogListResponse, error) {
	return s.list, s.err
}
func (s *catalogPolicyUsecaseStub) GetClass(context.Context, uuid.UUID) (*domain.CatalogItem, error) {
	return nil, s.err
}

func TestCatalogPolicyHTTPClosedAndUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &catalogPolicyUsecaseStub{list: &domain.CatalogListResponse{Items: []domain.CatalogItem{}, Pagination: domain.Pagination{Page: 1, PageSize: 20}}}
	h := NewCatalogHandler(stub)
	router := gin.New()
	router.GET("/catalog/classes", h.ListClasses)
	router.GET("/catalog/classes/:id", h.GetClass)
	check := func(path string, code int, fragment string) {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != code || !strings.Contains(response.Body.String(), fragment) {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
	}
	check("/catalog/classes", http.StatusOK, `"catalog_open":false`)
	stub.err = domain.ErrCatalogClosed
	check("/catalog/classes/"+uuid.NewString(), http.StatusNotFound, "Public catalog is closed")
	stub.err = domain.ErrCatalogPolicyUnavailable
	check("/catalog/classes", http.StatusServiceUnavailable, "Public catalog policy unavailable")
	check("/catalog/classes/"+uuid.NewString(), http.StatusServiceUnavailable, "Public catalog policy unavailable")
}
