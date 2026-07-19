package php_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// recordingGW captures the pool config the service asks the broker to write, so
// a test can assert on the rendered file without a database or a real broker.
type recordingGW struct {
	calls  []gwCall
	failOn string
	data   map[string]any
}

type gwCall struct {
	capability string
	input      map[string]any
}

func (g *recordingGW) Invoke(_ context.Context, capability string, input any) (map[string]any, error) {
	in, _ := input.(map[string]any)
	g.calls = append(g.calls, gwCall{capability: capability, input: in})
	if g.failOn == capability {
		return nil, errx.New(errx.KindUpstream, "boom", "simulated broker failure")
	}
	if g.data != nil {
		return g.data, nil
	}
	return map[string]any{"ok": true}, nil
}

func (g *recordingGW) Health(context.Context) error { return nil }

// lastConfig returns the most recent rendered pool file.
func (g *recordingGW) lastConfig(t *testing.T) string {
	t.Helper()
	for i := len(g.calls) - 1; i >= 0; i-- {
		if g.calls[i].capability != "php.write_pool" {
			continue
		}
		cfg, ok := g.calls[i].input["config"].(string)
		if !ok {
			t.Fatal("php.write_pool carried no config string")
		}
		return cfg
	}
	t.Fatal("php.write_pool was never called")
	return ""
}

func (g *recordingGW) callsTo(capability string) []gwCall {
	var out []gwCall
	for _, c := range g.calls {
		if c.capability == capability {
			out = append(out, c)
		}
	}
	return out
}

// fakePoolRepo is an in-memory php.PoolRepo.
type fakePoolRepo struct {
	rec     *php.PoolRecord
	upserts int
}

func (f *fakePoolRepo) Upsert(_ context.Context, r *php.PoolRecord) error {
	f.upserts++
	cp := *r
	f.rec = &cp
	return nil
}

func (f *fakePoolRepo) GetBySiteID(_ context.Context, _ int64) (*php.PoolRecord, error) {
	if f.rec == nil {
		return nil, errx.NotFound("php_pool_not_found", "No PHP pool for this site.")
	}
	cp := *f.rec
	return &cp, nil
}

func poolReq() php.PoolRequest {
	return php.PoolRequest{
		SiteID: 1, User: "hps1",
		Home: "/srv/heropanel/sites/1", DocumentRoot: "/srv/heropanel/sites/1/public",
	}
}

// renderWith applies settings shaped by mutate and returns the rendered pool.
func renderWith(t *testing.T, mutate func(*php.Settings)) string {
	t.Helper()
	gw := &recordingGW{}
	svc := php.NewService(&fakePoolRepo{}, gw)
	s := php.DefaultSettings()
	mutate(&s)
	if _, err := svc.ApplySettings(context.Background(), poolReq(), s); err != nil {
		t.Fatalf("ApplySettings: %v", err)
	}
	return gw.lastConfig(t)
}
