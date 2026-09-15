package grpcclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

const permissionServiceCheckPermissionPath = "/tenant.PermissionService/CheckPermission"

type PermissionClient interface {
	CheckPermission(ctx context.Context, roleID, permission string) (bool, error)
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

func (c *permissionClient) CheckPermission(ctx context.Context, roleID, permission string) (bool, error) {
	req, err := structpb.NewStruct(map[string]interface{}{"role_id": roleID, "permission": permission})
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
