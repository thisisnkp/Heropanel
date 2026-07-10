// Command hp-broker is HeroPanel's privileged broker. It runs as root and is the
// only component permitted to perform privileged system operations, on request
// from the unprivileged core (hpd) over a Unix socket.
//
// This entrypoint currently supports offline self-checks (--check, --version).
// The gRPC-over-Unix-socket server with SO_PEERCRED authentication is Linux-only
// and is wired up in the next milestone (see docs/06 and docs/08).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/audit"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/logx"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		check       = flag.Bool("check", false, "run an offline self-check and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("hp-broker", broker.Version)
		return
	}

	if *check {
		if err := runSelfCheck(); err != nil {
			fmt.Fprintln(os.Stderr, "self-check FAILED:", err)
			os.Exit(1)
		}
		fmt.Println("self-check OK")
		return
	}

	fmt.Fprintln(os.Stderr, "hp-broker: no command given (try --check or --version).")
	fmt.Fprintln(os.Stderr, "the socket server is wired up in the next milestone.")
	os.Exit(2)
}

// runSelfCheck constructs the broker with the default registry/policy and an
// in-memory audit chain, then exercises the authorization + audit path using a
// fake runner so no real system commands execute. It is safe to run anywhere.
func runSelfCheck() error {
	log := logx.New(os.Stderr, logx.Options{Level: logx.ParseLevel("warn")})

	var entries []audit.Entry
	chain := audit.NewChain(func(e audit.Entry) error {
		entries = append(entries, e)
		return nil
	})

	fake := &exec.FakeRunner{Result: exec.Result{ExitCode: 0}}
	b := broker.New(broker.DefaultRegistry(), policy.Default(), chain, fake, log)

	fmt.Println("registered capabilities:", b.Capabilities())

	// A valid, allowlisted restart should succeed.
	in, _ := json.Marshal(map[string]string{"service": "mariadb"})
	if _, err := b.Invoke(context.Background(), broker.Request{
		Capability: "service.restart",
		Input:      in,
	}); err != nil {
		return fmt.Errorf("expected service.restart to succeed: %w", err)
	}

	// The audit chain must verify.
	if err := audit.Verify(entries); err != nil {
		return fmt.Errorf("audit chain did not verify: %w", err)
	}
	fmt.Printf("audit entries: %d (chain verified)\n", len(entries))
	return nil
}
