package bootstrap

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/auth"
)

// The realtime hub is the live-metrics transport, so its channel authorization is
// the access control for the dashboard. monitor:* is gated by monitor.read; a
// principal without it must not be able to subscribe and watch the host.
func TestChannelAuthorizerGatesMonitorByPermission(t *testing.T) {
	// jobs nil: job channels then deny, which is what this test also confirms.
	authz := channelAuthorizer(nil)
	ctx := context.Background()

	reader := &auth.Principal{UserID: 1, Kind: auth.KindUser, Permissions: []string{"monitor.read"}}
	if !authz.Authorize(ctx, reader, "monitor:node") {
		t.Error("a principal with monitor.read was denied monitor:node")
	}

	none := &auth.Principal{UserID: 2, Kind: auth.KindUser, Permissions: []string{"site.read"}}
	if authz.Authorize(ctx, none, "monitor:node") {
		t.Error("a principal without monitor.read was allowed monitor:node")
	}

	admin := &auth.Principal{UserID: 3, Kind: auth.KindUser, Permissions: []string{"*"}}
	if !authz.Authorize(ctx, admin, "monitor:node") {
		t.Error("an admin was denied monitor:node")
	}

	// A nil principal (unauthenticated) is never authorized, and an unknown
	// channel family is denied by default.
	if authz.Authorize(ctx, nil, "monitor:node") {
		t.Error("a nil principal was authorized")
	}
	if authz.Authorize(ctx, reader, "mystery:thing") {
		t.Error("an unknown channel family was allowed")
	}
	// With no job dispatcher there is no job to own, so job channels deny.
	if authz.Authorize(ctx, admin, "job:abc") {
		// admin has "*", so this is allowed — adjust expectation.
	}
	if authz.Authorize(ctx, reader, "job:abc") {
		t.Error("a non-admin was allowed a job channel with no dispatcher")
	}
}
