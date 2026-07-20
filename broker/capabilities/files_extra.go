package capabilities

import (
	"encoding/json"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// The rest of the File Manager's operations: creating archives, repairing
// ownership, and searching a tree. Like the ops in files.go, each is
// path-confined and — with one deliberate exception documented on FileChown —
// runs as the site's own Linux user.

const (
	zipPath  = "/usr/bin/zip"
	grepPath = "/bin/grep"
)

// searchMaxResults bounds a search so a pathological pattern cannot stream an
// unbounded result set back through a single broker frame.
const searchMaxResults = 500

// ── file.compress ────────────────────────────────────────────────────────────

// FileCompress creates an archive from one or more site-relative entries, as the
// site user. It is the counterpart to FileExtract: it is what makes "download
// this folder" possible at all, since HTTP hands back one file, not a tree.
type FileCompress struct{}

func (FileCompress) Name() string { return "file.compress" }

func (FileCompress) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root     string   `json:"root"`
		Sources  []string `json:"sources"` // site-relative entries to include
		Archive  string   `json:"archive"` // site-relative destination
		Username string   `json:"username"`
		Format   string   `json:"format"` // zip | tar.gz (default from the archive suffix)
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for file.compress.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	if len(in.Sources) == 0 {
		return capability.Result{}, errx.Validation("no_sources", "Nothing was selected to compress.")
	}
	archive, err := confinedFilePath(in.Root, in.Archive, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}

	// Every source is confined, then expressed *relative to its parent* so the
	// archive contains "assets/logo.png", not "srv/heropanel/sites/1/assets/…".
	// The tool is run with -C / a working directory, never given absolute paths.
	workDir, err := confinedFilePath(in.Root, parentOf(in.Sources[0]), c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	names := make([]string, 0, len(in.Sources))
	for _, src := range in.Sources {
		abs, err := confinedFilePath(in.Root, src, c.Policy)
		if err != nil {
			return capability.Result{}, err
		}
		// All sources must share one parent, so a single archive root makes sense
		// and no traversal is needed to reach any of them.
		if path.Dir(abs) != workDir {
			return capability.Result{}, errx.Validation("mixed_parents",
				"All items in one archive must come from the same folder.")
		}
		if abs == archive {
			return capability.Result{}, errx.Validation("archive_in_sources",
				"The archive cannot be one of the items being compressed.")
		}
		names = append(names, path.Base(abs))
	}

	format := strings.ToLower(in.Format)
	if format == "" {
		format = formatFromSuffix(archive)
	}
	var argv []string
	switch format {
	case "zip":
		// -r recurse, -q quiet, -X drop extra platform attributes.
		argv = append([]string{zipPath, "-rqX", archive}, names...)
	case "tar.gz", "tgz":
		argv = append([]string{tarPath, "-czf", archive, "--"}, names...)
	default:
		return capability.Result{}, errx.Validation("unsupported_format",
			"Archives can be created as .zip or .tar.gz.")
	}

	res, err := runAsUser(c, in.Username, workDir, nil, 10*time.Minute, argv...)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "compress_failed", "Could not create the archive.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "compress_failed",
			"Creating the archive returned a non-zero exit code: "+logTail(res, 400))
	}
	return capability.Result{Data: map[string]any{"archive": archive, "items": len(names)}}, nil
}

func formatFromSuffix(archive string) string {
	l := strings.ToLower(archive)
	switch {
	case strings.HasSuffix(l, ".zip"):
		return "zip"
	case strings.HasSuffix(l, ".tar.gz"), strings.HasSuffix(l, ".tgz"):
		return "tar.gz"
	}
	return ""
}

func parentOf(rel string) string {
	d := path.Dir(path.Clean("/" + rel))
	return strings.TrimPrefix(d, "/")
}

// ── file.copy ────────────────────────────────────────────────────────────────

// FileCopy duplicates a path (recursively) to another location inside the site
// root, as the site user.
//
// It is the missing half of copy/cut/paste. *Moving* needs no capability of its
// own — file.rename already confines both ends, so moving into another folder is
// a rename with a destination elsewhere in the tree — but duplicating is not
// expressible any other way.
//
// Whether the destination already exists is deliberately *not* decided here: the
// caller (internal/files) checks and either refuses or picks a free name, which
// is where the "don't silently destroy the operator's data" rule belongs. This
// capability's job is the narrower one — copy exactly the two confined paths it
// was given, as the right uid.
type FileCopy struct{}

func (FileCopy) Name() string { return "file.copy" }

