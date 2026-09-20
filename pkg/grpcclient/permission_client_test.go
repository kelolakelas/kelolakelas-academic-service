package grpcclient

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

// fakeIdentityServer records the request fields identity would receive and answers with a
// canned decision. It implements the raw CheckPermission wire contract instead of importing
// identity, because the two services are separate Go modules that only share that contract.
type fakeIdentityServer struct {
	lastTenantID string
	lastRoleID   string
	lastPerm     string
	sawTenantID  bool
	allowed      bool
}

func (s *fakeIdentityServer) checkPermission(req *structpb.Struct) (*structpb.Struct, error) {
	fields := req.GetFields()
	s.lastRoleID = fields["role_id"].GetStringValue()
	s.lastPerm = fields["permission"].GetStringValue()
	if tenant, ok := fields["tenant_id"]; ok {
		s.sawTenantID = true
		s.lastTenantID = tenant.GetStringValue()
	}
	return structpb.NewStruct(map[string]interface{}{"allowed": s.allowed})
}

func newPermissionClientForTest(t *testing.T, server *fakeIdentityServer) PermissionClient {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	grpcServer.RegisterService(&grpc.ServiceDesc{
		ServiceName: "tenant.PermissionService",
		HandlerType: (*interface{})(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "CheckPermission",
			Handler: func(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
				req := new(structpb.Struct)
				if err := dec(req); err != nil {
					return nil, err
				}
				return server.checkPermission(req)
			},
		}},
	}, server)

	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial fake identity: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &permissionClient{conn: conn}
}

const (
	testTenantID = "11111111-1111-1111-1111-111111111111"
	testRoleID   = "22222222-2222-2222-2222-222222222222"
)

// TestCheckPermissionSendsTenantAlongsideRole is the contract test for KEL-20: academic must
// ask identity about a permission inside a specific tenant, and it must keep sending role_id
// and permission unchanged so the request stays backward compatible.
func TestCheckPermissionSendsTenantAlongsideRole(t *testing.T) {
	server := &fakeIdentityServer{allowed: true}
	client := newPermissionClientForTest(t, server)

	allowed, err := client.CheckPermission(context.Background(), testTenantID, testRoleID, "class:update")
	if err != nil {
		t.Fatalf("CheckPermission: %v", err)
	}
	if !allowed {
		t.Fatal("allowed=false, want true")
	}
	if !server.sawTenantID {
		t.Fatal("academic did not send tenant_id")
	}
	if server.lastTenantID != testTenantID || server.lastRoleID != testRoleID || server.lastPerm != "class:update" {
		t.Fatalf("identity received (tenant=%s role=%s permission=%s)", server.lastTenantID, server.lastRoleID, server.lastPerm)
	}
}

// TestCheckPermissionSurvivesIdentityIgnoringTenant reproduces the ADR 0002 deploy order from
// the academic side: an identity deployment that does not know tenant_id yet ignores the extra
// field, so catalog and schedule mutations keep working while both versions are live together.
func TestCheckPermissionSurvivesIdentityIgnoringTenant(t *testing.T) {
	server := &fakeIdentityServer{allowed: true}
	client := newPermissionClientForTest(t, server)

	allowed, err := client.CheckPermission(context.Background(), testTenantID, testRoleID, "class:update")
	if err != nil {
		t.Fatalf("an older identity deployment must still answer: %v", err)
	}
	if !allowed {
		t.Fatal("allowed=false, want true")
	}
	if server.lastRoleID != testRoleID || server.lastPerm != "class:update" {
		t.Fatalf("legacy keys changed: role=%s permission=%s", server.lastRoleID, server.lastPerm)
	}
}

// TestCheckPermissionReportsDenial proves a denial is returned as a decision instead of a
// transport error, so the middleware can tell "forbidden" apart from "identity unavailable".
func TestCheckPermissionReportsDenial(t *testing.T) {
	server := &fakeIdentityServer{allowed: false}
	client := newPermissionClientForTest(t, server)

	allowed, err := client.CheckPermission(context.Background(), testTenantID, testRoleID, "class:update")
	if err != nil {
		t.Fatalf("CheckPermission: %v", err)
	}
	if allowed {
		t.Fatal("allowed=true, want false")
	}
}
