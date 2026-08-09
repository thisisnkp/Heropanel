// Command np-broker is NexPanel's privileged broker. It runs as root and is the
// only component permitted to perform privileged system operations, on request
// from the unprivileged core (npd) over a Unix socket (ADR-0007).
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/thisisnkp/nexpanel/broker"
	"github.com/thisisnkp/nexpanel/broker/audit"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/policy"
	"github.com/thisisnkp/nexpanel/broker/transport"
	"github.com/thisisnkp/nexpanel/pkg/brokerwire"
	"github.com/thisisnkp/nexpanel/pkg/logx"
	"github.com/thisisnkp/nexpanel/pkg/nodepki"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		check       = flag.Bool("check", false, "run an offline self-check and exit")
		serve       = flag.Bool("serve", false, "run the broker socket server")
		socket      = flag.String("socket", brokerwire.DefaultSocket, "unix socket path to listen on")

		// Multi-node (docs/27). Absent, the broker is exactly what it has always
		// been: a Unix socket on one host.
		listenTLS   = flag.String("listen-tls", "", "additionally serve remote nodes on host:port over mutual TLS")
		tlsCert     = flag.String("tls-cert", "", "PEM certificate for the remote listener")
		tlsKey      = flag.String("tls-key", "", "PEM private key for the remote listener")
		tlsClientCA = flag.String("tls-client-ca", "", "PEM CA that remote node certificates must chain to")
		allowNodes  = flag.String("allow-node", "", "comma-separated certificate common names permitted to call remotely")

		initPKI  = flag.Bool("init-pki", false, "generate a node CA and a server/client keypair, then exit")
		pkiDir   = flag.String("pki-dir", "", "directory to write PKI material into (with --init-pki)")
		pkiNode  = flag.String("node-name", "", "common name for the issued client certificate (with --init-pki)")
		pkiHosts = flag.String("server-host", "", "comma-separated DNS names/IPs for the server certificate (with --init-pki)")
	)
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Println("np-broker", broker.Version)
	case *check:
		if err := runSelfCheck(); err != nil {
			fmt.Fprintln(os.Stderr, "self-check FAILED:", err)
			os.Exit(1)
		}
		fmt.Println("self-check OK")
	case *initPKI:
		if err := runInitPKI(*pkiDir, *pkiNode, *pkiHosts); err != nil {
			fmt.Fprintln(os.Stderr, "np-broker:", err)
			os.Exit(1)
		}
	case *serve:
		opts := remoteOptions{
			addr:     *listenTLS,
			certFile: *tlsCert,
			keyFile:  *tlsKey,
			caFile:   *tlsClientCA,
			nodes:    splitNodes(*allowNodes),
		}
		if err := runServe(*socket, opts); err != nil {
			fmt.Fprintln(os.Stderr, "np-broker:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "np-broker: no command given (try --serve, --check, --init-pki, or --version).")
		os.Exit(2)
	}
}

// remoteOptions is the multi-node listener's configuration. The zero value means
// "local only", which is what every single-node install has.
type remoteOptions struct {
	addr     string
	certFile string
	keyFile  string
	caFile   string
	nodes    []string
}

func (o remoteOptions) enabled() bool { return o.addr != "" }

// validate refuses a half-configured remote endpoint.
//
// Every missing piece here is a way to end up listening on a network with less
// authentication than intended, so none of them degrade to a default. An
// endpoint that cannot verify its callers must not open at all.
func (o remoteOptions) validate() error {
	switch {
	case o.certFile == "" || o.keyFile == "":
		return errors.New("--listen-tls requires --tls-cert and --tls-key")
	case o.caFile == "":
		return errors.New("--listen-tls requires --tls-client-ca (there is no way to verify a node without it)")
	case len(o.nodes) == 0:
		return errors.New("--listen-tls requires --allow-node (a valid certificate is not by itself permission to drive root)")
	}
	return nil
}

