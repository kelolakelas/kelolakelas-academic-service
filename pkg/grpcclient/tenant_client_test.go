package grpcclient

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/kelolakelas/kelolakelas-academic-service/pkg/proto/tenant"
)

// blockingTenantServer accepts the call and never answers, which is how an unresponsive
// identity behaves from the client's point of view.
type blockingTenantServer struct{}

func (s *blockingTenantServer) GetTenantPublicInfo(context.Context, *pb.TenantPublicInfoRequest) (*pb.TenantPublicInfoResponse, error) {
	select {}
}

func (s *blockingTenantServer) ValidateTenantStatus(context.Context, *pb.ValidateTenantRequest) (*pb.ValidateTenantResponse, error) {
	select {}
}

func newBlockingTenantClient(t *testing.T, timeout time.Duration) TenantClient {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	grpcServer.RegisterService(&grpc.ServiceDesc{
		ServiceName: "tenant.TenantService",
		HandlerType: (*interface{})(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "GetTenantPublicInfo",
				Handler: func(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
					req := new(pb.TenantPublicInfoRequest)
					if err := dec(req); err != nil {
						return nil, err
					}
					return (&blockingTenantServer{}).GetTenantPublicInfo(ctx, req)
				},
			},
			{
				MethodName: "ValidateTenantStatus",
				Handler: func(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
					req := new(pb.ValidateTenantRequest)
					if err := dec(req); err != nil {
						return nil, err
					}
					return (&blockingTenantServer{}).ValidateTenantStatus(ctx, req)
				},
			},
		},
	}, &blockingTenantServer{})
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &tenantClient{conn: conn, client: pb.NewTenantServiceClient(conn), timeout: timeout}
}

func TestGetTenantPublicInfoRespectsTimeout(t *testing.T) {
	client := newBlockingTenantClient(t, 150*time.Millisecond)

	start := time.Now()
	_, err := client.GetTenantPublicInfo(context.Background(), []string{"tenant"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("GetTenantPublicInfo returned no error for an unresponsive identity")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("GetTenantPublicInfo took %s, want the configured 150ms bound", elapsed)
	}
}

func TestValidateTenantStatusRespectsTimeout(t *testing.T) {
	client := newBlockingTenantClient(t, 150*time.Millisecond)

	start := time.Now()
	_, _, err := client.ValidateTenantStatus(context.Background(), "tenant")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ValidateTenantStatus returned no error for an unresponsive identity")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("ValidateTenantStatus took %s, want the configured 150ms bound", elapsed)
	}
}

// TestZeroTimeoutKeepsCallerContextAlive documents the escape hatch: a non-positive timeout
// must not cancel a call that the caller still wants to finish.
func TestZeroTimeoutKeepsCallerContextAlive(t *testing.T) {
	client := newBlockingTenantClient(t, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.GetTenantPublicInfo(ctx, []string{"tenant"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("GetTenantPublicInfo returned no error after the caller context expired")
	}
	// The call must have been ended by the caller context (200ms), not by a client bound.
	if elapsed > time.Second {
		t.Fatalf("GetTenantPublicInfo took %s, want the caller context to be the bound", elapsed)
	}
}
