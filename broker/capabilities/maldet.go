package capabilities

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// maldet — Linux Malware Detect — as a second malware engine beside ClamAV.
//
// Two scanners rather than one because they look for different things. ClamAV's
// signatures are a general antivirus corpus; maldet's are built from what
// actually turns up on shared hosting — web shells, injected PHP droppers,
// obfuscated backdoors — harvested from edge intrusion data and from ClamAV's
// own misses. On a panel whose job is hosting other people's PHP, the second
// list is the one that catches the thing that got in.
//
// maldet is deliberately run **report-only**. It has its own quarantine, and so
// does NexPanel (a root-only holding area with restore and delete, recorded in
// the panel's own history). Two quarantines would mean a file the operator can
// see in one and not the other, and a "restore" that puts back something the
// other tool already moved. So every scan passes `quarantine_hits=0` and
// `quarantine_clean=0` explicitly on the command line rather than trusting
// whatever the host's conf.maldet happens to say — the panel's quarantine is
// the only one.
//
// **Honest limit:** maldet is not in any distribution repository. It is
// distributed as a tarball from rfxn.com and installed by its own install.sh,
// which is why it needs an install capability of its own rather than a package
// name in the provisioning allowlist — and why that install is pinned to one
// host and records the hash of what it actually ran. See MaldetInstall.
const (
	maldetPath = "/usr/local/sbin/maldet"
	maldetRoot = "/usr/local/maldetect"
	// maldetSessDir holds one hits file per scan, named by the scan id.
	maldetSessDir = maldetRoot + "/sess"

	// maldetHost is the only host a maldet tarball may be fetched from. It is a
	// constant, not configuration: an operator-supplied URL would turn a panel
	// config file into "run this code as root", which is a much larger power
	// than "install the malware scanner".
	maldetHost = "https://www.rfxn.com"

	// maldetStageDir is where the tarball is downloaded and unpacked. Under
	// /var/lib so it survives nothing and is not on a noexec mount.
	maldetStageDir = "/var/lib/nexpanel/maldet-install"
)

// curlPath is the downloader. tarPath and shPath are declared by the file
// manager and the git deployer respectively; maldet reuses them rather than
// pinning a second opinion about where tar lives.
const curlPath = "/usr/bin/curl"

// reMaldetScanID matches the scan id maldet prints and names its session files
// by, e.g. "250822-1531.24680". Anchored and bounded so the path built from it
// cannot leave maldetSessDir.
var reMaldetScanID = regexp.MustCompile(`^[0-9]{6}-[0-9]{4}\.[0-9]{1,10}$`)

// reMaldetReportLine finds the scan id in maldet's "to view run: maldet
// --report <id>" line, which is the one place it prints the id unambiguously.
var reMaldetReportLine = regexp.MustCompile(`--report\s+([0-9]{6}-[0-9]{4}\.[0-9]{1,10})`)

// ── maldet.status ────────────────────────────────────────────────────────────

// MaldetStatus reports whether maldet is installed and what signatures it has.
//
// It exists so the panel can say "maldet is not installed" instead of running a
// scan that fails, and so the malware screen can show the signature date — a
// scanner running a year-old signature set is worth knowing about, and is
// invisible otherwise.
type MaldetStatus struct{}

// Name implements capability.Capability.
func (MaldetStatus) Name() string { return "maldet.status" }

// Execute implements capability.Capability.
func (MaldetStatus) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	installed, _ := c.FS.Exists(maldetPath)
	out := map[string]any{"installed": installed}
	if !installed {
		return capability.Result{Data: out}, nil
	}

	// `maldet --version` is cheap and does not touch the network.
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: maldetPath, Args: []string{"--version"}, Timeout: 30 * time.Second,
	}); err == nil {
		out["version"] = strings.TrimSpace(logTail(res, 200))
	}
	// The signature revision maldet last pulled. Absent on a fresh install that
	// has never run `maldet -u`, which is itself the answer.
	if b, err := c.FS.ReadFile(maldetRoot + "/sigs/sigpack.ver"); err == nil {
		out["signature_version"] = strings.TrimSpace(string(b))
	}
	return capability.Result{Data: out}, nil
}

// ── maldet.scan ──────────────────────────────────────────────────────────────

// MaldetScan runs maldet over a confined path and returns the detections.
type MaldetScan struct{}

type maldetScanInput struct {
	Path string `json:"path"`
}

// Name implements capability.Capability.
func (MaldetScan) Name() string { return "maldet.scan" }

