package capabilities_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// managedRunner answers the ownership inspect with `managed`, and everything
// else with success. It is how a test says "the daemon considers this container
// panel-managed (or not)" without a daemon.
func managedRunner(managed bool) *exec.FakeRunner {
	label := "0"
	if managed {
		label = "1"
	}
	return &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if len(c.Args) > 0 && c.Args[0] == "inspect" {
			return exec.Result{ExitCode: 0, Stdout: []byte(label + "\n")}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}
}

func dockerCtx(r exec.Runner) capability.Context { return gitCtx(r) }

// This is the boundary the whole Docker module rests on. Without it,
// `docker.container.stop` stops anything on the host — the site database, a CI
// runner, the monitoring agent — because the caller chooses the name.
func TestLifecycleRefusesAContainerThePanelDoesNotManage(t *testing.T) {
	for _, tc := range []struct {
		name string
		cap  capability.Capability
	}{
		{"start", capabilities.ContainerStart{}},
		{"stop", capabilities.ContainerStop{}},
		{"restart", capabilities.ContainerRestart{}},
		{"remove", capabilities.ContainerRemove{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fr := managedRunner(false)
			_, err := tc.cap.Execute(dockerCtx(fr), raw(t, map[string]any{"container": "postgres-prod"}))
			if err == nil {
				t.Fatalf("%s acted on an unmanaged container", tc.name)
			}
			if errx.KindOf(err) != errx.KindForbidden {
				t.Errorf("kind = %v, want Forbidden", errx.KindOf(err))
			}
			// Refusal must happen *before* the action: an inspect is expected,
			// a second command is not.
			for _, c := range fr.Calls {
				if len(c.Args) > 0 && c.Args[0] != "inspect" {
					t.Fatalf("ran %q on an unmanaged container", argvOf(c))
				}
			}
		})
	}
}

func TestLifecycleActsOnAManagedContainer(t *testing.T) {
	fr := managedRunner(true)
	if _, err := (capabilities.ContainerStop{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"container": "hp-site1-web"})); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(fr.Calls) != 2 {
		t.Fatalf("made %d calls, want inspect + stop", len(fr.Calls))
	}
	last, _ := fr.Last()
	if last.Path != "/usr/bin/docker" {
		t.Errorf("path = %q, want the docker CLI", last.Path)
	}
	if last.Args[0] != "stop" || last.Args[len(last.Args)-1] != "hp-site1-web" {
		t.Errorf("args = %v, want a stop of the named container", last.Args)
	}
}

// An argv array defeats *shell* injection but not flag injection: docker parses
// a leading "-" as an option wherever the value lands. A container literally
// named "--privileged" must be unrepresentable, not merely quoted.
func TestValuesThatWouldBeReadAsFlagsAreRefused(t *testing.T) {
	flagish := []string{"--privileged", "-v=/:/host", "--network=host", "-it"}
	for _, bad := range flagish {
		t.Run(bad, func(t *testing.T) {
			fr := managedRunner(true)
			_, err := (capabilities.ContainerStop{}).Execute(dockerCtx(fr),
				raw(t, map[string]any{"container": bad}))
			if err == nil {
				t.Fatalf("accepted %q as a container name", bad)
			}
			if errx.KindOf(err) != errx.KindValidation {
				t.Errorf("kind = %v, want Validation", errx.KindOf(err))
			}
			if len(fr.Calls) != 0 {
				t.Errorf("ran %d commands for a rejected name", len(fr.Calls))
			}
		})
	}

	for _, bad := range []string{"-v=/:/host", "--privileged", "-"} {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.ImagePull{}).Execute(dockerCtx(fr),
			raw(t, map[string]any{"image": bad})); err == nil {
			t.Errorf("pull accepted %q as an image reference", bad)
		}
	}
}

func TestImagePullTerminatesOptionParsing(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.ImagePull{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"image": "ghost:5-alpine"})); err != nil {
		t.Fatalf("pull: %v", err)
	}
	last, _ := fr.Last()
	// "--" before the operand, belt-and-braces with the pattern check.
	if len(last.Args) < 3 || last.Args[0] != "pull" || last.Args[1] != "--" || last.Args[2] != "ghost:5-alpine" {
		t.Errorf("args = %v, want [pull -- ghost:5-alpine]", last.Args)
	}
}

