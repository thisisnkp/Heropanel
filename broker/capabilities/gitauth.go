package capabilities

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// gitAuthRoot holds the short-lived credential material for a private clone. It
// lives on /run (tmpfs): the secrets never touch persistent disk, never reach a
// backup, and are gone after a reboot even if a crash skips cleanup.
const gitAuthRoot = "/run/heropanel/gitauth"

const sshPath = "/usr/bin/ssh"

// Authentication kinds accepted from hpd. Deliberately restated here rather than
// imported from internal/git: the broker is the privileged component and
// re-derives its own view of every input, exactly as it re-validates repo URLs
// and refs below.
const (
	authNone   = "none"
	authToken  = "token"
	authSSHKey = "ssh_key"
)

// gitAuth is the credential the panel unsealed for this deploy.
type gitAuth struct {
	Kind     string `json:"kind"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
	// HostKey pins the server's SSH host key(s) (known_hosts / ssh-keyscan
	// format). When set, the clone uses strict host-key checking; empty falls
	// back to accept-new (TOFU).
	HostKey string `json:"host_key"`
}

// gitCredential is a credential staged on disk for exactly one clone: the extra
// git arguments / environment that reference it, plus the directory to shred
// afterwards.
type gitCredential struct {
	dir  string   // removed by cleanup
	args []string // extra `git -c ...` arguments, before the subcommand
	env  []string // extra environment for the git process
}

// prepareGitAuth stages a credential for the site user and returns how to use
// it. It returns (nil, nil) for a public repository.
//
// Two invariants drive the design:
//
//  1. The secret never appears in argv. Every process on the box can read
//     /proc/<pid>/cmdline, so a token on a git command line is readable by every
//     other site on the server. Instead the secret goes into a 0600 file owned by
//     the site user and git is pointed at the *path* — a credential helper file
//     for tokens, an identity file for SSH keys.
//
//  2. The file is owned by the site user, not root, because git runs as that
//     user. That is also why the caller must destroy it before running the site's
//     own build command (see the cleanup call in git.deploy).
func prepareGitAuth(c capability.Context, a *gitAuth, username, repoURL string) (*gitCredential, error) {
	if a == nil || a.Kind == "" || a.Kind == authNone {
		return nil, nil
	}
	if a.Secret == "" {
		return nil, errx.Validation("missing_credential", "The credential is empty.")
	}

	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, errx.Upstream(err, "auth_setup_failed", "Could not stage the Git credential.")
	}
	dir := gitAuthRoot + "/" + hex.EncodeToString(nonce)
	cred := &gitCredential{dir: dir}

	// 0711, not 0700: the per-deploy directory below this one belongs to the site
	// user, and git runs as that user — so it has to be able to *traverse* the
	// parent to reach its own credential. --x grants exactly that and nothing
	// else: the directory cannot be listed, so one site cannot enumerate another
	// site's deploys, and each per-deploy directory is 0700 owned by its own user
	// anyway. (A 0700 root parent here silently breaks every private clone with
	// "Identity file not accessible".)
	if err := c.FS.MkdirAll(gitAuthRoot, 0o711); err != nil {
		return nil, errx.Upstream(err, "auth_setup_failed", "Could not stage the Git credential.")
	}
	// MkdirAll only applies its mode when it creates the directory, and the mode
	// is subject to umask besides. Force it, or a root-owned 0700 left over from
	// an earlier run keeps every private clone broken.
	if err := runChmod(c, "0711", gitAuthRoot); err != nil {
		return nil, err
	}
	if err := runInstall(c, "-d", "-m", "0700", "-o", username, "-g", username, dir); err != nil {
		return nil, err
	}

	switch a.Kind {
	case authToken:
		file := dir + "/credentials"
		// git-credential-store's format. Both fields are percent-encoded: a token
		// containing ':' or '@' would otherwise be parsed as part of the host.
		line := "https://" + url.QueryEscape(a.Username) + ":" + url.QueryEscape(a.Secret) + "@" + hostOf(repoURL) + "\n"
		if err := cred.writeAs(c, username, file, []byte(line)); err != nil {
			return nil, err
		}
		cred.args = []string{
			// Reset first: never let a helper configured elsewhere on the box (or
			// a stale ~/.gitconfig) answer for this clone.
			"-c", "credential.helper=",
			"-c", "credential.helper=store --file=" + file,
		}

	case authSSHKey:
		key := dir + "/id"
		if err := cred.writeAs(c, username, key, []byte(ensureTrailingNewline(a.Secret))); err != nil {
			return nil, err
		}
		known := dir + "/known_hosts"
		// If the operator pinned the host key, seed known_hosts with it and use
		// strict checking so the *first* connection is verified too — defeating a
		// MITM on first contact. Otherwise fall back to accept-new (TOFU) against
		// an empty file, which still refuses a *changed* key mid-clone.
		strict := "accept-new"
		var khContent []byte
		if a.HostKey != "" {
			if content := knownHostsContent(a.HostKey, repoURL); content != "" {
				khContent = []byte(content)
				strict = "yes"
			}
		}
		if err := cred.writeAs(c, username, known, khContent); err != nil {
			return nil, err
		}
		// IdentitiesOnly stops ssh from offering any other key it finds (an agent,
		// ~/.ssh/id_*) and burning the server's auth attempts before ours.
		// BatchMode turns a credential prompt into a fast failure instead of a hang.
		cred.env = []string{
			"GIT_SSH_COMMAND=" + sshPath +
				" -i " + key +
				" -o IdentitiesOnly=yes" +
				" -o IdentityAgent=none" +
				" -o BatchMode=yes" +
				" -o StrictHostKeyChecking=" + strict +
				" -o UserKnownHostsFile=" + known,
		}

	default:
		return nil, errx.Validation("invalid_auth_kind", "Unsupported Git authentication kind.")
	}
	return cred, nil
}

// writeAs stages one credential file: root writes it, then install(1) copies it
// into place with the site user's ownership and 0600 in a single step, and the
// root-owned original is removed.
func (g *gitCredential) writeAs(c capability.Context, username, dest string, data []byte) error {
	tmp := g.dir + ".stage"
	if err := c.FS.WriteFile(tmp, data, 0o600); err != nil {
		return errx.Upstream(err, "auth_setup_failed", "Could not stage the Git credential.")
	}
	defer func() { _ = c.FS.Remove(tmp) }()
	if err := runInstall(c, "-m", "0600", "-o", username, "-g", username, tmp, dest); err != nil {
		return err
	}
	return nil
}

// cleanup destroys the staged credential. It is safe to call more than once, and
// it must be called before any command the site owner controls gets to run.
func (g *gitCredential) cleanup(c capability.Context) {
	if g == nil || g.dir == "" {
		return
	}
	_ = c.FS.RemoveAll(g.dir)
	g.dir = ""
	g.args, g.env = nil, nil
}

// gitArgs returns the credential's `git -c ...` prefix (empty when public).
func (g *gitCredential) gitArgs() []string {
	if g == nil {
		return nil
	}
	return g.args
}

// gitEnv returns the credential's extra environment (empty when public).
func (g *gitCredential) gitEnv() []string {
	if g == nil {
		return nil
	}
	return g.env
}

// runChmod forces a path's mode. The broker's FS has no chmod primitive, and
// MkdirAll's mode is both umask-filtered and ignored for existing directories.
func runChmod(c capability.Context, mode, path string) error {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: chmodPath, Args: []string{mode, path}, Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return errx.New(errx.KindUpstream, "auth_setup_failed", "Could not stage the Git credential.")
	}
	return nil
}

// runInstall shells out to install(1) to create a path with an explicit owner
// and mode. The broker has no chown primitive; install does the whole job
// atomically, which is what we want for files that hold secrets.
func runInstall(c capability.Context, args ...string) error {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: installPath, Args: args, Timeout: 20 * time.Second,
	})
	if err != nil {
		return errx.Upstream(err, "auth_setup_failed", "Could not stage the Git credential.")
	}
	if res.ExitCode != 0 {
		return errx.New(errx.KindUpstream, "auth_setup_failed", "Could not stage the Git credential.")
	}
	return nil
}

// hostOf returns the host[:port] of an https URL, for the credential-store line.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// sshHostOf extracts the SSH host from either a full ssh:// URL or an scp-like
// "[user@]host:path" (which url.Parse cannot handle).
func sshHostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil {
			return u.Hostname()
		}
		return ""
	}
	if i := strings.Index(raw, "@"); i >= 0 {
		raw = raw[i+1:]
	}
	if i := strings.Index(raw, ":"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

// knownHostsContent turns an operator-supplied host key into known_hosts lines.
// A line with three or more fields is already ssh-keyscan output (host present)
// and is kept verbatim; a bare "keytype key" pair is prefixed with the repo's
// SSH host. Comments and blank lines are dropped. Returns "" if nothing usable
// remains, so the caller falls back to TOFU rather than pinning nothing.
func knownHostsContent(hostKey, repoURL string) string {
	host := sshHostOf(repoURL)
	var b strings.Builder
	for _, line := range strings.Split(hostKey, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch f := strings.Fields(line); {
		case len(f) >= 3:
			b.WriteString(line + "\n")
		case len(f) == 2 && host != "":
			b.WriteString(host + " " + line + "\n")
		}
	}
	return b.String()
}

func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
