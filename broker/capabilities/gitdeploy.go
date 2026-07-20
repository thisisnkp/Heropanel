package capabilities

import (
	"encoding/json"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Absolute tool paths. Everything a deploy runs as the site user goes through
// `runuser -u <user> -- /usr/bin/env <VARS> <cmd>`, so the child sees a
// deterministic environment regardless of PAM, and there is no shell between the
// broker and git except the site owner's own build command (docs/11 §5).
const (
	runuserPath = "/usr/sbin/runuser"
	envPath     = "/usr/bin/env"
	gitPath     = "/usr/bin/git"
	shPath      = "/bin/sh"
	lnPath      = "/bin/ln"
	mvPath      = "/bin/mv"
	rmPath      = "/bin/rm"
	testPath    = "/usr/bin/test"
	lsPath      = "/bin/ls"
	// composerPath is where hp-installer places Composer. Pinned like every other
	// tool here: the deploy must not pick up whatever `composer` happens to be
	// first on a site user's PATH.
	composerPath = "/usr/local/bin/composer"
)

// reGitRef bounds branch names and relative subpaths: no whitespace, no shell
// metacharacters, safe to embed in a filesystem path and never mistaken for a
// CLI flag (must start with an alphanumeric).
var reGitRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)

// reReleaseID bounds the release directory name (a ULID the service generates).
var reReleaseID = regexp.MustCompile(`^[0-9A-Za-z]{8,64}$`)

// deployEnv is the minimal environment every deploy command runs with.
func deployEnv(home string) []string {
	return []string{
		"HOME=" + home,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"GIT_TERMINAL_PROMPT=0",
	}
}

// runAsUser executes argv as the given unprivileged Linux user via runuser, with
// a deterministic environment (and optional working directory) supplied through
// /usr/bin/env. Nothing here runs as root.
func runAsUser(c capability.Context, user, workDir string, env []string, timeout time.Duration, argv ...string) (exec.Result, error) {
	return runAsUserStdin(c, user, workDir, env, nil, timeout, argv...)
}

// runAsUserStdin is runAsUser with data piped to the child's stdin (e.g. the
// bytes tee writes to a file). Nothing here runs as root.
func runAsUserStdin(c capability.Context, user, workDir string, env []string, stdin []byte, timeout time.Duration, argv ...string) (exec.Result, error) {
	a := []string{"-u", user, "--", envPath}
	if workDir != "" {
		a = append(a, "-C", workDir)
	}
	a = append(a, env...)
	a = append(a, argv...)
	return c.Runner.Run(c.Ctx, exec.Command{Path: runuserPath, Args: a, Stdin: stdin, Timeout: timeout})
}

// logTail returns the last n bytes of combined output, for a bounded deploy log.
func logTail(res exec.Result, n int) string {
	b := append(append([]byte{}, res.Stdout...), res.Stderr...)
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}

// ── git.deploy ───────────────────────────────────────────────────────────────

// GitDeploy clones a branch into a fresh release directory, builds it as the
// site user, and atomically activates it. It performs no privileged action
// beyond dropping to the site's existing unprivileged uid.
type GitDeploy struct{}

func (GitDeploy) Name() string { return "git.deploy" }

type gitDeployInput struct {
	Username     string   `json:"username"`
	Home         string   `json:"home"`
	RepoURL      string   `json:"repo_url"`
	Branch       string   `json:"branch"`
	BuildCommand string   `json:"build_command"`
	WebRoot      string   `json:"web_root"`
	ReleaseID    string   `json:"release_id"`
	Keep         int      `json:"keep"`
	AutoComposer bool     `json:"auto_composer"`
	Auth         *gitAuth `json:"auth"`
}