// Execute implements capability.Capability.
func (MaldetScan) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in maldetScanInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for maldet.scan.")
	}
	if err := capability.ValidatePath(in.Path, c.Policy); err != nil {
		return capability.Result{}, err
	}
	if ok, _ := c.FS.Exists(maldetPath); !ok {
		return capability.Result{}, errx.New(errx.KindUnavailable, "maldet_not_installed",
			"maldet is not installed on this host.")
	}

	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: maldetPath,
		// --config-option overrides conf.maldet for this run only. Both flags
		// matter: quarantine_hits moves files, quarantine_clean tries to repair
		// them. Either one would edit a customer's site behind the operator's
		// back, and both belong to the panel's own quarantine instead.
		Args: []string{
			"--config-option", "quarantine_hits=0,quarantine_clean=0,quarantine_suspend_user=0",
			"-a", in.Path,
		},
		Timeout: 30 * time.Minute,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "scan_failed", "Could not run the maldet scan.")
	}
	// maldet exits non-zero in ordinary situations (including "hits found" on
	// some builds), so the exit code is not the signal. The scan id is: without
	// one, maldet did not complete a scan.
	stdout := string(res.Stdout) + string(res.Stderr)
	scanID := parseMaldetScanID(stdout)
	if scanID == "" {
		return capability.Result{}, errx.New(errx.KindUpstream, "scan_failed",
			"maldet did not report a scan id: "+logTail(res, 500))
	}

	findings := []map[string]any{}
	// No hits file means no hits. maldet only writes one when something matched,
	// so a missing file is a clean result rather than a failure.
	if b, err := c.FS.ReadFile(maldetSessDir + "/session.hits." + scanID); err == nil {
		findings = parseMaldetHits(string(b))
	}
	return capability.Result{Data: map[string]any{
		"findings": findings, "infected": len(findings) > 0, "scan_id": scanID,
	}}, nil
}

// parseMaldetScanID pulls the scan id out of maldet's output.
//
// It reads the "--report <id>" line rather than the "SCAN ID:" line because
// that is the form maldet prints consistently across versions, and because it
// is the id the session files are named by — which is what this is for.
func parseMaldetScanID(out string) string {
	m := reMaldetReportLine.FindStringSubmatch(out)
	if len(m) != 2 || !reMaldetScanID.MatchString(m[1]) {
		return ""
	}
	return m[1]
}

// parseMaldetHits parses a session.hits file.
//
// Each line is "<signature> : <path>", and with quarantine on it gains
// " => <quarantined path>". Quarantine is forced off above, so the arrow should
// never appear — it is stripped anyway, because a host that somehow ran with a
// different config should still yield the original path rather than a path
// inside maldet's quarantine that means nothing to the panel.
func parseMaldetHits(out string) []map[string]any {
	findings := []map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sig, rest, ok := strings.Cut(line, " : ")
		if !ok {
			continue
		}
		path := strings.TrimSpace(rest)
		if i := strings.Index(path, " => "); i >= 0 {
			path = strings.TrimSpace(path[:i])
		}
		if path == "" {
			continue
		}
		findings = append(findings, map[string]any{
			"path": path, "signature": strings.TrimSpace(sig),
		})
		if len(findings) >= maxScanFindings {
			break
		}
	}
	return findings
}

// ── maldet.update ────────────────────────────────────────────────────────────

// MaldetUpdate refreshes maldet's signature set (`maldet -u`).
type MaldetUpdate struct{}

// Name implements capability.Capability.
func (MaldetUpdate) Name() string { return "maldet.update" }

// Execute implements capability.Capability.
func (MaldetUpdate) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	if ok, _ := c.FS.Exists(maldetPath); !ok {
		return capability.Result{}, errx.New(errx.KindUnavailable, "maldet_not_installed",
			"maldet is not installed on this host.")
	}
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: maldetPath, Args: []string{"-u"}, Timeout: 10 * time.Minute,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "update_failed",
			"Could not update the maldet signatures.")
	}
	out := map[string]any{"updated": res.ExitCode == 0}
	if b, err := c.FS.ReadFile(maldetRoot + "/sigs/sigpack.ver"); err == nil {
		out["signature_version"] = strings.TrimSpace(string(b))
	}
	return capability.Result{Data: out}, nil
}

// ── maldet.install ───────────────────────────────────────────────────────────

