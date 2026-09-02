package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/thisisnkp/nexpanel/internal/bootstrap"
	"github.com/thisisnkp/nexpanel/internal/config"
	"github.com/thisisnkp/nexpanel/internal/license"
)

// runLicense implements `npd license <activate|status|refresh|deactivate>`.
//
// A subcommand rather than a flag, and one that stands up the licence client on
// its own rather than booting the daemon, because the two moments it matters
// most are a fresh install with no database configured and a box where the
// panel will not start. Both are exactly when an operator needs to see, or fix,
// the licence — and neither can wait for npd to come up first.
//
// `status` never touches the network: it reads the lease on disk and walks the
// same ladder the running panel does, so what it prints is what the panel is
// doing, not what the licence server thinks.
func runLicense(args []string) int {
	if len(args) == 0 {
		licenseUsage()
		return 2
	}

	cfg, err := config.Load(configPathForCLI())
	if err != nil {
		fmt.Fprintln(os.Stderr, "npd license: config error:", err)
		return 1
	}

	svc, err := license.New(license.Options{
		Dir:       bootstrap.DataDir(cfg),
		ServerURL: cfg.License.ServerURL,
		ExtraKeys: map[string]string{cfg.License.KeyID: cfg.License.PubKey},
		Version:   version,
		// Quiet: this is a CLI, and its output is the report below, not a log
		// stream interleaved with it.
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "npd license:", err)
		fmt.Fprintln(os.Stderr, "  This build pins no licence key and none is configured, so there is nothing to check.")
		return 1
	}

	// Generous, because activation is interactive and the retries inside the
	// client are deliberately spaced. An operator who typed a key would rather
	// wait than be told to try again.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	switch args[0] {
	case "status":
		printLicense(svc)
		return 0

	case "activate":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			fmt.Fprintln(os.Stderr, "usage: npd license activate <key>")
			fmt.Fprintln(os.Stderr, "  The key looks like NXP-XXXXX-XXXXX-XXXXX-XXXXX.")
			return 2
		}
		if _, err := svc.Activate(ctx, args[1]); err != nil {
			return licenseFailure("activate", err)
		}
		fmt.Println("Activated.")
		fmt.Println()
		printLicense(svc)
		return 0

	case "refresh":
		if _, err := svc.Refresh(ctx); err != nil {
			// Not fatal to the licence, only to this attempt. The panel keeps
			// running on its stored lease, and saying so here stops an operator
			// concluding their licence has failed when their network has.
			fmt.Fprintln(os.Stderr, "npd license refresh:", err)
			fmt.Fprintln(os.Stderr, "  The panel is still running on its stored lease; nothing has changed.")
			fmt.Println()
			printLicense(svc)
			return 1
		}
		fmt.Println("Refreshed.")
		fmt.Println()
		printLicense(svc)
		return 0

	case "deactivate":
		if err := svc.Deactivate(ctx); err != nil {
			if errors.Is(err, license.ErrNotActivated) {
				fmt.Fprintln(os.Stderr, "npd license deactivate: this installation is not activated.")
				return 1
			}
			return licenseFailure("deactivate", err)
		}
		fmt.Println("Deactivated. The activation slot has been released and can be used on another server.")
		fmt.Println("Your websites, databases and mail are untouched and still running.")
		return 0

	default:
		licenseUsage()
		return 2
	}
}

func licenseUsage() {
	fmt.Fprintln(os.Stderr, "usage: npd license <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  status              show the licence and what it allows (works offline)")
	fmt.Fprintln(os.Stderr, "  activate <key>      bind this server to a licence key")
	fmt.Fprintln(os.Stderr, "  refresh             fetch a fresh lease now")
	fmt.Fprintln(os.Stderr, "  deactivate          release this server's activation slot")
}

// printLicense is the report. Written for someone who opened it because
// something is wrong, so it leads with the state and always says, in plain
// words, that their sites are unaffected.
func printLicense(svc *license.Service) {
	st := svc.Status()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintf(w, "State:\t%s\n", st.State)
	fmt.Fprintf(w, "Reason:\t%s\n", st.Reason)
	if st.Plan != "" {
		fmt.Fprintf(w, "Plan:\t%s\n", st.Plan)
		fmt.Fprintf(w, "Licence:\t%s\n", st.LID)
		fmt.Fprintf(w, "Limits:\t%d sites, %d databases, %d panel users\n",
			st.Limits.Sites, st.Limits.DBs, st.Limits.Users)
	}
	if len(st.Features) > 0 {
		fmt.Fprintf(w, "Features:\t%s\n", strings.Join(st.Features, ", "))
	}
	if !st.ExpiresAt.IsZero() {
		fmt.Fprintf(w, "Lease expires:\t%s\n", st.ExpiresAt.Format(time.RFC1123))
	}
	if !st.SubscriptionEndsAt.IsZero() {
		fmt.Fprintf(w, "Subscription ends:\t%s\n", st.SubscriptionEndsAt.Format(time.RFC1123))
	}
	if last := svc.LastContact(); !last.IsZero() {
		fmt.Fprintf(w, "Last contact:\t%s\n", last.Format(time.RFC1123))
	} else {
		fmt.Fprintf(w, "Last contact:\t%s\n", "never")
	}
	if !svc.Pinned() {
		fmt.Fprintf(w, "Enforcement:\t%s\n",
			"off — this build pins no licence key (development build)")
	}
	_ = w.Flush()

	if b := st.Banner(); b != "" {
		fmt.Println()
		fmt.Println(b)
	}
	if st.State == license.StateDegraded || st.State == license.StateLocked {
		fmt.Println()
		fmt.Println("Nothing that serves traffic is affected: the web server, PHP, MySQL, mail,")
		fmt.Println("cron and backups all keep running exactly as before.")
	}
}

func licenseFailure(verb string, err error) int {
	var se *license.ServerError
	if errors.As(err, &se) {
		fmt.Fprintf(os.Stderr, "npd license %s: %s\n", verb, se.Message)
		fmt.Fprintf(os.Stderr, "  (%s)\n", se.Code)
		return 1
	}
	fmt.Fprintf(os.Stderr, "npd license %s: %v\n", verb, err)
	return 1
}

// configPathForCLI mirrors the daemon's own rule: the NP_CONFIG path, or the
// default, and an absent file is not an error because defaults plus NP_*
// environment variables are a complete configuration.
func configPathForCLI() string {
	path := envOr("NP_CONFIG", "/etc/nexpanel/config.yaml")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
