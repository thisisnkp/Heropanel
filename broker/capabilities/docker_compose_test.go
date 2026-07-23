package capabilities_test

import (
	"os"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

const sampleCompose = `services:
  web:
    image: ghost:5-alpine
    ports: ["2368:2368"]
`

// The compose file must never reach docker as a path hpd supplied — it is
// written to a directory the *broker* chooses, so hpd cannot point the broker at
// /etc/anything. The operator's YAML must also never appear in argv, where /proc
// would expose any secret in it.
func TestComposeFileGoesToABrokerChosenPathNotArgv(t *testing.T) {
	// The config probe reports one service so the override is non-empty; the up
	// call is what is inspected.
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if hasArg(c, "config") {
			return exec.Result{ExitCode: 0, Stdout: []byte("web\n")}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	if _, err := (capabilities.ComposeUp{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"project": "ghost", "file": sampleCompose})); err != nil {
		t.Fatalf("up: %v", err)
	}
	up, ok := lastMatching(fr, "up")
	if !ok {
		t.Fatal("no compose up command was run")
	}
	// The file is passed by path (compose has no stdin for a two-file merge), but
	// both paths sit under the OS temp dir — the broker's, not one hpd named.
	tmp := os.TempDir()
	files := 0
	for i, a := range up.Args {
		if a == "-f" && i+1 < len(up.Args) {
			files++
			if !strings.HasPrefix(up.Args[i+1], tmp) {
				t.Errorf("compose was given a path outside the broker's temp dir: %q", up.Args[i+1])
			}
		}
	}
	if files < 2 {
		t.Errorf("expected the operator file plus a label override, got %d -f args: %v", files, up.Args)
	}
	// The YAML must not appear in argv.
	for _, a := range up.Args {
		if strings.Contains(a, "ghost:5-alpine") {
			t.Errorf("the compose file leaked into argv: %v", up.Args)
		}
	}
}

// Every container a stack creates must carry the managed label, or the ownership
// boundary that guards single containers would not reach a stack's containers.
// The label is injected via a generated override, so the label injection itself
// is what is pinned here — the file plumbing is proven live in run-docker.sh.
func TestLabelOverrideStampsEveryService(t *testing.T) {
	override := capabilities.BuildLabelOverride("web\ndb\n", "site1")
	// Both services must be labelled, so a multi-service stack is fully managed.
	for _, svc := range []string{"web:", "db:"} {
		if !strings.Contains(override, svc) {
			t.Errorf("override does not label service %q:\n%s", svc, override)
		}
	}
	if strings.Count(override, capabilities.LabelManaged+": \"1\"") != 2 {
		t.Errorf("the managed label was not applied to both services:\n%s", override)
	}
	if !strings.Contains(override, capabilities.LabelSite+": site1") {
		t.Errorf("the site label was not applied:\n%s", override)
	}

	// With no site, no site label — but still managed.
	noSite := capabilities.BuildLabelOverride("web\n", "")
	if strings.Contains(noSite, capabilities.LabelSite) {
		t.Errorf("a site label was emitted with no site:\n%s", noSite)
	}
	if !strings.Contains(noSite, capabilities.LabelManaged) {
		t.Errorf("the managed label is required even without a site:\n%s", noSite)
	}
}

// -p scopes every operation to the project, so down/ps/logs act on this stack
// and not another.
func TestComposeScopesToTheProject(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if hasArg(c, "config") {
			return exec.Result{ExitCode: 0, Stdout: []byte("web\n")}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	if _, err := (capabilities.ComposeUp{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"project": "ghost", "file": sampleCompose})); err != nil {
		t.Fatalf("up: %v", err)
	}
	up, _ := lastMatching(fr, "up")
	if !strings.Contains(strings.Join(up.Args, " "), "-p ghost") {
		t.Errorf("stack was not scoped to a project: %v", up.Args)
	}
}

// hasArg reports whether the command contains an exact argument.
func hasArg(c exec.Command, want string) bool {
	for _, a := range c.Args {
		if a == want {
			return true
		}
	}
	return false
}

// lastMatching returns the most recent recorded command containing arg.
func lastMatching(fr *exec.FakeRunner, arg string) (exec.Command, bool) {
	for i := len(fr.Calls) - 1; i >= 0; i-- {
		if hasArg(fr.Calls[i], arg) {
			return fr.Calls[i], true
		}
	}
	return exec.Command{}, false
}

// A project name is an argv element in a privileged command. It cannot be a flag
// or a path.
func TestComposeProjectNameIsValidated(t *testing.T) {
	for _, bad := range []string{"--privileged", "-p", "../escape", "has/slash", ""} {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.ComposeUp{}).Execute(dockerCtx(fr),
			raw(t, map[string]any{"project": bad, "file": sampleCompose})); err == nil {
			t.Errorf("accepted %q as a project name", bad)
		}
		if len(fr.Calls) != 0 {
			t.Errorf("ran docker for an invalid project %q", bad)
		}
	}
}

