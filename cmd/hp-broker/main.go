// Command hp-broker is HeroPanel's privileged broker. It runs as root and is the
// only component permitted to perform privileged system operations, on request
// from the unprivileged core (hpd) over a Unix socket (ADR-0007).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/audit"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/broker/transport"
	"github.com/thisisnkp/heropanel/pkg/brokerwire"
	"github.com/thisisnkp/heropanel/pkg/logx"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		check       = flag.Bool("check", false, "run an offline self-check and exit")
		serve       = flag.Bool("serve", false, "run the broker socket server")
		socket      = flag.String("socket", brokerwire.DefaultSocket, "unix socket path to listen on")
	)
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Println("hp-broker", broker.Version)
	case *check:
		if err := runSelfCheck(); err != nil {
			fmt.Fprintln(os.Stderr, "self-check FAILED:", err)
			os.Exit(1)
		}
		fmt.Println("self-check OK")
	case *serve:
		if err := runServe(*socket); err != nil {
			fmt.Fprintln(os.Stderr, "hp-broker:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "hp-broker: no command given (try --serve, --check, or --version).")
		os.Exit(2)
	}
}

// runServe listens on the Unix socket and serves privileged capabilities.
func runServe(socket string) error {
	token := os.Getenv("HP_BROKER_TOKEN")
	if token == "" {
		return fmt.Errorf("HP_BROKER_TOKEN is required to serve (refusing to run without a token)")
	}

	log := logx.New(os.Stderr, logx.Options{
		Level:  logx.ParseLevel(os.Getenv("HP_LOG_LEVEL")),
		Format: logx.FormatJSON,
	})

	// Audit entries are emitted as JSON lines to stderr (captured by journald).
	enc := json.NewEncoder(os.Stderr)
	chain := audit.NewChain(func(e audit.Entry) error { return enc.Encode(e) })

	b := broker.New(broker.DefaultRegistry(), policy.Default(), chain, exec.OSRunner{}, log)
	srv := transport.NewServer(b, token, log)
	if v := os.Getenv("HP_BROKER_ALLOWED_UID"); v != "" {
		if uid, err := strconv.Atoi(v); err == nil {
			srv.AllowedUID = uid
		}
	}

	// Fresh socket, group-accessible (root:heropanel via installer; here we set
	// the mode and rely on the group for access).
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("listen %s: %w", socket, err)
	}
	if err := os.Chmod(socket, 0o660); err != nil {
		log.Warn("could not chmod broker socket", "err", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("broker serving", "socket", socket, "capabilities", b.Capabilities(), "peercred", srv.AllowedUID >= 0)
	return srv.Serve(ctx, ln)
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

	in, _ := json.Marshal(map[string]string{"service": "mariadb"})
	if _, err := b.Invoke(context.Background(), broker.Request{
		Capability: "service.restart",
		Input:      in,
	}); err != nil {
		return fmt.Errorf("expected service.restart to succeed: %w", err)
	}
	if err := audit.Verify(entries); err != nil {
		return fmt.Errorf("audit chain did not verify: %w", err)
	}
	fmt.Printf("audit entries: %d (chain verified)\n", len(entries))
	return nil
}
