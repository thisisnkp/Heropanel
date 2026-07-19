package site_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// logGateway answers site.read_log with a canned payload, shaped like the
// broker envelope (numbers arrive as float64 through JSON).
type logGateway struct {
	calls   []gwCall
	content string
	exists  bool
	lines   int
}

func (g *logGateway) Invoke(_ context.Context, capability string, input any) (map[string]any, error) {
	in, _ := input.(map[string]any)
	g.calls = append(g.calls, gwCall{capability: capability, input: in})
	if capability != "site.read_log" {
		return map[string]any{"ok": true}, nil
	}
	return map[string]any{
		"content": g.content,
		"exists":  g.exists,
		"lines":   float64(g.lines),
	}, nil
}

func (g *logGateway) Health(context.Context) error { return nil }

func newSiteWithLogs(t *testing.T, gw *logGateway) (*site.Service, string) {
	t.Helper()
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: gw, Web: &fakeApplier{}})
	created, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return svc, created.UID
}

func TestLogsReadsTheAccessLogThroughTheBroker(t *testing.T) {
	gw := &logGateway{content: "127.0.0.1 - GET /\n", exists: true, lines: 1}
	svc, uid := newSiteWithLogs(t, gw)

	out, err := svc.Logs(context.Background(), uid, site.LogAccess, 50)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if out.Content != "127.0.0.1 - GET /\n" {
		t.Errorf("content = %q", out.Content)
	}
	if !out.Exists || out.Lines != 1 {
		t.Errorf("exists=%v lines=%d, want true/1", out.Exists, out.Lines)
	}

	var read *gwCall
	for i := range gw.calls {
		if gw.calls[i].capability == "site.read_log" {
			read = &gw.calls[i]
		}
	}
	if read == nil {
		t.Fatal("the broker was never asked to read the log")
	}
	// The logs are 0750 and owned by the site's user; hpd cannot read them itself.
	if read.input["root"] != "/srv/heropanel/sites/1" {
		t.Errorf("root = %v, want the site's home", read.input["root"])
	}
	if read.input["kind"] != site.LogAccess || read.input["lines"] != 50 {
		t.Errorf("kind/lines = %v/%v", read.input["kind"], read.input["lines"])
	}
}

// A log file that does not exist is the normal state of a site nobody has
// visited. Reporting it as an error would show a fault where there is none.
func TestLogsReportsAMissingFileAsAFactNotAnError(t *testing.T) {
	gw := &logGateway{content: "", exists: false, lines: 0}
	svc, uid := newSiteWithLogs(t, gw)

	out, err := svc.Logs(context.Background(), uid, site.LogError, 0)
	if err != nil {
		t.Fatalf("logs on a site with no log file: %v", err)
	}
	if out.Exists {
		t.Error("exists = true, want false")
	}
	if out.Content != "" {
		t.Errorf("content = %q, want empty", out.Content)
	}
}

func TestLogsDefaultsTheTailDepth(t *testing.T) {
	gw := &logGateway{exists: true}
	svc, uid := newSiteWithLogs(t, gw)

	if _, err := svc.Logs(context.Background(), uid, site.LogAccess, 0); err != nil {
		t.Fatalf("logs: %v", err)
	}
	for _, c := range gw.calls {
		if c.capability == "site.read_log" {
			if c.input["lines"] != site.DefaultLogLines {
				t.Errorf("lines = %v, want the default %d", c.input["lines"], site.DefaultLogLines)
			}
			return
		}
	}
	t.Fatal("site.read_log was not called")
}

// The kind is concatenated into a path broker-side. The edge rejects anything
// outside the allowlist too, so a bad request never costs a socket round-trip.
func TestLogsRejectsAnUnknownKind(t *testing.T) {
	gw := &logGateway{}
	svc, uid := newSiteWithLogs(t, gw)

	for _, kind := range []string{"", "syslog", "../../../etc/passwd", "access.log"} {
		if _, err := svc.Logs(context.Background(), uid, kind, 10); err == nil {
			t.Errorf("logs accepted kind %q", kind)
		}
	}
	if n := len(callsToLog(gw)); n != 0 {
		t.Errorf("the broker was called %d times for invalid kinds", n)
	}
}

func TestLogsRejectsAnOutOfRangeTailDepth(t *testing.T) {
	gw := &logGateway{}
	svc, uid := newSiteWithLogs(t, gw)

	for _, lines := range []int{-1, site.MaxLogLines + 1, 1 << 20} {
		if _, err := svc.Logs(context.Background(), uid, site.LogAccess, lines); err == nil {
			t.Errorf("logs accepted lines=%d", lines)
		}
	}
}

func TestLogsWithoutABrokerIsUnavailable(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})
	created, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A service constructed with no broker at all (reads still work, privileged
	// operations do not).
	noBroker := site.NewService(site.Deps{Repo: store})
	_, err = noBroker.Logs(context.Background(), created.UID, site.LogAccess, 10)
	if err == nil {
		t.Fatal("logs succeeded with no broker")
	}
	if !errx.IsKind(err, errx.KindUnavailable) {
		t.Errorf("error kind = %v, want unavailable", errx.KindOf(err))
	}
}

func callsToLog(g *logGateway) []gwCall {
	var out []gwCall
	for _, c := range g.calls {
		if c.capability == "site.read_log" {
			out = append(out, c)
		}
	}
	return out
}
