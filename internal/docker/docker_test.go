package docker_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/thisisnkp/heropanel/internal/docker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// fakeBroker answers capability invocations from a table, and records what was
// asked. It stands in for the privileged process, which is the only thing that
// ever touches the daemon.
type fakeBroker struct {
	calls    []string
	inputs   []any
	respond  map[string]map[string]any
	failWith error
}

func (f *fakeBroker) Invoke(_ context.Context, capability string, input any) (map[string]any, error) {
	f.calls = append(f.calls, capability)
	f.inputs = append(f.inputs, input)
	if f.failWith != nil {
		return nil, f.failWith
	}
	if r, ok := f.respond[capability]; ok {
		return r, nil
	}
	return map[string]any{}, nil
}

// available is a broker whose daemon is present.
func available(extra map[string]map[string]any) *fakeBroker {
	responses := map[string]map[string]any{
		"docker.info": {
			"available": true,
			"version": map[string]any{
				"Server": map[string]any{"Version": "27.1.1", "ApiVersion": "1.46"},
			},
		},
	}
	for k, v := range extra {
		responses[k] = v
	}
	return &fakeBroker{respond: responses}
}

func TestInfoReadsTheServerVersion(t *testing.T) {
	svc := docker.New(available(nil))
	info := svc.Info(context.Background())
	if !info.Available {
		t.Fatal("daemon reported unavailable")
	}
	if info.ServerVersion != "27.1.1" || info.APIVersion != "1.46" {
		t.Errorf("version = %q/%q, want 27.1.1/1.46", info.ServerVersion, info.APIVersion)
	}
}

// A host without Docker is a state, not an error — and the daemon's own reason
// must survive, because "not installed" and "permission denied" need different
// fixes and only the daemon can tell them apart.
func TestAbsentDaemonIsReportedNotThrown(t *testing.T) {
	b := &fakeBroker{respond: map[string]map[string]any{
		"docker.info": {"available": false, "reason": "Cannot connect to the Docker daemon"},
	}}
	svc := docker.New(b)
	info := svc.Info(context.Background())
	if info.Available {
		t.Fatal("reported available with no daemon")
	}
	if info.Reason == "" {
		t.Error("the daemon's reason was dropped")
	}

	// And every operation then fails with one clear error rather than whatever
	// docker happened to print.
	_, err := svc.ListContainers(context.Background(), "")
	if errx.KindOf(err) != errx.KindUnavailable {
		t.Errorf("kind = %v, want Unavailable", errx.KindOf(err))
	}
}

// Presence is asked on every page; it must not cost a privileged round trip
// each time.
func TestDaemonPresenceIsCached(t *testing.T) {
	b := available(nil)
	svc := docker.New(b)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		svc.Info(ctx)
	}
	probes := 0
	for _, c := range b.calls {
		if c == "docker.info" {
			probes++
		}
	}
	if probes != 1 {
		t.Errorf("probed the daemon %d times for 5 calls, want 1", probes)
	}
}

func TestListParsesDockerCLIRows(t *testing.T) {
	rows := []any{
		`{"ID":"abc123","Names":"/hp-site1-web","Image":"ghost:5","State":"running","Status":"Up 2 hours","Ports":"127.0.0.1:2368->2368/tcp","CreatedAt":"2026-07-20 10:00:00 +0000 UTC","Labels":"io.heropanel.managed=1,io.heropanel.site=site1"}`,
		`{"ID":"def456","Names":"postgres-prod","Image":"postgres:16","State":"running","Status":"Up 3 days","Labels":"com.example.team=data"}`,
	}
	svc := docker.New(available(map[string]map[string]any{
		"docker.container.list": {"containers": rows},
	}))

	got, err := svc.ListContainers(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed %d containers, want 2", len(got))
	}

	// The panel's own container: named without the API's leading slash, and
	// recognised as managed and attributed to its site.
	if got[0].Name != "hp-site1-web" {
		t.Errorf("name = %q, want the leading slash stripped", got[0].Name)
	}
	if !got[0].Managed || got[0].SiteUID != "site1" {
		t.Errorf("managed=%v site=%q, want true/site1", got[0].Managed, got[0].SiteUID)
	}

	// Someone else's container is listed — hiding it would make the panel lie
	// about the host — but never marked managed, which is what gates mutation.
	if got[1].Managed {
		t.Error("an unlabelled container was reported as panel-managed")
	}
	if got[1].Name != "postgres-prod" {
		t.Errorf("name = %q", got[1].Name)
	}
}

