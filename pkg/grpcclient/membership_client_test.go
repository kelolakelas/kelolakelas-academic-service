package grpcclient

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

// membershipTestServer answers CheckActiveMembership with a canned verdict
// while recording the tenant/member identity received, following the same
// raw-structpb style as the permission client tests: the two services are
// separate Go modules that only share this wire contract.
type membershipTestServer struct {
	lastTenantID string
	lastMemberID string
	active       bool
	answer       map[string]interface{}
	err          error
}

func (s *membershipTestServer) check(req *structpb.Struct) (*structpb.Struct, error) {
	if s.err != nil {
		return nil, s.err
	}
	fields := req.GetFields()
	s.lastTenantID = fields["tenant_id"].GetStringValue()
	s.lastMemberID = fields["member_id"].GetStringValue()
	if s.answer != nil {
		return structpb.NewStruct(s.answer)
	}
	return structpb.NewStruct(map[string]interface{}{"active": s.active})
}

func newMembershipClientForTest(t *testing.T, timeout time.Duration, server *membershipTestServer) MembershipClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	grpcServer.RegisterService(&grpc.ServiceDesc{
		ServiceName: "tenant.MembershipService",
		HandlerType: (*interface{})(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "CheckActiveMembership",
			Handler: func(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
				req := new(structpb.Struct)
				if err := dec(req); err != nil {
					return nil, err
				}
				return server.check(req)
			},
		}},
	}, struct{}{})
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial fake membership: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &membershipClient{conn: conn, timeout: timeout}
}

// TestMembershipClientSendsTenantAndMember is the KEL-135 wire contract:
// academic names the calling tenant and the candidate member unchanged, so
// identity can pin the verdict to that membership row.
func TestMembershipClientSendsTenantAndMember(t *testing.T) {
	server := &membershipTestServer{active: true}
	client := newMembershipClientForTest(t, time.Second, server)
	active, err := client.CheckActiveMember(context.Background(), "tenant-1", "member-1")
	if err != nil || !active {
		t.Fatalf("active=%v err=%v want true", active, err)
	}
	if server.lastTenantID != "tenant-1" || server.lastMemberID != "member-1" {
		t.Fatalf("identity received tenant=%q member=%q", server.lastTenantID, server.lastMemberID)
	}
}

// TestMembershipClientParsesVerdict covers the inactive path: false is a
// valid answer, not an error, so the caller reports it as ineligible.
func TestMembershipClientParsesVerdict(t *testing.T) {
	server := &membershipTestServer{active: false}
	client := newMembershipClientForTest(t, time.Second, server)
	active, err := client.CheckActiveMember(context.Background(), "tenant-1", "member-1")
	if err != nil || active {
		t.Fatalf("active=%v err=%v want false,nil", active, err)
	}
}

// TestMembershipClientRejectsMalformedAnswers is the fail-closed proof: a
// missing or non-boolean "active" is an error, never an eligibility guess.
func TestMembershipClientRejectsMalformedAnswers(t *testing.T) {
	for name, answer := range map[string]map[string]interface{}{
		"missing active": {},
		"non-boolean":    {"active": "yes"},
	} {
		t.Run(name, func(t *testing.T) {
			server := &membershipTestServer{answer: answer}
			client := newMembershipClientForTest(t, time.Second, server)
			active, err := client.CheckActiveMember(context.Background(), "tenant-1", "member-1")
			if err == nil {
				t.Fatalf("active=%v want an error for %v", active, answer)
			}
			if active {
				t.Fatal("active=true on a malformed answer, want false")
			}
		})
	}
}

// TestMembershipClientTimesOutWhenIdentityHangs proves a slow identity turns
// into an error the caller reports as unavailable, instead of holding the
// substitution open.
func TestMembershipClientTimesOutWhenIdentityHangs(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	grpcServer.RegisterService(&grpc.ServiceDesc{
		ServiceName: "tenant.MembershipService",
		HandlerType: (*interface{})(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "CheckActiveMembership",
			Handler: func(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}},
	}, struct{}{})
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial fake membership: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := &membershipClient{conn: conn, timeout: 100 * time.Millisecond}
	started := time.Now()
	active, err := client.CheckActiveMember(context.Background(), "tenant-1", "member-1")
	if err == nil || active {
		t.Fatalf("active=%v err=%v want false + timeout error", active, err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("returned after %s, far beyond the deadline", elapsed)
	}
}
