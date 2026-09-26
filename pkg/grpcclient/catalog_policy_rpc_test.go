package grpcclient

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
	"net"
	"testing"
	"time"
)

// The server registers the same structpb unary method name and response
// fields as identity's CatalogPolicyService (no generated .proto in either
// repository). This catches drift in the cross-repo wire contract.
type catalogPolicyRPCServer interface {
	GetPublicCatalogPolicy(context.Context, *structpb.Struct) (*structpb.Struct, error)
}
type policyRPCFixture struct{ response *structpb.Struct }

func (s *policyRPCFixture) GetPublicCatalogPolicy(context.Context, *structpb.Struct) (*structpb.Struct, error) {
	return s.response, nil
}
func TestCatalogPolicyClientWireContract(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	fixture := &policyRPCFixture{response: mustCatalogPolicyStruct(t, map[string]interface{}{"open": false, "applied_version": 3, "desired_version": 4})}
	server.RegisterService(&grpc.ServiceDesc{ServiceName: "tenant.CatalogPolicyService", HandlerType: (*catalogPolicyRPCServer)(nil), Methods: []grpc.MethodDesc{{MethodName: "GetPublicCatalogPolicy", Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
		req := new(structpb.Struct)
		if err := dec(req); err != nil {
			return nil, err
		}
		return srv.(catalogPolicyRPCServer).GetPublicCatalogPolicy(ctx, req)
	}}}}, fixture)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	client, err := NewCatalogPolicyClient(listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	policy, err := client.GetPublicCatalogPolicy(context.Background())
	if err != nil || policy.Open || policy.AppliedVersion != 3 || policy.DesiredVersion != 4 {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}
}
func mustCatalogPolicyStruct(t *testing.T, fields map[string]interface{}) *structpb.Struct {
	t.Helper()
	value, err := structpb.NewStruct(fields)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
