package capabilities_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// createOK builds a minimal valid create request, which each test then spoils in
// exactly one way.
func createOK(extra map[string]any) map[string]any {
	in := map[string]any{"name": "hp-site1-web", "image": "ghost:5-alpine"}
	for k, v := range extra {
		in[k] = v
	}
	return in
}

func mustCreate(t *testing.T, in map[string]any) exec.Command {
	t.Helper()
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.ContainerCreate{}).Execute(dockerCtx(fr), raw(t, in)); err != nil {
		t.Fatalf("create: %v", err)
	}
	last, ok := fr.Last()
	if !ok {
		t.Fatal("create ran no command")
	}
	return last
}

// A bind mount of a host path inside a container is a complete escape to host
// root — `-v /:/host`, or the docker socket itself. Only *named volumes* may be
// mounted, and the name pattern admits no "/" at all, so a host path is not
// merely rejected: it is unrepresentable.
func TestHostPathsCanNeverBeMounted(t *testing.T) {
	for _, bad := range []string{
		"/", "/etc", "/var/run/docker.sock", "./relative", "../escape",
		"/var/lib/heropanel", "vol/../../etc",
	} {
		t.Run(bad, func(t *testing.T) {
			fr := &exec.FakeRunner{}
			_, err := (capabilities.ContainerCreate{}).Execute(dockerCtx(fr), raw(t, createOK(map[string]any{
				"volumes": []map[string]any{{"volume": bad, "path": "/data"}},
			})))
			if err == nil {
				t.Fatalf("accepted %q as a volume — that is a host bind mount", bad)
			}
			if errx.KindOf(err) != errx.KindValidation {
				t.Errorf("kind = %v, want Validation", errx.KindOf(err))
			}
			if len(fr.Calls) != 0 {
				t.Errorf("ran docker anyway: %v", fr.Calls)
			}
		})
	}
}

func TestNamedVolumesAreMounted(t *testing.T) {
	cmd := mustCreate(t, createOK(map[string]any{
		"volumes": []map[string]any{
			{"volume": "ghost-content", "path": "/var/lib/ghost/content"},
			{"volume": "ghost-config", "path": "/etc/ghost", "read_only": true},
		},
	}))
	argv := strings.Join(cmd.Args, " ")
	if !strings.Contains(argv, "--volume ghost-content:/var/lib/ghost/content") {
		t.Errorf("named volume not mounted: %v", cmd.Args)
	}
	if !strings.Contains(argv, "--volume ghost-config:/etc/ghost:ro") {
		t.Errorf("read-only mount not marked ro: %v", cmd.Args)
	}
}

// Docker writes its own firewall rules ahead of the host's, so a container
// published on 0.0.0.0 is reachable from the internet even when the host
// firewall denies that port. Every publish must be loopback, and the caller
// must have no way to say otherwise.
func TestPublishedPortsAreAlwaysBoundToLoopback(t *testing.T) {
	cmd := mustCreate(t, createOK(map[string]any{
		"ports": []map[string]any{{"host": 2368, "container": 2368}},
	}))
	argv := strings.Join(cmd.Args, " ")
	if !strings.Contains(argv, "--publish 127.0.0.1:2368:2368/tcp") {
		t.Fatalf("port was not bound to loopback: %v", cmd.Args)
	}
	for _, a := range cmd.Args {
		if strings.Contains(a, "0.0.0.0") || strings.Contains(a, "::") {
			t.Errorf("a port was published on all interfaces: %q", a)
		}
	}
}

func TestPortsAreRangeChecked(t *testing.T) {
	for _, p := range []map[string]any{
		{"host": 0, "container": 80}, {"host": 70000, "container": 80},
		{"host": 80, "container": -1},
	} {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.ContainerCreate{}).Execute(dockerCtx(fr),
			raw(t, createOK(map[string]any{"ports": []map[string]any{p}}))); err == nil {
			t.Errorf("accepted out-of-range port %v", p)
		}
	}
}

// The container the panel creates must carry the label the panel later checks
// before touching it. Without this, nothing the panel creates is ever
// manageable — the ownership guard would make the whole module read-only.
func TestCreatedContainersAreLabelledAsManaged(t *testing.T) {
	cmd := mustCreate(t, createOK(map[string]any{"site": "site1"}))
	argv := strings.Join(cmd.Args, " ")
	if !strings.Contains(argv, capabilities.LabelManaged+"=1") {
		t.Errorf("the created container carries no managed label: %v", cmd.Args)
	}
	if !strings.Contains(argv, capabilities.LabelSite+"=site1") {
		t.Errorf("the created container is not attributed to its site: %v", cmd.Args)
	}
}

// A setuid binary inside someone else's image must not be able to gain more
// than the container started with.
func TestNoNewPrivilegesIsAlwaysSet(t *testing.T) {
	cmd := mustCreate(t, createOK(nil))
	if !strings.Contains(strings.Join(cmd.Args, " "), "no-new-privileges") {
		t.Errorf("no-new-privileges was not applied: %v", cmd.Args)
	}
}

