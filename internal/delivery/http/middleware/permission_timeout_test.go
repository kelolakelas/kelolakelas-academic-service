package middleware

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

// startIdentityPermissionServer serves tenant.PermissionService/CheckPermission on a
// loopback port with handler and returns its address.
func startIdentityPermissionServer(t *testing.T, handler func(context.Context, *structpb.Struct) (*structpb.Struct, error)) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "tenant.PermissionService",
		HandlerType: (*interface{})(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "CheckPermission",
			Handler: func(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
				req := new(structpb.Struct)
				if err := dec(req); err != nil {
					return nil, err
				}
				return handler(ctx, req)
			},
		}},
	}, struct{}{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

func permissionRouterWithClient(t *testing.T, client grpcclient.PermissionClient, called *bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	roleID, tenantID := uuid.New(), uuid.New()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("role_id", roleID.String())
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	router.POST("/mutate", RequirePermission(client, "class:update"), func(c *gin.Context) {
		*called = true
		c.Status(http.StatusNoContent)
	})
	return router
}

// TestRequirePermissionAnswers503WhenIdentityHangs wires the production permission client
// (KEL-78) against an identity that never answers: the guarded route must answer 503
// "Authorization service unavailable" within the configured timeout, without running the
// handler.
func TestRequirePermissionAnswers503WhenIdentityHangs(t *testing.T) {
	const timeout = 150 * time.Millisecond
	addr := startIdentityPermissionServer(t, func(ctx context.Context, _ *structpb.Struct) (*structpb.Struct, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	client, err := grpcclient.NewPermissionClient(addr, timeout)
	if err != nil {
		t.Fatalf("NewPermissionClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	called := false
	router := permissionRouterWithClient(t, client, &called)

	started := time.Now()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mutate", nil))
	elapsed := time.Since(started)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Authorization service unavailable") {
		t.Fatalf("unexpected body=%s", response.Body.String())
	}
	if called {
		t.Fatal("guarded handler ran while identity was not answering")
	}
	if elapsed > timeout+2*time.Second {
		t.Fatalf("503 took %s, want it bounded by the %s permission timeout", elapsed, timeout)
	}
}

// With a responsive identity the same production client keeps the allow and deny
// decisions unchanged.
func TestRequirePermissionDecisionsUnchangedWithResponsiveIdentity(t *testing.T) {
	cases := []struct {
		name       string
		allowed    bool
		wantStatus int
		wantCalled bool
	}{
		{name: "allowed", allowed: true, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "denied", allowed: false, wantStatus: http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr := startIdentityPermissionServer(t, func(_ context.Context, req *structpb.Struct) (*structpb.Struct, error) {
				if _, ok := req.GetFields()["tenant_id"]; !ok {
					t.Error("identity request is missing tenant_id")
				}
				return structpb.NewStruct(map[string]interface{}{"allowed": tc.allowed})
			})
			client, err := grpcclient.NewPermissionClient(addr, 2*time.Second)
			if err != nil {
				t.Fatalf("NewPermissionClient: %v", err)
			}
			t.Cleanup(func() { _ = client.Close() })

			called := false
			router := permissionRouterWithClient(t, client, &called)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mutate", nil))

			if response.Code != tc.wantStatus || called != tc.wantCalled {
				t.Fatalf("status=%d called=%t, want status=%d called=%t", response.Code, called, tc.wantStatus, tc.wantCalled)
			}
		})
	}
}