// A malformed row must not take the whole listing down: one container with
// output the panel cannot parse should cost that row, not the page.
func TestOneBadRowDoesNotLoseTheListing(t *testing.T) {
	svc := docker.New(available(map[string]map[string]any{
		"docker.container.list": {"containers": []any{
			`{"ID":"ok","Names":"good"}`,
			`{not json at all`,
		}},
	}))
	got, err := svc.ListContainers(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Name != "good" {
		t.Errorf("got %+v, want just the parseable row", got)
	}
}

func TestListScopesToASite(t *testing.T) {
	b := available(map[string]map[string]any{"docker.container.list": {"containers": []any{}}})
	svc := docker.New(b)
	if _, err := svc.ListContainers(context.Background(), "site7"); err != nil {
		t.Fatalf("list: %v", err)
	}
	for i, c := range b.calls {
		if c != "docker.container.list" {
			continue
		}
		in, _ := b.inputs[i].(map[string]any)
		if in["site"] != "site7" {
			t.Errorf("site filter = %v, want site7", in["site"])
		}
		// Stopped containers are the ones an operator is looking for.
		if in["all"] != true {
			t.Error("listing excluded stopped containers")
		}
	}
}

func TestImagesAreParsed(t *testing.T) {
	svc := docker.New(available(map[string]map[string]any{
		"docker.image.list": {"images": []any{
			`{"ID":"sha256:aaa","Repository":"ghost","Tag":"5-alpine","Size":"438MB","CreatedSince":"3 weeks ago"}`,
		}},
	}))
	got, err := svc.ListImages(context.Background())
	if err != nil {
		t.Fatalf("images: %v", err)
	}
	if len(got) != 1 || got[0].Repository != "ghost" || got[0].Tag != "5-alpine" || got[0].Size != "438MB" {
		t.Errorf("got %+v", got)
	}
}

func TestStatsAreParsed(t *testing.T) {
	svc := docker.New(available(map[string]map[string]any{
		"docker.container.stats": {"stats": []any{
			`{"ID":"abc","Name":"hp-site1-web","CPUPerc":"0.42%","MemUsage":"120MiB / 2GiB","MemPerc":"5.86%","NetIO":"1.2kB / 0B","BlockIO":"0B / 0B","PIDs":"7"}`,
		}},
	}))
	got, err := svc.Stats(context.Background(), "")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(got) != 1 || got[0].CPUPerc != "0.42%" || got[0].PIDs != "7" {
		t.Errorf("got %+v", got)
	}
}

// The service maps a verb to a capability; an unknown verb must be refused here
// rather than reaching the broker as a made-up capability name.
func TestUnknownActionNeverReachesTheBroker(t *testing.T) {
	b := available(nil)
	svc := docker.New(b)
	err := svc.Action(context.Background(), "destroy", "hp-site1-web", false)
	if errx.KindOf(err) != errx.KindValidation {
		t.Fatalf("kind = %v, want Validation", errx.KindOf(err))
	}
	for _, c := range b.calls {
		if c != "docker.info" {
			t.Errorf("an unknown action invoked %q", c)
		}
	}
}

func TestActionsMapToCapabilities(t *testing.T) {
	for verb, want := range map[string]string{
		"start":   "docker.container.start",
		"stop":    "docker.container.stop",
		"restart": "docker.container.restart",
		"remove":  "docker.container.remove",
	} {
		b := available(nil)
		svc := docker.New(b)
		if err := svc.Action(context.Background(), verb, "hp-site1-web", false); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
		found := false
		for _, c := range b.calls {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s invoked %v, want %s", verb, b.calls, want)
		}
	}
}

// The service must not second-guess the broker's refusal by reporting success:
// ownership is enforced in the privileged process and its error is the answer.
func TestBrokerRefusalIsPropagated(t *testing.T) {
	b := available(nil)
	svc := docker.New(b)
	// Prime the daemon probe first: a broker that fails *everything* would fail
	// the presence check instead, and the test would pass for the wrong reason.
	svc.Info(context.Background())
	b.failWith = errx.New(errx.KindForbidden, "container_not_managed", "not ours")

	err := svc.Action(context.Background(), "stop", "postgres-prod", false)
	if err == nil {
		t.Fatal("a refused action reported success")
	}
	if errx.KindOf(err) != errx.KindForbidden {
		t.Errorf("kind = %v, want Forbidden", errx.KindOf(err))
	}
}

func TestInspectPassesDockersPayloadThrough(t *testing.T) {
	svc := docker.New(available(map[string]map[string]any{
		"docker.container.inspect": {"container": map[string]any{"Id": "abc", "State": map[string]any{"Running": true}}},
	}))
	raw, err := svc.Inspect(context.Background(), "hp-site1-web")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("inspect returned unparseable JSON: %v", err)
	}
	if decoded["Id"] != "abc" {
		t.Errorf("payload = %v, want docker's own fields untouched", decoded)
	}
}

func TestNilBrokerDisablesTheModule(t *testing.T) {
	if svc := docker.New(nil); svc != nil {
		t.Error("a panel with no broker should have no Docker service")
	}
}

func TestClampTail(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{0, 2000}, {-1, 2000}, {50, 50}, {100000, 2000}} {
		if got := docker.ClampTail(tc.in); got != tc.want {
			t.Errorf("ClampTail(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestBrokerTransportErrorSurfacesAsUnavailable(t *testing.T) {
	b := &fakeBroker{failWith: errors.New("dial unix /run/heropanel/broker.sock: connection refused")}
	svc := docker.New(b)
	info := svc.Info(context.Background())
	if info.Available {
		t.Fatal("an unreachable broker was reported as a working daemon")
	}
	if info.Reason == "" {
		t.Error("the transport error was swallowed")
	}
}