// A missing daemon is a state the UI renders, not a failure. If this returned an
// error, every panel on a host without Docker would look broken.
func TestInfoReportsAnAbsentDaemonWithoutFailing(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{ExitCode: 1,
		Stderr: []byte("Cannot connect to the Docker daemon at unix:///var/run/docker.sock.")}}
	res, err := (capabilities.DockerInfo{}).Execute(dockerCtx(fr), nil)
	if err != nil {
		t.Fatalf("info returned an error for an absent daemon: %v", err)
	}
	data := res.Data
	if data["available"] != false {
		t.Errorf("available = %v, want false", data["available"])
	}
	if !strings.Contains(data["reason"].(string), "Cannot connect") {
		t.Errorf("reason = %q, want the daemon's own message", data["reason"])
	}
}

// Reading is deliberately not restricted to managed containers: an admin whose
// host is out of memory needs to see the container eating it even if the panel
// did not start it.
func TestListShowsEveryContainerButCanScopeToASite(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{Stdout: []byte(`{"Names":"a"}` + "\n" + `{"Names":"b"}` + "\n")}}
	res, err := (capabilities.ContainerList{}).Execute(dockerCtx(fr), raw(t, map[string]any{"all": true}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	last, _ := fr.Last()
	if strings.Contains(argvOf(last), capabilities.LabelManaged) {
		t.Errorf("list filtered to managed containers only: %v", last.Args)
	}
	data := res.Data
	if got := len(data["containers"].([]json.RawMessage)); got != 2 {
		t.Errorf("parsed %d containers, want 2", got)
	}

	fr2 := &exec.FakeRunner{}
	if _, err := (capabilities.ContainerList{}).Execute(dockerCtx(fr2),
		raw(t, map[string]any{"site": "site1"})); err != nil {
		t.Fatalf("scoped list: %v", err)
	}
	scoped, _ := fr2.Last()
	if !strings.Contains(argvOf(scoped), "label="+capabilities.LabelSite+"=site1") {
		t.Errorf("site scoping did not filter by label: %v", scoped.Args)
	}
}

// Empty docker output must be an empty list, not a list holding one empty item —
// otherwise the UI renders a phantom row for a host with no containers.
func TestEmptyOutputIsAnEmptyList(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{Stdout: []byte("\n")}}
	res, err := (capabilities.ContainerList{}).Execute(dockerCtx(fr), nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	data := res.Data
	if got := len(data["containers"].([]json.RawMessage)); got != 0 {
		t.Errorf("parsed %d containers from empty output, want 0", got)
	}
}

// An unbounded tail is how a log viewer becomes a way to exhaust the broker: the
// wire frame caps at 1 MiB and a busy container has gigabytes of history.
func TestLogTailIsClampedRegardlessOfWhatIsAsked(t *testing.T) {
	for _, ask := range []int{0, -5, 1_000_000} {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.ContainerLogs{}).Execute(dockerCtx(fr),
			raw(t, map[string]any{"container": "hp-site1-web", "tail": ask})); err != nil {
			t.Fatalf("logs: %v", err)
		}
		last, _ := fr.Last()
		if !strings.Contains(argvOf(last), "--tail 2000") {
			t.Errorf("tail=%d produced %v, want it clamped to 2000", ask, last.Args)
		}
	}
}

// `docker stats` without --no-stream never returns, and a capability that never
// returns holds a broker connection open forever.
func TestStatsNeverStreams(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.ContainerStats{}).Execute(dockerCtx(fr), nil); err != nil {
		t.Fatalf("stats: %v", err)
	}
	last, _ := fr.Last()
	if !strings.Contains(argvOf(last), "--no-stream") {
		t.Fatalf("stats args = %v, want --no-stream", last.Args)
	}
}