func (GitDeploy) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in gitDeployInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for git.deploy.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidatePath(in.Home, c.Policy); err != nil {
		return capability.Result{}, err
	}
	authKind := authNone
	if in.Auth != nil && in.Auth.Kind != "" {
		authKind = in.Auth.Kind
	}
	if err := validateRepoURL(in.RepoURL, authKind); err != nil {
		return capability.Result{}, err
	}
	if err := validateGitRef(in.Branch, "branch"); err != nil {
		return capability.Result{}, err
	}
	if in.WebRoot != "" {
		if err := validateSubpath(in.WebRoot); err != nil {
			return capability.Result{}, err
		}
	}
	if len(in.BuildCommand) > 1000 || strings.ContainsRune(in.BuildCommand, '\x00') {
		return capability.Result{}, errx.Validation("invalid_build_command", "Invalid build command.")
	}
	if !reReleaseID.MatchString(in.ReleaseID) {
		return capability.Result{}, errx.Validation("invalid_release_id", "Invalid release id.")
	}

	home := path.Clean(in.Home)
	releases := home + "/releases"
	shared := home + "/shared"
	release := releases + "/" + in.ReleaseID
	current := home + "/current"
	currentTmp := home + "/.current.tmp"
	public := home + "/public"
	publicTmp := home + "/.public.tmp"
	// Defense in depth: every path we touch must stay within an allowed root.
	for _, p := range []string{releases, shared, release, current, currentTmp, public, publicTmp} {
		if err := capability.ValidatePath(p, c.Policy); err != nil {
			return capability.Result{}, err
		}
	}

	env := deployEnv(home)

	// 0. Stage the credential for a private repository. It is destroyed the moment
	//    the clone is done — see step 3b — and on every error path.
	cred, err := prepareGitAuth(c, in.Auth, in.Username, in.RepoURL)
	if err != nil {
		return capability.Result{}, err
	}
	defer cred.cleanup(c)

	// 1. Ensure releases/ and shared/ exist, owned by the site user (root
	//    creates them; git then writes the release as the user).
	for _, d := range []string{releases, shared} {
		res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path:    installPath,
			Args:    []string{"-d", "-m", "0750", "-o", in.Username, "-g", in.Username, d},
			Timeout: 20 * time.Second,
		})
		if err != nil {
			return capability.Result{}, errx.Upstream(err, "mkdir_failed", "Failed to prepare the release directory.")
		}
		if res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "mkdir_failed", "Preparing the release directory failed.")
		}
	}

	// 2. Clone the branch into the fresh release directory (as the site user).
	//    Any credential is referenced by path, never passed in argv.
	cloneArgs := append([]string{}, cred.gitArgs()...)
	cloneArgs = append(cloneArgs,
		"clone", "--depth", "1", "--single-branch", "--branch", in.Branch, "--", in.RepoURL, release)
	clone, err := runAsUser(c, in.Username, "", append(env, cred.gitEnv()...), 5*time.Minute, append([]string{gitPath}, cloneArgs...)...)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "clone_failed", "Could not run git clone.")
	}
	if clone.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "clone_failed", "git clone failed: "+logTail(clone, 500))
	}

	// 3. Resolve the deployed commit.
	rev, err := runAsUser(c, in.Username, release, env, 30*time.Second, gitPath, "rev-parse", "HEAD")
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "rev_parse_failed", "Could not resolve the commit.")
	}
	commit := strings.TrimSpace(string(rev.Stdout))

	// 3b. Destroy the credential before anything the site owner wrote gets to run.
	//     The build command executes as the site user, and the credential file is
	//     owned by that same user — so a build script could simply cat it and walk
	//     away with the operator's deploy token. Nothing past this point needs it.
	cred.cleanup(c)

	// 4. Composer, when the release actually has a composer.json. This runs before
	//    the build command so a build can rely on vendor/ being present, and it is
	//    what makes a plain `git clone` of a Laravel app runnable with no build
	//    command at all. --no-dev keeps dev tooling off a production box.
	var logs []string
	if in.AutoComposer {
		out, err := runComposer(c, in.Username, release, env)
		if err != nil {
			// Clean up the broken release; the live site is untouched (fail-safe).
			_, _ = runAsUser(c, in.Username, "", env, time.Minute, rmPath, "-rf", release)
			return capability.Result{}, err
		}
		if out != "" {
			logs = append(logs, "[composer]\n"+out)
		}
	}

	// 5. Optional build, in the release dir, as the site user.
	if in.BuildCommand != "" {
		b, err := runAsUser(c, in.Username, release, env, 15*time.Minute, shPath, "-lc", in.BuildCommand)
		if err != nil {
			return capability.Result{}, errx.Upstream(err, "build_failed", "Could not run the build command.")
		}
		out := logTail(b, 4000)
		if b.ExitCode != 0 {
			// Clean up the broken release; the live site is untouched (fail-safe).
			_, _ = runAsUser(c, in.Username, "", env, time.Minute, rmPath, "-rf", release)
			return capability.Result{}, errx.New(errx.KindUpstream, "build_failed", "The build command failed:\n"+out)
		}
		logs = append(logs, "[build]\n"+out)
	}
	buildLog := strings.Join(logs, "\n")

	// 6. Ensure public -> current/<web_root> exists (converted from the real dir
	//    on the first deploy only; a stable pointer thereafter).
	linkTarget := "current"
	if in.WebRoot != "" {
		linkTarget = "current/" + in.WebRoot
	}
	isSymlink, err := runAsUser(c, in.Username, "", env, 10*time.Second, testPath, "-L", public)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "activate_failed", "Could not inspect the document root.")
	}
	if isSymlink.ExitCode != 0 {
		steps := [][]string{
			{lnPath, "-sfn", linkTarget, publicTmp},
			{rmPath, "-rf", public},
			{mvPath, "-T", publicTmp, public},
		}
		for _, st := range steps {
			if err := mustRunAsUser(c, in.Username, env, 20*time.Second, st...); err != nil {
				return capability.Result{}, err
			}
		}
	}

	// 7. Atomic activation: create a temp symlink to the new release and rename
	//    it over `current`. rename(2) is atomic, so requests never see a partial
	//    swap.
	if err := mustRunAsUser(c, in.Username, env, 20*time.Second, lnPath, "-sfn", release, currentTmp); err != nil {
		return capability.Result{}, err
	}
	if err := mustRunAsUser(c, in.Username, env, 20*time.Second, mvPath, "-Tf", currentTmp, current); err != nil {
		return capability.Result{}, err
	}

	// 8. Prune superseded releases. Best-effort and last: the new release is
	//    already live, so a pruning hiccup must not fail a good deploy. Without
	//    this every deploy leaks a full checkout and the disk fills up quietly.
	pruned := pruneReleases(c, in.Username, releases, in.ReleaseID, env, in.Keep)

	return capability.Result{Data: map[string]any{
		"commit":    commit,
		"release":   release,
		"activated": true,
		"log":       buildLog,
		"pruned":    pruned,
	}}, nil
}

