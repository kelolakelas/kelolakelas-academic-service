package main

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/paymentlink"
	"net/http"
	"testing"
)

type routeLinkUsecase struct {
	calls         int
	tenant, id    uuid.UUID
	authorization string
	err           error
}

func (u *routeLinkUsecase) SendPaymentLink(_ context.Context, tenant, id uuid.UUID, auth string) (*paymentlink.Message, error) {
	u.calls++
	u.tenant = tenant
	u.id = id
	u.authorization = auth
	return &paymentlink.Message{ID: uuid.New(), ConversationID: id, SenderKind: "system"}, u.err
}
func TestSendPaymentLinkRouteAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name                                         string
		enrollment, chat, parent, noToken, badTenant bool
		permissionErr                                error
		status                                       int
		calls                                        int
	}{
		{name: "authorized", enrollment: true, chat: true, status: 200, calls: 1},
		{name: "enrollment denied", chat: true, status: 403},
		{name: "chat denied", enrollment: true, status: 403},
		{name: "parent denied", enrollment: true, chat: true, parent: true, status: 403},
		{name: "no token", noToken: true, status: 401},
		{name: "invalid tenant", enrollment: true, chat: true, badTenant: true, status: 401},
		{name: "identity unavailable", enrollment: true, chat: true, permissionErr: errors.New("offline"), status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			p := &routePermissionClient{permissions: map[string]bool{"enrollment:update": tc.enrollment, "chat:manage": tc.chat}, err: tc.permissionErr}
			u := &routeLinkUsecase{}
			engine := gin.New()
			registerRoutes(engine, routeHandlers{privateRequest: handler.NewPrivateScheduleRequestHandler(nil).WithPaymentLink(u)}, routeTestSecret, "credential", p)
			claims := middleware.Claims{UserID: uuid.NewString(), TenantID: uuid.NewString(), RoleID: uuid.NewString(), MemberID: uuid.NewString(), IsParent: tc.parent}
			if tc.badTenant {
				claims.TenantID = ""
			}
			token := routeToken(t, claims)
			if tc.noToken {
				token = ""
			}
			id := uuid.New()
			w := routeRequest(engine, http.MethodPost, "/api/v1/schedule-requests/"+id.String()+"/send-payment-link", "", token)
			if w.Code != tc.status || u.calls != tc.calls {
				t.Fatalf("code=%d calls=%d body=%s", w.Code, u.calls, w.Body.String())
			}
			if tc.calls == 1 {
				if u.authorization != "Bearer "+token || u.tenant.String() != claims.TenantID || u.id != id || len(p.calls) != 2 || p.calls[0] != "enrollment:update" || p.calls[1] != "chat:manage" {
					t.Fatalf("scope/calls: %+v %+v", u, p)
				}
			}
		})
	}
}
func TestSendPaymentLinkRouteContract(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		err              error
		status, calls    int
	}{
		{name: "body rejected", body: `{"checkout_session_url":"https://attacker"}`, status: 400},
		{name: "invalid ID", path: "bad-id", status: 400},
		{name: "inactive", err: usecase.ErrPaymentLinkInactive, status: 409, calls: 1},
		{name: "upstream", err: paymentlink.ErrUnavailable, status: 503, calls: 1},
		{name: "success", status: 200, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			p := &routePermissionClient{permissions: map[string]bool{"enrollment:update": true, "chat:manage": true}}
			u := &routeLinkUsecase{err: tc.err}
			engine := gin.New()
			registerRoutes(engine, routeHandlers{privateRequest: handler.NewPrivateScheduleRequestHandler(nil).WithPaymentLink(u)}, routeTestSecret, "credential", p)
			claims := middleware.Claims{UserID: uuid.NewString(), TenantID: uuid.NewString(), RoleID: uuid.NewString(), MemberID: uuid.NewString()}
			id := tc.path
			if id == "" {
				id = uuid.NewString()
			}
			w := routeRequest(engine, "POST", "/api/v1/schedule-requests/"+id+"/send-payment-link", tc.body, routeToken(t, claims))
			if w.Code != tc.status || u.calls != tc.calls {
				t.Fatalf("code=%d calls=%d body=%s", w.Code, u.calls, w.Body.String())
			}
		})
	}
}