// Removing a container is routine; removing its data is not. `docker rm` must
// never carry --volumes as a side effect of a delete button.
func TestRemoveNeverDeletesVolumes(t *testing.T) {
	fr := managedRunner(true)
	if _, err := (capabilities.ContainerRemove{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"container": "hp-site1-web", "force": true})); err != nil {
		t.Fatalf("remove: %v", err)
	}
	last, _ := fr.Last()
	for _, a := range last.Args {
		if a == "--volumes" || a == "-v" {
			t.Fatalf("remove deleted volumes: %v", last.Args)
		}
	}
	if !strings.Contains(argvOf(last), "--force") {
		t.Errorf("force was requested but not passed: %v", last.Args)
	}
}

// Removing an image terminates option parsing and passes force through, so a
// reference that begins with "-" cannot smuggle a flag past the pattern check.
func TestImageRemoveTerminatesOptionParsing(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.ImageRemove{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"image": "ghost:5-alpine", "force": true})); err != nil {
		t.Fatalf("remove: %v", err)
	}
	last, _ := fr.Last()
	if last.Args[0] != "rmi" {
		t.Fatalf("args = %v, want an rmi", last.Args)
	}
	if !strings.Contains(argvOf(last), "--force") {
		t.Errorf("force requested but not passed: %v", last.Args)
	}
	// "--" must precede the operand.
	if !strings.Contains(argvOf(last), "-- ghost:5-alpine") {
		t.Errorf("args = %v, want -- before the operand", last.Args)
	}

	for _, bad := range []string{"-v=/:/host", "--force", "-"} {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.ImageRemove{}).Execute(dockerCtx(fr),
			raw(t, map[string]any{"image": bad})); err == nil {
			t.Errorf("remove accepted %q as an image reference", bad)
		}
	}
}

// Docker refuses to remove an image a container still uses; that refusal is the
// ownership boundary for images, so it must reach the operator, not be masked.
func TestImageRemoveSurfacesTheInUseRefusal(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{ExitCode: 1,
		Stderr: []byte("Error response from daemon: conflict: unable to remove repository reference \"ghost:5-alpine\" (must force) - container abc is using its referenced image")}}
	_, err := (capabilities.ImageRemove{}).Execute(dockerCtx(fr),
		raw(t, map[string]any{"image": "ghost:5-alpine"}))
	if err == nil {
		t.Fatal("remove did not surface the in-use refusal")
	}
	if !strings.Contains(err.Error(), "using its referenced image") {
		t.Errorf("error = %q, want docker's own reason passed through", err.Error())
	}
}

// Prune is dangling-only unless all is asked for, and always non-interactive: a
// prompt the broker cannot answer would hang the capability.
func TestImagePruneIsDanglingOnlyByDefault(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.ImagePrune{}).Execute(dockerCtx(fr), nil); err != nil {
		t.Fatalf("prune: %v", err)
	}
	last, _ := fr.Last()
	if !strings.Contains(argvOf(last), "prune") || !strings.Contains(argvOf(last), "--force") {
		t.Fatalf("args = %v, want a forced prune", last.Args)
	}
	if strings.Contains(argvOf(last), "--all") {
		t.Errorf("default prune used --all: %v", last.Args)
	}

	fr2 := &exec.FakeRunner{}
	if _, err := (capabilities.ImagePrune{}).Execute(dockerCtx(fr2), raw(t, map[string]any{"all": true})); err != nil {
		t.Fatalf("prune all: %v", err)
	}
	all, _ := fr2.Last()
	if !strings.Contains(argvOf(all), "--all") {
		t.Errorf("all=true did not pass --all: %v", all.Args)
	}
}

// A container the daemon does not know is a 404, not an internal error — and it
// must not be reported as "not managed", which would be a confusing lie.
func TestUnknownContainerIsNotFound(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{ExitCode: 1, Stderr: []byte("No such object: nope")}}
	_, err := (capabilities.ContainerStart{}).Execute(dockerCtx(fr), raw(t, map[string]any{"container": "nope"}))
	if errx.KindOf(err) != errx.KindNotFound {
		t.Errorf("kind = %v, want NotFound", errx.KindOf(err))
	}
}
