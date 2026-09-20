package grpcclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

const permissionServiceCheckPermissionPath = "/tenant.PermissionService/CheckPermission"

type PermissionClient interface {
	// CheckPermission asks identity whether roleID grants permission while operating on
	// tenantID. The tenant is part of the question because identity only accepts a role that
	// belongs to that tenant or is a system role, so a role lifted from another tenant can
	// never satisfy the check.
	CheckPermission(ctx context.Context, tenantID, roleID, permission string) (bool, error)
	Close() error
}

type permissionClient struct {
	conn *grpc.ClientConn
}

func NewPermissionClient(target string) (PermissionClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &permissionClient{conn: conn}, nil
}

func (c *permissionClient) CheckPermission(ctx context.Context, tenantID, roleID, permission string) (bool, error) {
	// tenant_id is sent alongside the existing role_id and permission keys, so identity
	// deployments that only understand the older contract keep working during the ADR 0002
	// transition window.
	req, err := structpb.NewStruct(map[string]interface{}{
		"role_id":    roleID,
		"permission": permission,
		"tenant_id":  tenantID,
	})
	if err != nil {
		return false, err
	}
	resp := new(structpb.Struct)
	if err := c.conn.Invoke(ctx, permissionServiceCheckPermissionPath, req, resp, grpc.StaticMethod()); err != nil {
		return false, err
	}
	value, ok := resp.GetFields()["allowed"]
	return ok && value.GetBoolValue(), nil
}

func (c *permissionClient) Close() error { return c.conn.Close() }
