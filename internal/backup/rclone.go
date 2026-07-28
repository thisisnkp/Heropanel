package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// An rclone-backed backup target: 70+ cloud backends (Google Drive, Dropbox,
// OneDrive, Backblaze, …) without the panel implementing any of their APIs or
// OAuth flows. The operator configures the remote once with `rclone config`;
// the panel just streams its already-sealed blobs to it. rclone is a local CLI
// hpd execs directly (like the local/S3/SFTP targets, no broker is involved —
// there is no privilege to cross, only the operator's own rclone config and the
// network). This is the lean way to reach consumer drives: one external tool,
// no OAuth code and no provider SDKs in hpd.

const (
	rcloneSessionTO = 30 * time.Minute
)

// RcloneConfig configures the rclone target.
type RcloneConfig struct {
	Bin    string // rclone binary (default "rclone")
	Config string // path to rclone.conf ("" = rclone's default)
	// Remote is the operator's configured destination, base path included,
	// e.g. "gdrive:heropanel-backups" or ":local:/srv/backups".
	Remote string
}

// rcloneTarget is a backup Target backed by the rclone CLI.
type rcloneTarget struct {
	cfg RcloneConfig
}

// TargetRclone is the registered name of the rclone target.
const TargetRclone = "rclone"

// NewRcloneTarget builds an rclone target. Returns nil when no remote is set.
func NewRcloneTarget(cfg RcloneConfig) *rcloneTarget {
	if strings.TrimSpace(cfg.Remote) == "" {
		return nil
	}
	if cfg.Bin == "" {
		cfg.Bin = "rclone"
	}
	return &rcloneTarget{cfg: cfg}
}

func (*rcloneTarget) Name() string { return TargetRclone }

// object maps a key to its full rclone path under the remote.
func (t *rcloneTarget) object(key string) string {
	remote := strings.TrimRight(t.cfg.Remote, "/")
	return remote + "/" + strings.TrimLeft(key, "/")
}

// args builds the argv: the config flag (when set) before the verb, then the
// verb and its operands. These verbs (rcat/cat/deletefile) never prompt, so no
// interaction flag is needed — and older rclone builds reject some global flags
// in front of a subcommand.
func (t *rcloneTarget) args(verb string, rest ...string) []string {
	a := []string{}
	if t.cfg.Config != "" {
		a = append(a, "--config", t.cfg.Config)
	}
	a = append(a, verb)
	return append(a, rest...)
}

// Put streams r to the remote object. `rclone rcat` reads stdin and creates the
// object (and any parent directories) at the destination.
func (t *rcloneTarget) Put(ctx context.Context, key string, r io.Reader, _ int64) error {
	ctx, cancel := context.WithTimeout(ctx, rcloneSessionTO)
	defer cancel()
	cmd := exec.CommandContext(ctx, t.cfg.Bin, t.args("rcat", t.object(key))...)
	cmd.Stdin = r
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone rcat: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Get downloads the remote object. `rclone cat` writes it to stdout; backups are
// pulled to a staging file for restore, so buffering is fine.
func (t *rcloneTarget) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, rcloneSessionTO)
	defer cancel()
	cmd := exec.CommandContext(ctx, t.cfg.Bin, t.args("cat", t.object(key))...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("rclone cat: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return io.NopCloser(bytes.NewReader(out.Bytes())), nil
}

// Delete removes the remote object. A missing object is not an error (delete is
// idempotent) — `rclone deletefile` on an absent file is tolerated.
func (t *rcloneTarget) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, rcloneSessionTO)
	defer cancel()
	cmd := exec.CommandContext(ctx, t.cfg.Bin, t.args("deletefile", t.object(key))...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.ToLower(stderr.String())
		if strings.Contains(msg, "not found") || strings.Contains(msg, "no such") ||
			strings.Contains(msg, "object not found") || strings.Contains(msg, "directory not found") {
			return nil
		}
		return fmt.Errorf("rclone deletefile: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
