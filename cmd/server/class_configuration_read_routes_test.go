package main

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"testing"
)

type classReadRouteStore struct {
	tenant, id uuid.UUID
	calls      int
	err        error
}

func (s *classReadRouteStore) Class(_ context.Context, tenant, id uuid.UUID) (*domain.Class, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if tenant != s.tenant || id != s.id {
		return nil, gorm.ErrRecordNotFound
	}
	return &domain.Class{ID: id, TenantID: tenant}, nil
}
func (s *classReadRouteStore) Schedules(c context.Context, tenant, id uuid.UUID, _ domain.ListQuery) ([]domain.ClassSchedule, int64, error) {
	_, err := s.Class(c, tenant, id)
	return []domain.ClassSchedule{}, 0, err
}
func TestKEL227ClassReadRouteSecurity(t *testing.T) {
	for _, suffix := range []string{"", "/schedules"} {
		for _, tc := range []struct {
			name                                        string
			class, schedule, parent, anonymous, foreign bool
			permissionErr, storeErr                     error
			query                                       string
			status, calls                               int
		}{
			{name: "authorized", class: true, schedule: true, status: 200, calls: 1},
			{name: "no class permission", schedule: true, status: 403},
			{name: "parent", class: true, schedule: true, parent: true, status: 403},
			{name: "no session", anonymous: true, status: 401},
			{name: "inactive or denied member", status: 403},
			{name: "permission outage", class: true, schedule: true, permissionErr: errors.New("identity unavailable"), status: 503},
			{name: "foreign class", class: true, schedule: true, foreign: true, status: 404, calls: 1},
			{name: "store outage", class: true, schedule: true, storeErr: errors.New("private connection detail"), status: 503, calls: 1},
		} {
			t.Run(suffix+tc.name, func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				tenant, id := uuid.New(), uuid.New()
				store := &classReadRouteStore{tenant: tenant, id: id, err: tc.storeErr}
				p := &routePermissionClient{permissions: map[string]bool{"class:read": tc.class, "schedule:read": tc.schedule}, err: tc.permissionErr}
				r := gin.New()
				registerRoutes(r, routeHandlers{classRead: &handler.ClassConfigurationReadHandler{Read: &usecase.ClassConfigurationRead{Store: store}}}, routeTestSecret, "internal", p)
				claims := middleware.Claims{UserID: uuid.NewString(), TenantID: tenant.String(), RoleID: uuid.NewString(), MemberID: uuid.NewString(), IsParent: tc.parent}
				if tc.foreign {
					claims.TenantID = uuid.NewString()
				}
				token := routeToken(t, claims)
				if tc.anonymous {
					token = ""
				}
				req := httptest.NewRequest("GET", "/api/v1/classes/"+id.String()+suffix+tc.query, nil)
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				req.Header.Set("X-Tenant-ID", uuid.NewString())
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != tc.status || store.calls != tc.calls {
					t.Fatalf("status=%d calls=%d body=%s", w.Code, store.calls, w.Body.String())
				}
				if strings.Contains(w.Body.String(), "private connection detail") {
					t.Fatal("error leaked")
				}
				if store.calls > 0 && w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("read cacheable")
				}
			})
		}
	}
	// Class-only grants cannot read schedule configuration; pagination is parsed
	// before a successful store call and never silently capped or defaulted.
	for _, query := range []string{"?page=0", "?page=bad", "?page_size=101"} {
		tenant, id := uuid.New(), uuid.New()
		store := &classReadRouteStore{tenant: tenant, id: id}
		p := &routePermissionClient{permissions: map[string]bool{"class:read": true, "schedule:read": true}}
		r := gin.New()
		registerRoutes(r, routeHandlers{classRead: &handler.ClassConfigurationReadHandler{Read: &usecase.ClassConfigurationRead{Store: store}}}, routeTestSecret, "internal", p)
		token := routeToken(t, middleware.Claims{UserID: uuid.NewString(), TenantID: tenant.String(), RoleID: uuid.NewString(), MemberID: uuid.NewString()})
		w := routeRequest(r, "GET", "/api/v1/classes/"+id.String()+"/schedules"+query, "", token)
		if w.Code != 400 || store.calls != 0 {
			t.Fatal("invalid pagination reached store")
		}
	}
	tenant, id := uuid.New(), uuid.New()
	store := &classReadRouteStore{tenant: tenant, id: id}
	p := &routePermissionClient{permissions: map[string]bool{"class:read": true}}
	r := gin.New()
	registerRoutes(r, routeHandlers{classRead: &handler.ClassConfigurationReadHandler{Read: &usecase.ClassConfigurationRead{Store: store}}}, routeTestSecret, "internal", p)
	token := routeToken(t, middleware.Claims{UserID: uuid.NewString(), TenantID: tenant.String(), RoleID: uuid.NewString(), MemberID: uuid.NewString()})
	w := routeRequest(r, "GET", "/api/v1/classes/"+id.String()+"/schedules", "", token)
	if w.Code != 403 || store.calls != 0 {
		t.Fatal("schedule permission bypassed")
	}
}
