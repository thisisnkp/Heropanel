package exec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/thisisnkp/heropanel/broker/exec"
)

func TestOSRunnerRejectsRelativePath(t *testing.T) {
	_, err := exec.OSRunner{}.Run(context.Background(), exec.Command{Path: "systemctl"})
	if !errors.Is(err, exec.ErrNotAbsolute) {
		t.Fatalf("expected ErrNotAbsolute for relative path, got %v", err)
	}
}

func TestFakeRunnerRecordsCalls(t *testing.T) {
	f := &exec.FakeRunner{Result: exec.Result{ExitCode: 0}}
	ctx := context.Background()

	if _, ok := f.Last(); ok {
		t.Fatal("no calls yet, Last should report false")
	}
	_, _ = f.Run(ctx, exec.Command{Path: "/bin/a", Args: []string{"1"}})
	_, _ = f.Run(ctx, exec.Command{Path: "/bin/b", Args: []string{"2"}})

	if len(f.Calls) != 2 {
		t.Fatalf("recorded %d calls, want 2", len(f.Calls))
	}
	last, ok := f.Last()
	if !ok || last.Path != "/bin/b" {
		t.Fatalf("Last = %+v (ok=%v), want /bin/b", last, ok)
	}
}