// runComposer installs a release's PHP dependencies when it has a composer.json.
// A release without one is not an error — most repos are not PHP — so the check
// comes first and a miss is simply a no-op.
//
// Composer is run as the site user with the release as both cwd and HOME's
// cache scope, so nothing it downloads escapes the site's own isolation.
func runComposer(c capability.Context, username, release string, env []string) (string, error) {
	has, err := runAsUser(c, username, "", env, 10*time.Second, testPath, "-f", release+"/composer.json")
	if err != nil {
		return "", errx.Upstream(err, "composer_failed", "Could not inspect the release for composer.json.")
	}
	if has.ExitCode != 0 {
		return "", nil // not a Composer project
	}

	res, err := runAsUser(c, username, release, env, 15*time.Minute, composerPath,
		"install", "--no-interaction", "--no-progress", "--prefer-dist",
		"--no-dev", "--optimize-autoloader")
	if err != nil {
		return "", errx.Upstream(err, "composer_failed",
			"Could not run Composer. Is it installed at "+composerPath+"?")
	}
	out := logTail(res, 4000)
	if res.ExitCode != 0 {
		return "", errx.New(errx.KindUpstream, "composer_failed", "composer install failed:\n"+out)
	}
	return out, nil
}

// pruneReleases deletes all but the newest `keep` release directories, never
// touching the one just activated. Release ids are ULIDs, so a lexical sort is a
// chronological sort. Returns the number removed.
func pruneReleases(c capability.Context, username, releasesDir, keepID string, env []string, keep int) int {
	if keep <= 0 {
		return 0
	}
	ls, err := runAsUser(c, username, "", env, 30*time.Second, lsPath, "-1", releasesDir)
	if err != nil || ls.ExitCode != 0 {
		return 0
	}
	var ids []string
	for _, line := range strings.Split(string(ls.Stdout), "\n") {
		id := strings.TrimSpace(line)
		// Ignore anything that is not one of our own release directories.
		if id != "" && id != keepID && reReleaseID.MatchString(id) {
			ids = append(ids, id)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))

	// The live release counts against the budget, hence keep-1 of the rest.
	retain := keep - 1
	if retain < 0 {
		retain = 0
	}
	if len(ids) <= retain {
		return 0
	}
	n := 0
	for _, id := range ids[retain:] {
		dir := releasesDir + "/" + id
		if err := capability.ValidatePath(dir, c.Policy); err != nil {
			continue
		}
		if res, err := runAsUser(c, username, "", env, time.Minute, rmPath, "-rf", dir); err == nil && res.ExitCode == 0 {
			n++
		}
	}
	return n
}

