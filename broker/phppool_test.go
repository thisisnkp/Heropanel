package broker_test

import (
	"context"
	"strings"
	"testing"

	brokerd "github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func TestPHPWritePool(t *testing.T) {
	var ran []string
	runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		ran = append(ran, c.Path+" "+strings.Join(c.Args, " "))
		return exec.Result{ExitCode: 0}, nil
	}}
	b, fs := newBrokerWithFS(t, runner)

	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "php.write_pool",
		Input: mustJSON(t, map[string]string{
			"version":   "8.2",
			"pool_name": "hps1",
			"config":    "[hps1]\nuser = hps1\n",
		}),
	})
	if err != nil {
		t.Fatalf("write pool: %v", err)
	}
	if got, ok := fs.Written("/etc/php/8.2/fpm/pool.d/hps1.conf"); !ok || !strings.Contains(got, "[hps1]") {
		t.Fatalf("pool config not written correctly: %q", got)
	}
	if len(ran) != 1 || !strings.Contains(ran[0], "reload php8.2-fpm") {
		t.Fatalf("expected php8.2-fpm reload, got %v", ran)
	}
}

func TestPHPWritePoolRejectsBadVersion(t *testing.T) {
	b, _ := newBrokerWithFS(t, &exec.FakeRunner{})
	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "php.write_pool",
		Input:      mustJSON(t, map[string]string{"version": "8.2; rm -rf", "pool_name": "hps1", "config": "x"}),
	})
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation error for bad version, got %v", err)
	}
}

func TestPHPWritePoolRollsBackOnReloadFailure(t *testing.T) {
	runner := &exec.FakeRunner{Result: exec.Result{ExitCode: 1}} // reload fails
	b, fs := newBrokerWithFS(t, runner)
	_ = fs.WriteFile("/etc/php/8.2/fpm/pool.d/hps1.conf", []byte("PRIOR"), 0o644)

	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "php.write_pool",
		Input:      mustJSON(t, map[string]string{"version": "8.2", "pool_name": "hps1", "config": "NEW"}),
	})
	if !errx.IsKind(err, errx.KindUpstream) {
		t.Fatalf("want upstream error on reload failure, got %v", err)
	}
	if got, _ := fs.Written("/etc/php/8.2/fpm/pool.d/hps1.conf"); got != "PRIOR" {
		t.Fatalf("pool config not rolled back, got %q", got)
	}
}