// MaldetInstall downloads and installs maldet from rfxn.com.
//
// This is the one capability in the panel that fetches code and runs it as
// root, and it exists only because maldet ships no distribution package. Three
// things bound it:
//
//   - **The host is a constant.** Only https://www.rfxn.com. An
//     operator-supplied URL would turn a config file into arbitrary root code
//     execution, which is a far larger power than "install a scanner".
//   - **A configured checksum is enforced.** When npd sends one, a mismatch
//     aborts before anything is unpacked. There is nothing to verify against
//     otherwise: rfxn publishes no signature and no stable checksum for the
//     "current" tarball.
//   - **The observed hash is always returned.** An operator who installs once
//     unverified can pin that hash and have every later install checked against
//     it — and the audit log records what was actually run either way.
//
// Stated plainly because it is a real gap: this is TLS-and-a-hostname trust,
// which is weaker than the signed, key-pinned chain the panel uses for its own
// releases and for marketplace modules. It is the strongest thing maldet's
// distribution allows.
type MaldetInstall struct{}

type maldetInstallInput struct {
	// Path is the tarball's path on maldetHost, e.g.
	// "/downloads/maldetect-current.tar.gz". A full URL is rejected.
	Path string `json:"path"`
	// SHA256 is the expected hash, hex. Empty means unverified (allowed, and
	// reported back so it can be pinned).
	SHA256 string `json:"sha256"`
}

// Name implements capability.Capability.
func (MaldetInstall) Name() string { return "maldet.install" }

// reMaldetDownloadPath bounds the download path: one segment of ordinary
// characters under /downloads, ending .tar.gz. No traversal, no host, no query.
var reMaldetDownloadPath = regexp.MustCompile(`^/downloads/[A-Za-z0-9._-]{1,64}\.tar\.gz$`)

// Execute implements capability.Capability.
func (MaldetInstall) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in maldetInstallInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for maldet.install.")
	}
	if !reMaldetDownloadPath.MatchString(in.Path) {
		return capability.Result{}, errx.Validation("bad_path",
			"The maldet download path must be a plain /downloads/<name>.tar.gz path.")
	}
	if in.SHA256 != "" {
		if _, err := hex.DecodeString(in.SHA256); err != nil || len(in.SHA256) != 64 {
			return capability.Result{}, errx.Validation("bad_sha256",
				"The expected checksum must be 64 hex characters.")
		}
	}

	// A clean stage every time: a half-unpacked tree from a previous failure
	// must not be what install.sh runs.
	if err := c.FS.RemoveAll(maldetStageDir); err != nil {
		return capability.Result{}, errx.Upstream(err, "stage_failed",
			"Could not clear the maldet staging directory.")
	}
	if err := c.FS.MkdirAll(maldetStageDir, 0o700); err != nil {
		return capability.Result{}, errx.Upstream(err, "stage_failed",
			"Could not create the maldet staging directory.")
	}
	tarball := maldetStageDir + "/maldetect.tar.gz"

	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: curlPath,
		// --proto/--tlsv1.2 pin the transport: this is the only trust boundary
		// the download has, so a redirect to plain HTTP must not be followed.
		Args: []string{
			"-fsSL", "--proto", "=https", "--tlsv1.2",
			"-o", tarball, maldetHost + in.Path,
		},
		Timeout: 5 * time.Minute,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "download_failed",
			"Could not download maldet from "+maldetHost+".")
	}

	body, err := c.FS.ReadFile(tarball)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "download_failed",
			"The maldet download could not be read back.")
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if in.SHA256 != "" && !strings.EqualFold(got, in.SHA256) {
		_ = c.FS.RemoveAll(maldetStageDir)
		return capability.Result{}, errx.New(errx.KindUpstream, "checksum_mismatch",
			"The maldet download did not match the expected checksum; nothing was installed.")
	}

	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: tarPath, Args: []string{"-xzf", tarball, "-C", maldetStageDir, "--strip-components=1"},
		Timeout: 2 * time.Minute,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "extract_failed",
			"The maldet archive could not be extracted.")
	}

	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		// Run from inside the unpacked tree: maldet's installer resolves its own
		// files relatively and does not work from anywhere else.
		Path: shPath, Args: []string{"./install.sh"}, Dir: maldetStageDir,
		Timeout: 10 * time.Minute,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "install_failed",
			"The maldet installer failed: "+logTail(res, 500))
	}

	// The installer is what decides where maldet lands; confirm rather than
	// assume, so a successful-looking run that installed nothing is caught here
	// instead of at the first scan.
	if ok, _ := c.FS.Exists(maldetPath); !ok {
		return capability.Result{}, errx.New(errx.KindUpstream, "install_failed",
			"The maldet installer completed but "+maldetPath+" is not present.")
	}
	_ = c.FS.RemoveAll(maldetStageDir)

	return capability.Result{Data: map[string]any{
		"installed": true,
		// Returned so an operator who installed unverified can pin this hash and
		// have every later install checked against it.
		"sha256": got,
	}}, nil
}
