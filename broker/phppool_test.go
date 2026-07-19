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
	// Config-test first (php-fpm -t), then reload — the reload-first discipline
	// that keeps one site's bad pool from taking the whole FPM master down.
	if len(ran) != 2 {
		t.Fatalf("expected a config test then a reload, got %v", ran)
	}
	if !strings.Contains(ran[0], "php-fpm8.2 -t") {
		t.Fatalf("expected a config test before reload, got %v", ran)
	}
	if !strings.Contains(ran[1], "reload php8.2-fpm") {
		t.Fatalf("expected php8.2-fpm reload, got %v", ran)
	}
}

// A pool file php-fpm rejects must never reach a reload: the master re-reads
// every pool on the version, so one bad file takes them all down.
func TestPHPWritePoolRollsBackOnFailedConfigTest(t *testing.T) {
	runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if strings.Contains(strings.Join(c.Args, " "), "-t") {
			return exec.Result{ExitCode: 1}, nil // config test fails
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	b, fs := newBrokerWithFS(t, runner)
	_ = fs.WriteFile("/etc/php/8.2/fpm/pool.d/hps1.conf", []byte("PRIOR"), 0o644)

	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "php.write_pool",
		Input:      mustJSON(t, map[string]string{"version": "8.2", "pool_name": "hps1", "config": "NEW"}),
	})
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation error on a failed config test, got %v", err)
	}
	if got, _ := fs.Written("/etc/php/8.2/fpm/pool.d/hps1.conf"); got != "PRIOR" {
		t.Fatalf("pool config not rolled back after a failed test, got %q", got)
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
	// The config test passes; only the reload fails — the case where the config
	// was fine but the service could not be signalled.
	runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if strings.Contains(strings.Join(c.Args, " "), "-t") {
			return exec.Result{ExitCode: 0}, nil
		}
		return exec.Result{ExitCode: 1}, nil // reload fails
	}}
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
