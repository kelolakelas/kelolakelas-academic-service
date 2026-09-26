package grpcclient

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// hungIdentity never answers on its own: it only returns once the call's context ends,
// which is what a stalled identity looks like from academic.
func hungIdentity(ctx context.Context, _ *structpb.Struct) (*structpb.Struct, error) {
	<-ctx.Done()
	return nil, status.FromContextError(ctx.Err()).Err()
}

// delayedIdentity answers allowed after delay unless the call ends first.
func delayedIdentity(delay time.Duration, allowed bool) func(context.Context, *structpb.Struct) (*structpb.Struct, error) {
	return func(ctx context.Context, _ *structpb.Struct) (*structpb.Struct, error) {
		select {
		case <-time.After(delay):
			return structpb.NewStruct(map[string]interface{}{"allowed": allowed})
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
	}
}

// TestCheckPermissionTimesOutWhenIdentityNeverAnswers is the KEL-78 regression: without a
// deadline a silent identity held every guarded academic request open indefinitely.
func TestCheckPermissionTimesOutWhenIdentityNeverAnswers(t *testing.T) {
	const timeout = 100 * time.Millisecond
	client := newPermissionClientWithHandler(t, timeout, hungIdentity)

	started := time.Now()
	allowed, err := client.CheckPermission(context.Background(), testTenantID, testRoleID, testMemberID, "class:update")
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("CheckPermission returned no error while identity never answered")
	}
	if allowed {
		t.Fatal("allowed=true on timeout, want false")
	}
	if code := status.Code(err); code != codes.DeadlineExceeded {
		t.Fatalf("status code=%s, want %s (err=%v)", code, codes.DeadlineExceeded, err)
	}
	if elapsed < timeout {
		t.Fatalf("returned after %s, before the %s deadline", elapsed, timeout)
	}
	if elapsed > timeout+2*time.Second {
		t.Fatalf("returned after %s, far beyond the %s deadline", elapsed, timeout)
	}
}

// A request the HTTP client already abandoned must not wait for the permission deadline.
func TestCheckPermissionHonoursCallerCancellationBeforeDeadline(t *testing.T) {
	client := newPermissionClientWithHandler(t, 5*time.Second, hungIdentity)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	allowed, err := client.CheckPermission(ctx, testTenantID, testRoleID, testMemberID, "class:update")
	elapsed := time.Since(started)

	if err == nil || allowed {
		t.Fatalf("allowed=%t err=%v, want a cancellation error", allowed, err)
	}
	if code := status.Code(err); code != codes.Canceled {
		t.Fatalf("status code=%s, want %s (err=%v)", code, codes.Canceled, err)
	}
	if elapsed > time.Second {
		t.Fatalf("cancelled call took %s, want it to end well before the 5s deadline", elapsed)
	}
}

// A caller deadline shorter than the configured timeout still wins, because the permission
// deadline is derived from the caller's context rather than replacing it.
func TestCheckPermissionKeepsShorterCallerDeadline(t *testing.T) {
	client := newPermissionClientWithHandler(t, 5*time.Second, hungIdentity)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := client.CheckPermission(ctx, testTenantID, testRoleID, testMemberID, "class:update")
	if code := status.Code(err); code != codes.DeadlineExceeded {
		t.Fatalf("status code=%s, want %s (err=%v)", code, codes.DeadlineExceeded, err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("call took %s, want it bounded by the 50ms caller deadline", elapsed)
	}
}

// Around the boundary: an identity that answers inside the timeout keeps its decision, and
// one that answers after it is reported as an error rather than a late decision.
func TestCheckPermissionAroundTimeoutBoundary(t *testing.T) {
	cases := []struct {
		name        string
		timeout     time.Duration
		delay       time.Duration
		allowed     bool
		wantAllowed bool
		wantCode    codes.Code
	}{
		{name: "allow answered just inside the timeout", timeout: 500 * time.Millisecond, delay: 50 * time.Millisecond, allowed: true, wantAllowed: true, wantCode: codes.OK},
		{name: "deny answered just inside the timeout", timeout: 500 * time.Millisecond, delay: 50 * time.Millisecond, allowed: false, wantAllowed: false, wantCode: codes.OK},
		{name: "allow answered after the timeout", timeout: 50 * time.Millisecond, delay: 500 * time.Millisecond, allowed: true, wantAllowed: false, wantCode: codes.DeadlineExceeded},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newPermissionClientWithHandler(t, tc.timeout, delayedIdentity(tc.delay, tc.allowed))

			allowed, err := client.CheckPermission(context.Background(), testTenantID, testRoleID, testMemberID, "class:update")
			if code := status.Code(err); code != tc.wantCode {
				t.Fatalf("status code=%s, want %s (err=%v)", code, tc.wantCode, err)
			}
			if allowed != tc.wantAllowed {
				t.Fatalf("allowed=%t, want %t", allowed, tc.wantAllowed)
			}
		})
	}
}

// NewPermissionClient keeps the configured timeout, so main.go's value reaches every check.
func TestNewPermissionClientStoresTimeout(t *testing.T) {
	client, err := NewPermissionClient("passthrough:///identity", 1500*time.Millisecond)
	if err != nil {
		t.Fatalf("NewPermissionClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	concrete, ok := client.(*permissionClient)
	if !ok {
		t.Fatalf("client type=%T, want *permissionClient", client)
	}
	if concrete.timeout != 1500*time.Millisecond {
		t.Fatalf("timeout=%s, want 1.5s", concrete.timeout)
	}
}