// Nothing in the input maps to these, so they cannot appear whatever is asked.
// The assertion is on the produced argv rather than on a rejection, because the
// design claim is "unrepresentable", not "filtered".
func TestEscapeFlagsAreNeverProduced(t *testing.T) {
	cmd := mustCreate(t, createOK(map[string]any{
		"env":     map[string]string{"X": "--privileged"},
		"command": []string{"--privileged", "--pid=host"},
		"ports":   []map[string]any{{"host": 8080, "container": 80}},
		"volumes": []map[string]any{{"volume": "data", "path": "/data"}},
	}))
	// Everything after the image operand belongs to the container, so the search
	// stops there — that is where docker itself stops parsing its own options.
	imageAt := -1
	for i, a := range cmd.Args {
		if a == "ghost:5-alpine" {
			imageAt = i
			break
		}
	}
	if imageAt < 0 {
		t.Fatalf("the image operand is missing: %v", cmd.Args)
	}
	for _, forbidden := range []string{
		"--privileged", "--cap-add", "--device", "--userns", "--pid", "--ipc", "--uts", "--network=host",
	} {
		for _, a := range cmd.Args[:imageAt] {
			if a == forbidden || strings.HasPrefix(a, forbidden+"=") {
				t.Errorf("docker was given %q: %v", forbidden, cmd.Args)
			}
		}
	}
}

// Environment values are where a generated database password lives. argv is
// world-readable through /proc, so they must travel by stdin — and the bytes
// must not appear in the arguments at all.
func TestEnvironmentNeverReachesArgv(t *testing.T) {
	cmd := mustCreate(t, createOK(map[string]any{
		"env": map[string]string{"DB_PASSWORD": "hunter2SuperSecret", "SITE_URL": "https://x.test"},
	}))
	for _, a := range cmd.Args {
		if strings.Contains(a, "hunter2SuperSecret") {
			t.Fatalf("a secret leaked into argv: %v", cmd.Args)
		}
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "--env-file /dev/stdin") {
		t.Errorf("environment was not passed as an env-file: %v", cmd.Args)
	}
	if !strings.Contains(string(cmd.Stdin), "DB_PASSWORD=hunter2SuperSecret\n") {
		t.Errorf("the env-file does not carry the value: %q", cmd.Stdin)
	}
	// Sorted, so the same request always produces the same bytes.
	if !strings.HasPrefix(string(cmd.Stdin), "DB_PASSWORD=") {
		t.Errorf("env-file is not in a stable order: %q", cmd.Stdin)
	}
}

// A newline in a value would let one variable forge another line of the
// env-file — injection through a different door than argv.
func TestEnvValuesCannotForgeExtraLines(t *testing.T) {
	fr := &exec.FakeRunner{}
	_, err := (capabilities.ContainerCreate{}).Execute(dockerCtx(fr), raw(t, createOK(map[string]any{
		"env": map[string]string{"A": "ok\nB=injected"},
	})))
	if err == nil {
		t.Fatal("accepted a newline inside an environment value")
	}
	if len(fr.Calls) != 0 {
		t.Error("ran docker with a forged env-file")
	}
}

func TestEnvKeysAreValidated(t *testing.T) {
	for _, bad := range []string{"9BAD", "has space", "has=equals", ""} {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.ContainerCreate{}).Execute(dockerCtx(fr),
			raw(t, createOK(map[string]any{"env": map[string]string{bad: "v"}}))); err == nil {
			t.Errorf("accepted %q as an environment variable name", bad)
		}
	}
}

func TestRestartPolicyIsAnAllowlistWithASaneDefault(t *testing.T) {
	cmd := mustCreate(t, createOK(nil))
	if !strings.Contains(strings.Join(cmd.Args, " "), "--restart unless-stopped") {
		t.Errorf("default restart policy = %v, want unless-stopped", cmd.Args)
	}
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.ContainerCreate{}).Execute(dockerCtx(fr),
		raw(t, createOK(map[string]any{"restart": "on-boot; rm -rf /"}))); err == nil {
		t.Error("accepted an invented restart policy")
	}
}

func TestMemoryLimitIsBounded(t *testing.T) {
	cmd := mustCreate(t, createOK(map[string]any{"memory_mb": 512}))
	if !strings.Contains(strings.Join(cmd.Args, " "), "--memory 512m") {
		t.Errorf("memory limit not applied: %v", cmd.Args)
	}
	for _, bad := range []int{1, 15, 2 * 1024 * 1024} {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.ContainerCreate{}).Execute(dockerCtx(fr),
			raw(t, createOK(map[string]any{"memory_mb": bad}))); err == nil {
			t.Errorf("accepted memory_mb=%d", bad)
		}
	}
}