// ── git.rollback ─────────────────────────────────────────────────────────────

// GitRollback repoints the live release symlink at a previous release directory.
// No rebuild; just the atomic swap.
type GitRollback struct{}

func (GitRollback) Name() string { return "git.rollback" }

type gitRollbackInput struct {
	Username   string `json:"username"`
	Home       string `json:"home"`
	ReleaseDir string `json:"release_dir"`
}

func (GitRollback) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in gitRollbackInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for git.rollback.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidatePath(in.Home, c.Policy); err != nil {
		return capability.Result{}, err
	}
	home := path.Clean(in.Home)
	release := path.Clean(in.ReleaseDir)
	if err := capability.ValidatePath(release, c.Policy); err != nil {
		return capability.Result{}, err
	}
	// The target must be one of this site's own releases.
	if !strings.HasPrefix(release, home+"/releases/") {
		return capability.Result{}, errx.Validation("invalid_release", "The release is not part of this site.")
	}

	env := deployEnv(home)
	current := home + "/current"
	currentTmp := home + "/.current.tmp"

	// Refuse to activate a release that no longer exists on disk.
	exists, err := runAsUser(c, in.Username, "", env, 10*time.Second, testPath, "-d", release)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "rollback_failed", "Could not inspect the release.")
	}
	if exists.ExitCode != 0 {
		return capability.Result{}, errx.NotFound("release_missing", "That release no longer exists on disk.")
	}

	if err := mustRunAsUser(c, in.Username, env, 20*time.Second, lnPath, "-sfn", release, currentTmp); err != nil {
		return capability.Result{}, err
	}
	if err := mustRunAsUser(c, in.Username, env, 20*time.Second, mvPath, "-Tf", currentTmp, current); err != nil {
		return capability.Result{}, err
	}

	return capability.Result{Data: map[string]any{"activated": true, "release": release}}, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// mustRunAsUser runs a command as the site user and turns a non-zero exit or a
// runner error into a structured error.
func mustRunAsUser(c capability.Context, user string, env []string, timeout time.Duration, argv ...string) error {
	res, err := runAsUser(c, user, "", env, timeout, argv...)
	if err != nil {
		return errx.Upstream(err, "activate_failed", "A deploy filesystem step failed.")
	}
	if res.ExitCode != 0 {
		return errx.New(errx.KindUpstream, "activate_failed", "A deploy filesystem step failed: "+logTail(res, 300))
	}
	return nil
}

// reSCPLike matches git's scp-style remote (`[user@]host:path`), the form used
// for SSH clones. The path may be relative (`git@github.com:owner/repo.git`) or
// absolute (`git@host:/srv/git/repo.git`). Kept strict otherwise: no whitespace,
// no shell metacharacters, and no path that starts by climbing.
var reSCPLike = regexp.MustCompile(`^(?:[A-Za-z0-9._-]+@)?[A-Za-z0-9.-]+:/?[A-Za-z0-9][A-Za-z0-9._/~-]{0,254}$`)

// validateRepoURL mirrors the service check (defense in depth — the broker never
// trusts hpd's validation): https:// for public and token clones, an SSH remote
// only when a deploy key was supplied. Credentials embedded in the URL are
// refused outright; they would end up in argv, which every user on the box can
// read.
func validateRepoURL(raw, authKind string) error {
	invalid := errx.Validation("invalid_repo_url", "A valid repository URL is required.")
	if raw == "" || len(raw) > 512 || strings.HasPrefix(raw, "-") || strings.ContainsAny(raw, " \t\r\n") {
		return invalid
	}
	if authKind == authSSHKey {
		if reSCPLike.MatchString(raw) && !strings.Contains(raw, "..") {
			return nil
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "ssh" || u.Host == "" || u.Path == "" || strings.Contains(u.Path, "..") {
			return invalid
		}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path == "" || u.User != nil {
		return invalid
	}
	return nil
}

func validateGitRef(s, field string) error {
	if !reGitRef.MatchString(s) || strings.Contains(s, "..") {
		return errx.Validation("invalid_"+field, "Invalid "+field+".")
	}
	return nil
}

func validateSubpath(s string) error {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "-") || strings.Contains(s, "..") || !reGitRef.MatchString(s) {
		return errx.Validation("invalid_web_root", "Invalid web root.")
	}
	return nil
}
