package grpcclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/tutorin-id/tutorin-academic-service/pkg/proto/tenant"
)

type TenantClient interface {
	ValidateTenantStatus(ctx context.Context, tenantID string) (bool, string, error)
	Close() error
}

type tenantClient struct {
	conn   *grpc.ClientConn
	client pb.TenantServiceClient
}

func NewTenantClient(target string) (TenantClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	client := pb.NewTenantServiceClient(conn)
	return &tenantClient{
		conn:   conn,
		client: client,
	}, nil
}

func (c *tenantClient) ValidateTenantStatus(ctx context.Context, tenantID string) (bool, string, error) {
	resp, err := c.client.ValidateTenantStatus(ctx, &pb.ValidateTenantRequest{TenantId: tenantID})
	if err != nil {
		return false, "", err
	}
	return resp.GetIsActive(), resp.GetMessage(), nil
}

func (c *tenantClient) Close() error {
	return c.conn.Close()
}