// Volumes and networks carry the same ownership boundary as containers, and for
// volumes it is sharper: removing one destroys data, and a volume the panel did
// not create usually belongs to a database.
func TestVolumeAndNetworkRemovalRefuseWhatThePanelDoesNotOwn(t *testing.T) {
	unmanaged := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if len(c.Args) > 1 && c.Args[1] == "inspect" {
			return exec.Result{ExitCode: 0, Stdout: []byte("<no value>\n")}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}

	if _, err := (capabilities.VolumeRemove{}).Execute(dockerCtx(unmanaged),
		raw(t, map[string]any{"name": "pgdata"})); errx.KindOf(err) != errx.KindForbidden {
		t.Errorf("volume remove kind = %v, want Forbidden", errx.KindOf(err))
	}
	for _, c := range unmanaged.Calls {
		if len(c.Args) > 1 && c.Args[1] == "rm" {
			t.Fatal("removed a volume the panel does not manage — that destroys someone else's data")
		}
	}

	net := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if len(c.Args) > 1 && c.Args[1] == "inspect" {
			return exec.Result{ExitCode: 0, Stdout: []byte("<no value>\n")}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	if _, err := (capabilities.NetworkRemove{}).Execute(dockerCtx(net),
		raw(t, map[string]any{"name": "bridge"})); errx.KindOf(err) != errx.KindForbidden {
		t.Errorf("network remove kind = %v, want Forbidden", errx.KindOf(err))
	}
}

// Inspecting a volume is read-only, so — unlike remove — it does NOT gate on the
// managed label; and it must return the volume's consumers, which come from a
// separate `ps` filtered by the volume.
func TestVolumeInspectIsUnguardedAndReportsConsumers(t *testing.T) {
	sawPS := false
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if len(c.Args) > 1 && c.Args[0] == "volume" && c.Args[1] == "inspect" {
			return exec.Result{ExitCode: 0, Stdout: []byte(`{"Name":"pgdata","Driver":"local"}` + "\n")}, nil
		}
		if len(c.Args) > 0 && c.Args[0] == "ps" {
			sawPS = true
			// The filter must scope to this volume, or the consumer list is a lie.
			if !strings.Contains(strings.Join(c.Args, " "), "volume=pgdata") {
				t.Errorf("consumer lookup not filtered by the volume: %v", c.Args)
			}
			return exec.Result{ExitCode: 0, Stdout: []byte(`{"Names":"pg","Image":"postgres:16","State":"running"}` + "\n")}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	res, err := (capabilities.VolumeInspect{}).Execute(dockerCtx(fr), raw(t, map[string]any{"name": "pgdata"}))
	if err != nil {
		t.Fatalf("volume inspect: %v", err)
	}
	// No ownership inspect (`--format {{index .Labels ...}}`) should have run — a
	// read is not gated. Every inspect here uses --format json.
	for _, c := range fr.Calls {
		if len(c.Args) > 1 && c.Args[1] == "inspect" && strings.Contains(strings.Join(c.Args, " "), "Labels") {
			t.Fatal("volume inspect ran an ownership check; reading must not be gated")
		}
	}
	if !sawPS {
		t.Fatal("volume inspect did not look up consumers")
	}
	if got := len(res.Data["consumers"].([]json.RawMessage)); got != 1 {
		t.Errorf("parsed %d consumers, want 1", got)
	}
}

// Network inspect is likewise read-only and unguarded, and passes docker's own
// payload (which already carries connected containers) straight through.
func TestNetworkInspectIsUnguarded(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if len(c.Args) > 1 && c.Args[0] == "network" && c.Args[1] == "inspect" {
			return exec.Result{ExitCode: 0, Stdout: []byte(`{"Name":"bridge","Containers":{}}` + "\n")}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	if _, err := (capabilities.NetworkInspect{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"name": "bridge"})); err != nil {
		t.Fatalf("network inspect: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("network inspect made %d calls, want just the inspect", len(fr.Calls))
	}
}

func TestVolumeAndNetworkCreationAreLabelled(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.VolumeCreate{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"name": "ghost-content", "site": "site1"})); err != nil {
		t.Fatalf("volume create: %v", err)
	}
	last, _ := fr.Last()
	if !strings.Contains(strings.Join(last.Args, " "), capabilities.LabelManaged+"=1") {
		t.Errorf("created volume is unlabelled: %v", last.Args)
	}

	fr2 := &exec.FakeRunner{}
	if _, err := (capabilities.NetworkCreate{}).Execute(dockerCtx(fr2),
		raw(t, map[string]any{"name": "ghost-net"})); err != nil {
		t.Fatalf("network create: %v", err)
	}
	net, _ := fr2.Last()
	argv := strings.Join(net.Args, " ")
	if !strings.Contains(argv, capabilities.LabelManaged+"=1") {
		t.Errorf("created network is unlabelled: %v", net.Args)
	}
	// Always a bridge: a container on the host network shares the host's stack
	// outright, discarding the isolation that made containerising it worthwhile.
	if !strings.Contains(argv, "--driver bridge") {
		t.Errorf("network was not created as a bridge: %v", net.Args)
	}
}