func TestComposeUpRequiresAFile(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.ComposeUp{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"project": "ghost"})); errx.KindOf(err) != errx.KindValidation {
		t.Errorf("kind = %v, want Validation for a missing file", errx.KindOf(err))
	}
	if len(fr.Calls) != 0 {
		t.Error("ran compose up with no file")
	}
}

// Tear-down is guarded by ownership: a stack the panel did not create is not the
// panel's to remove. A project name alone is not proof — anyone can name a stack
// anything — so the check looks for the managed label on the stack's containers.
func TestComposeDownRefusesAStackThePanelDoesNotManage(t *testing.T) {
	// The ps ownership probe returns nothing → no managed containers → refuse.
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		return exec.Result{ExitCode: 0, Stdout: nil}, nil
	}}
	_, err := (capabilities.ComposeDown{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"project": "someone-elses-stack"}))
	if errx.KindOf(err) != errx.KindForbidden {
		t.Fatalf("kind = %v, want Forbidden", errx.KindOf(err))
	}
	for _, c := range fr.Calls {
		for _, a := range c.Args {
			if a == "down" {
				t.Fatal("tore down a stack the panel does not manage")
			}
		}
	}
}

func TestComposeDownActsWhenManaged(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		// The ownership probe (ps ... --format {{.ID}}) reports a container id.
		if len(c.Args) > 0 && c.Args[0] == "ps" {
			return exec.Result{ExitCode: 0, Stdout: []byte("abc123\n")}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	if _, err := (capabilities.ComposeDown{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"project": "ghost"})); err != nil {
		t.Fatalf("down: %v", err)
	}
	sawDown := false
	for _, c := range fr.Calls {
		for _, a := range c.Args {
			if a == "down" {
				sawDown = true
			}
		}
	}
	if !sawDown {
		t.Error("a managed stack was not torn down")
	}
}

// Down removes containers and networks but must never remove volumes: a stack's
// database keeps its data in a volume, and tearing the stack down is not the
// same act as destroying that data.
func TestComposeDownNeverRemovesVolumes(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if len(c.Args) > 0 && c.Args[0] == "ps" {
			return exec.Result{ExitCode: 0, Stdout: []byte("abc\n")}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	if _, err := (capabilities.ComposeDown{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"project": "ghost"})); err != nil {
		t.Fatalf("down: %v", err)
	}
	for _, c := range fr.Calls {
		for _, a := range c.Args {
			if a == "--volumes" || a == "-v" {
				t.Fatalf("compose down removed volumes: %v", c.Args)
			}
		}
	}
}

func TestComposeLogsAreClamped(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.ComposeLogs{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"project": "ghost", "tail": 999999})); err != nil {
		t.Fatalf("logs: %v", err)
	}
	last, _ := fr.Last()
	if !strings.Contains(strings.Join(last.Args, " "), "--tail 2000") {
		t.Errorf("compose log tail not clamped: %v", last.Args)
	}
}