func splitNodes(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// runServe listens on the Unix socket — and, when configured, additionally on a
// mutual-TLS network endpoint for remote nodes — and serves privileged
// capabilities.
func runServe(socket string, remote remoteOptions) error {
	token := os.Getenv("NP_BROKER_TOKEN")
	if token == "" {
		return fmt.Errorf("NP_BROKER_TOKEN is required to serve (refusing to run without a token)")
	}
	// Validated before anything is created. A half-configured remote endpoint
	// should not leave a socket file behind on its way to failing.
	if remote.enabled() {
		if err := remote.validate(); err != nil {
			return err
		}
	}

	log := logx.New(os.Stderr, logx.Options{
		Level:  logx.ParseLevel(os.Getenv("NP_LOG_LEVEL")),
		Format: logx.FormatJSON,
	})

	// Audit entries are emitted as JSON lines to stderr (captured by journald).
	enc := json.NewEncoder(os.Stderr)
	chain := audit.NewChain(func(e audit.Entry) error { return enc.Encode(e) })

	pol := policy.Default()
	// The account npd runs as is a deployment fact: a packager may install under
	// a different name, and a test harness may run the panel as root.
	if v := os.Getenv("NP_BROKER_PANEL_USER"); v != "" {
		pol.PanelUser = v
	}

	b := broker.New(broker.DefaultRegistry(), pol, chain, exec.OSRunner{}, log)
	srv := transport.NewServer(b, token, log)
	if v := os.Getenv("NP_BROKER_ALLOWED_UID"); v != "" {
		if uid, err := strconv.Atoi(v); err == nil {
			srv.AllowedUID = uid
		}
	}

	// Fresh socket, group-accessible (root:nexpanel via installer; here we set
	// the mode and rely on the group for access).
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("listen %s: %w", socket, err)
	}
	if err := os.Chmod(socket, 0o660); err != nil {
		log.Warn("could not chmod broker socket", "err", err)
	}
	// Give the socket to the panel user's group so the unprivileged core (which
	// runs as pol.PanelUser, not root) can connect through the 0660 mode. Without
	// this the socket is root:root and privilege separation would wall npd off
	// from the very broker it must call. Best-effort: on a dev/test host that
	// account may not exist, in which case we leave the mode as-is.
	if pu := pol.EffectivePanelUser(); pu != "" {
		if u, err := user.Lookup(pu); err == nil {
			if gid, err := strconv.Atoi(u.Gid); err == nil {
				if err := os.Chown(socket, 0, gid); err != nil {
					log.Warn("could not set broker socket group to panel group", "user", pu, "err", err)
				}
			}
		} else {
			log.Debug("panel user not found; leaving broker socket group unchanged", "user", pu)
		}
	}

	// The remote listener is built before the local one starts accepting, so a
	// misconfigured endpoint is a refusal to start rather than a broker that runs
	// happily with the network half quietly missing.
	var remoteLn net.Listener
	if remote.enabled() {
		if err := remote.validate(); err != nil {
			return err
		}
		certPEM, err := os.ReadFile(remote.certFile)
		if err != nil {
			return fmt.Errorf("read --tls-cert: %w", err)
		}
		keyPEM, err := os.ReadFile(remote.keyFile)
		if err != nil {
			return fmt.Errorf("read --tls-key: %w", err)
		}
		caPEM, err := os.ReadFile(remote.caFile)
		if err != nil {
			return fmt.Errorf("read --tls-client-ca: %w", err)
		}
		tlsCfg, err := nodepki.ServerTLS(certPEM, keyPEM, caPEM)
		if err != nil {
			return err
		}
		raw, err := net.Listen("tcp", remote.addr)
		if err != nil {
			return fmt.Errorf("listen %s: %w", remote.addr, err)
		}
		remoteLn = tls.NewListener(raw, tlsCfg)
		srv.AllowedNodes = remote.nodes
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("broker serving", "socket", socket, "capabilities", b.Capabilities(),
		"peercred", srv.AllowedUID >= 0, "remote", remote.addr, "nodes", remote.nodes)

	// Either listener failing ends the process: systemd restarts it, and a broker
	// serving only half of what it was told to serve is worse than one that is
	// visibly down.
	errCh := make(chan error, 2)
	go func() { errCh <- srv.Serve(ctx, ln) }()
	if remoteLn != nil {
		go func() { errCh <- srv.ServeRemote(ctx, remoteLn) }()
	}
	return <-errCh
}

// runInitPKI mints the material a two-node install needs: a root, a server
// certificate for the broker's listener, and a client certificate for the node
// that will call it.
//
// It exists so standing up the remote transport does not begin with an openssl
// incantation. A deployment with its own PKI can skip it entirely — the listener
// only ever consumes PEM files and does not care who made them.
func runInitPKI(dir, node, hosts string) error {
	if dir == "" {
		return errors.New("--init-pki requires --pki-dir")
	}
	if node == "" {
		return errors.New("--init-pki requires --node-name")
	}
	var dns []string
	var ips []net.IP
	for _, h := range splitNodes(hosts) {
		if ip := net.ParseIP(h); ip != nil {
			ips = append(ips, ip)
		} else {
			dns = append(dns, h)
		}
	}
	if len(dns) == 0 && len(ips) == 0 {
		return errors.New("--init-pki requires --server-host (the address nodes will dial)")
	}

	ca, err := nodepki.NewCA("nexpanel node CA", 0)
	if err != nil {
		return err
	}
	server, err := ca.Issue(nodepki.Options{
		CommonName: "np-broker", TTL: 0, Server: true, DNSNames: dns, IPs: ips,
	})
	if err != nil {
		return err
	}
	client, err := ca.Issue(nodepki.Options{CommonName: node, TTL: 0, Client: true})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	// Private keys are 0600 and the CA key never leaves this directory. Writing a
	// key world-readable would hand over the whole installation.
	files := []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{"ca.crt", ca.CertPEM, 0o644},
		{"ca.key", ca.KeyPEM, 0o600},
		{"server.crt", server.CertPEM, 0o644},
		{"server.key", server.KeyPEM, 0o600},
		{node + ".crt", client.CertPEM, 0o644},
		{node + ".key", client.KeyPEM, 0o600},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, f.mode); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	fmt.Printf("wrote node PKI to %s\n", dir)
	fmt.Printf("  broker:  --listen-tls <addr> --tls-cert server.crt --tls-key server.key --tls-client-ca ca.crt --allow-node %s\n", node)
	fmt.Printf("  node %s: ca.crt + %s.crt + %s.key\n", node, node, node)
	return nil
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
