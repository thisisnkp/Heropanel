package terminal

import (
	"context"
	"testing"

	brokerclient "github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/brokerwire"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// fakeStream is a no-op broker stream.
type fakeStream struct{ closed bool }

func (f *fakeStream) Send(brokerwire.StreamFrame) error     { return nil }
func (f *fakeStream) Recv() (brokerwire.StreamFrame, error) { return brokerwire.StreamFrame{}, nil }
func (f *fakeStream) Close() error                          { f.closed = true; return nil }

// fakeStreamGateway records what the service asked the broker to open.
type fakeStreamGateway struct {
	capability string
	input      map[string]any
	calls      int
	err        error
}

func (g *fakeStreamGateway) OpenStream(_ context.Context, capability string, input any) (brokerclient.Stream, error) {
	g.calls++
	g.capability = capability
	g.input, _ = input.(map[string]any)
	if g.err != nil {
		return nil, g.err
	}
	return &fakeStream{}, nil
}

type fakeSites struct {
	ref *SiteRef
	err error
}

func (s fakeSites) Resolve(context.Context, string) (*SiteRef, error) { return s.ref, s.err }

func provisionedRef() *SiteRef {
	return &SiteRef{ID: 1, UID: "site1", LinuxUser: "hps1", HomeDir: "/srv/heropanel/sites/1", DeployMode: "baremetal"}
}

func TestOpenSendsSiteIdentityAndSize(t *testing.T) {
	gw := &fakeStreamGateway{}
	svc := NewService(fakeSites{ref: provisionedRef()}, gw)

	_, ref, err := svc.Open(context.Background(), "site1", "public", 120, 40)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if gw.capability != Capability {
		t.Errorf("capability = %q, want %q", gw.capability, Capability)
	}
	// The Linux user is derived from the resolved site, never from the caller.
	if gw.input["username"] != "hps1" || gw.input["root"] != "/srv/heropanel/sites/1" {
		t.Errorf("payload identity = %v", gw.input)
	}
	if gw.input["cwd"] != "public" || gw.input["cols"] != uint16(120) || gw.input["rows"] != uint16(40) {
		t.Errorf("payload session = %v", gw.input)
	}
	if ref.LinuxUser != "hps1" {
		t.Errorf("returned ref = %+v", ref)
	}
}

func TestOpenDefaultsWindowSize(t *testing.T) {
	gw := &fakeStreamGateway{}
	svc := NewService(fakeSites{ref: provisionedRef()}, gw)

	if _, _, err := svc.Open(context.Background(), "site1", "", 0, 0); err != nil {
		t.Fatalf("open: %v", err)
	}
	if gw.input["cols"] != uint16(DefaultCols) || gw.input["rows"] != uint16(DefaultRows) {
		t.Errorf("want the default window size, got cols=%v rows=%v", gw.input["cols"], gw.input["rows"])
	}
}

// A site with no Linux account has nothing to attach a shell to; that must be a
// clean refusal, not a broker call that fails deep in the stack.
func TestOpenRefusesUnprovisionedSite(t *testing.T) {
	for _, ref := range []*SiteRef{
		{UID: "s", LinuxUser: "", HomeDir: "/srv/heropanel/sites/1"},
		{UID: "s", LinuxUser: "hps1", HomeDir: ""},
	} {
		gw := &fakeStreamGateway{}
		svc := NewService(fakeSites{ref: ref}, gw)
		if _, _, err := svc.Open(context.Background(), "site1", "", 80, 24); !errx.IsKind(err, errx.KindUnavailable) {
			t.Errorf("ref %+v: want unavailable, got %v", ref, err)
		}
		if gw.calls != 0 {
			t.Error("the broker must not be called for an unprovisioned site")
		}
	}
}

func TestOpenWithoutBrokerIsUnavailable(t *testing.T) {
	svc := NewService(fakeSites{ref: provisionedRef()}, nil)
	if svc.Available() {
		t.Error("Available() must be false without a broker")
	}
	if _, _, err := svc.Open(context.Background(), "site1", "", 80, 24); !errx.IsKind(err, errx.KindUnavailable) {
		t.Errorf("want unavailable without a broker, got %v", err)
	}
}

func TestOpenPropagatesResolveError(t *testing.T) {
	gw := &fakeStreamGateway{}
	svc := NewService(fakeSites{err: errx.NotFound("no_site", "no such site")}, gw)
	if _, _, err := svc.Open(context.Background(), "missing", "", 80, 24); !errx.IsKind(err, errx.KindNotFound) {
		t.Errorf("want the resolve error, got %v", err)
	}
	if gw.calls != 0 {
		t.Error("the broker must not be called when the site cannot be resolved")
	}
}

func TestOpenPropagatesBrokerError(t *testing.T) {
	gw := &fakeStreamGateway{err: errx.Forbidden("capability_disabled", "terminals are off")}
	svc := NewService(fakeSites{ref: provisionedRef()}, gw)
	if _, _, err := svc.Open(context.Background(), "site1", "", 80, 24); !errx.IsKind(err, errx.KindForbidden) {
		t.Errorf("a broker refusal should surface, got %v", err)
	}
}