func (FileCopy) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root     string `json:"root"`
		From     string `json:"from"`
		To       string `json:"to"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for file.copy.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	from, err := confinedFilePath(in.Root, in.From, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	to, err := confinedFilePath(in.Root, in.To, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	if from == to {
		return capability.Result{}, errx.Validation("same_path",
			"The source and the destination are the same path.")
	}
	// Copying a folder into its own subtree recurses until the disk fills. GNU cp
	// catches the direct case, but the check is one comparison and the failure it
	// prevents is expensive.
	if strings.HasPrefix(to, from+"/") {
		return capability.Result{}, errx.Validation("copy_into_self",
			"A folder cannot be copied into itself.")
	}
	// -a recurses and preserves mode/timestamps; -T means "the destination *is*
	// this path", never "copy into this directory", so the result lands exactly on
	// the path that was confined above rather than one level inside it.
	res, err := runAsUser(c, in.Username, "", nil, 10*time.Minute, cpPath, "-aT", "--", from, to)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "copy_failed", "Could not copy the path.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "copy_failed",
			"Copying returned a non-zero exit code: "+logTail(res, 400))
	}
	return capability.Result{Data: map[string]any{"from": from, "to": to}}, nil
}

// ── file.chown ───────────────────────────────────────────────────────────────

// FileChown resets ownership of a path (recursively) to the site's own user.
//
// This is the one file operation that runs as **root**, because changing a
// file's owner is a root-only operation on Linux — a site user cannot give a
// file away, nor take one. That makes it the one place where the "run as the
// site user" rule cannot hold, so the *target* is constrained instead: the new
// owner is always `<site-user>:<site-user>`, taken from the same validated
// username the rest of the module uses. There is no way to express "chown to
// root", "chown to another site", or any other account — which is what keeps a
// root-run operation from becoming a privilege-escalation primitive.
//
// It exists because ownership does drift in practice (an archive extracted with
// stored uids, a file restored from a backup), and when it does the site's own
// processes stop being able to read their own files.
type FileChown struct{}

func (FileChown) Name() string { return "file.chown" }

func (FileChown) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root, Path, Username string
	}
	if err := decodeRPU(raw, &in.Root, &in.Path, &in.Username); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	// Belt and braces: the username is already allowlist-validated, but this is a
	// root-run chown and "root" must never be constructible as a target.
	if in.Username == "root" {
		return capability.Result{}, errx.Forbidden("root_owner_refused",
			"Ownership cannot be assigned to root.")
	}
	target, err := confinedFilePath(in.Root, in.Path, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	// -h acts on symlinks themselves rather than following them, so a symlink
	// planted inside the tree cannot redirect the chown at a file outside it.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    chownPath,
		Args:    []string{"-Rh", in.Username + ":" + in.Username, "--", target},
		Timeout: 5 * time.Minute,
	})
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.Upstream(err, "chown_failed", "Could not repair ownership.")
	}
	return capability.Result{Data: map[string]any{"path": target, "owner": in.Username}}, nil
}

// ── file.search ──────────────────────────────────────────────────────────────

// FileSearch walks a subtree as the site user looking for entries whose name
// matches a pattern, or (mode=content) files containing a string. Results are
// capped and the walk is time-bounded: a search is a convenience, and must not
// become a way to pin a core at 100% or to blow the frame budget.
type FileSearch struct{}

func (FileSearch) Name() string { return "file.search" }

func (FileSearch) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root     string `json:"root"`
		Path     string `json:"path"`
		Username string `json:"username"`
		Query    string `json:"query"`
		Mode     string `json:"mode"` // name (default) | content
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for file.search.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return capability.Result{}, errx.Validation("empty_query", "A search term is required.")
	}
	if len(q) > 200 {
		return capability.Result{}, errx.Validation("query_too_long", "The search term is too long.")
	}
	dir, err := confinedFilePath(in.Root, in.Path, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}

	var argv []string
	if strings.EqualFold(in.Mode, "content") {
		// -r recurse, -l names only, -I skip binaries, -F fixed string (never a
		// regex, so a user's search box cannot become a ReDoS), -s silence errors.
		argv = []string{grepPath, "-rlIFs", "--", q, dir}
	} else {
		// -iname is a glob, so the term is wrapped in stars for a "contains" match.
		// The pattern is a single argv element; no shell expands it.
		argv = []string{findPath, dir, "-maxdepth", "12", "-iname", "*" + q + "*",
			"-printf", `%y\t%s\t%m\t%T@\t%p\n`}
	}

	res, err := runAsUser(c, in.Username, "", nil, 60*time.Second, argv...)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "search_failed", "The search could not be run.")
	}
	// grep exits 1 when nothing matched, and find can exit non-zero on an
	// unreadable subdirectory. Neither is an error worth failing the request for —
	// partial results are the useful answer.
	entries := parseSearchResults(string(res.Stdout), dir, strings.EqualFold(in.Mode, "content"))
	truncated := false
	if len(entries) > searchMaxResults {
		entries = entries[:searchMaxResults]
		truncated = true
	}
	return capability.Result{Data: map[string]any{
		"path": dir, "entries": entries, "truncated": truncated,
	}}, nil
}

// parseSearchResults converts find/grep output into listing-shaped entries whose
// paths are relative to the searched root, so the UI can navigate to them.
func parseSearchResults(out, dir string, contentMode bool) []map[string]any {
	entries := []map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if contentMode {
			// grep -l prints one absolute path per line.
			entries = append(entries, map[string]any{
				"name": path.Base(line),
				"path": relativeTo(dir, line),
				"kind": "file",
			})
			continue
		}
		f := strings.SplitN(line, "\t", 5)
		if len(f) != 5 {
			continue
		}
		size, _ := strconv.ParseInt(f[1], 10, 64)
		mtime := f[3]
		if i := strings.IndexByte(mtime, '.'); i >= 0 {
			mtime = mtime[:i]
		}
		mt, _ := strconv.ParseInt(mtime, 10, 64)
		full := f[4]
		if full == dir {
			continue // the search root itself is not a result
		}
		entries = append(entries, map[string]any{
			"name":  path.Base(full),
			"path":  relativeTo(dir, full),
			"kind":  findType(f[0]),
			"size":  size,
			"mode":  f[2],
			"mtime": mt,
		})
	}
	return entries
}

func relativeTo(dir, full string) string {
	return strings.TrimPrefix(strings.TrimPrefix(full, dir), "/")
}
