package capabilities

import (
	"encoding/json"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// The File Manager capabilities. Every one runs as the *site's* Linux user (via
// runAsUser), never as root: that is what actually contains the blast radius. A
// symlink under the site root pointing at /etc/shadow is only as reachable as
// the site user's own uid allows (i.e. not at all), and a write cannot escape
// the tree the user owns. Path confinement (below) is the second, belt-and-
// suspenders layer — it clamps traversal so a request can never even *name* a
// path outside the site root.
//
// I/O is chunked because the broker's wire framing caps a frame at 1 MiB
// (pkg/brokerwire): reads take an offset+length, writes take an append flag, so
// hpd streams large files by looping rather than trying to move them in one
// frame.

const (
	findPath  = "/usr/bin/find"
	teePath   = "/usr/bin/tee"
	unzipPath = "/usr/bin/unzip"
	ddPath    = "/bin/dd"
	mkdirPath = "/bin/mkdir"
	tarPath   = "/bin/tar"
	// chmodPath is declared in dbdump.go (same package).
)

// fileChunkMax bounds a single read/write chunk. It sits well under the 1 MiB
// wire cap so the base64 expansion (4/3) of returned bytes plus the JSON
// envelope still fits in a frame.
const fileChunkMax = 512 * 1024

var reFileMode = regexp.MustCompile(`^[0-7]{3,4}$`)

// confinedFilePath clamps a site-relative path under the site root. The clamp
// itself lives in capability.ConfinedPath, shared with the interactive terminal
// so there is exactly one implementation of this security boundary.
func confinedFilePath(root, rel string, p policy.Policy) (string, error) {
	return capability.ConfinedPath(root, rel, p)
}

// ── file.list ────────────────────────────────────────────────────────────────

// FileList lists one directory (non-recursive) as the site user.
type FileList struct{}

func (FileList) Name() string { return "file.list" }

func (FileList) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root     string `json:"root"`
		Path     string `json:"path"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for file.list.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	dir, err := confinedFilePath(in.Root, in.Path, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	// -printf gives a stable, parseable record per entry: type, size, octal mode,
	// mtime (epoch), name. type is f/d/l/… from %y. -mindepth 1 excludes the dir
	// itself; -maxdepth 1 keeps it non-recursive.
	res, err := runAsUser(c, in.Username, "", nil, 30*time.Second,
		findPath, dir, "-maxdepth", "1", "-mindepth", "1",
		"-printf", `%y\t%s\t%m\t%T@\t%f\n`)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "list_failed", "Could not list the directory.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.NotFound("not_a_directory", "The path is not a listable directory.")
	}
	entries := parseFindListing(string(res.Stdout))
	return capability.Result{Data: map[string]any{"path": dir, "entries": entries}}, nil
}

func parseFindListing(out string) []map[string]any {
	entries := []map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 5)
		if len(f) != 5 {
			continue
		}
		size, _ := strconv.ParseInt(f[1], 10, 64)
		mtime := f[3]
		if i := strings.IndexByte(mtime, '.'); i >= 0 { // %T@ is a float; keep whole seconds
			mtime = mtime[:i]
		}
		mt, _ := strconv.ParseInt(mtime, 10, 64)
		entries = append(entries, map[string]any{
			"name":  f[4],
			"kind":  findType(f[0]),
			"size":  size,
			"mode":  f[2],
			"mtime": mt,
		})
	}
	return entries
}

func findType(y string) string {
	switch y {
	case "d":
		return "dir"
	case "l":
		return "symlink"
	case "f":
		return "file"
	default:
		return "other"
	}
}

// ── file.read ────────────────────────────────────────────────────────────────

// FileRead reads up to fileChunkMax bytes from a file at a byte offset, as the
// site user. hpd loops over offsets to stream a whole file.
type FileRead struct{}

func (FileRead) Name() string { return "file.read" }

func (FileRead) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root     string `json:"root"`
		Path     string `json:"path"`
		Username string `json:"username"`
		Offset   int64  `json:"offset"`
		Length   int    `json:"length"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for file.read.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	if in.Offset < 0 {
		return capability.Result{}, errx.Validation("bad_offset", "Offset must be non-negative.")
	}
	length := in.Length
	if length <= 0 || length > fileChunkMax {
		length = fileChunkMax
	}
	file, err := confinedFilePath(in.Root, in.Path, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	// Read `length` bytes from byte offset `offset`. skip_bytes makes skip a byte
	// count; fullblock makes dd accumulate a whole bs-sized block (short reads are
	// retried) so one `count=1` block is exactly `length` bytes — or fewer at EOF.
	// (count_bytes must NOT be used here: it would reinterpret count=1 as a single
	// byte, truncating every read to one byte.) status=none keeps dd's stats off
	// stdout so the payload is exactly the file bytes.
	res, err := runAsUser(c, in.Username, "", nil, 30*time.Second, ddPath,
		"if="+file, "bs="+strconv.Itoa(length), "count=1",
		"skip="+strconv.FormatInt(in.Offset, 10),
		"iflag=skip_bytes,fullblock", "status=none")
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "read_failed", "Could not read the file.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.NotFound("read_failed", "The file could not be read (missing, a directory, or not permitted).")
	}
	// A short read means we reached end of file. content is []byte → base64 in the
	// JSON envelope, so binary is safe.
	return capability.Result{Data: map[string]any{
		"content": res.Stdout,
		"read":    len(res.Stdout),
		"offset":  in.Offset,
		"eof":     len(res.Stdout) < length,
	}}, nil
}

// ── file.write ───────────────────────────────────────────────────────────────

// FileWrite writes a chunk to a file as the site user. append=false truncates
// then writes (editor save / first upload chunk); append=true appends (later
// upload chunks) — so hpd streams an upload by looping.
type FileWrite struct{}

func (FileWrite) Name() string { return "file.write" }

func (FileWrite) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root     string `json:"root"`
		Path     string `json:"path"`
		Username string `json:"username"`
		Content  []byte `json:"content"` // base64 on the wire
		Append   bool   `json:"append"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for file.write.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	if len(in.Content) > fileChunkMax {
		return capability.Result{}, errx.Validation("chunk_too_large", "Write chunk exceeds the maximum size.")
	}
	file, err := confinedFilePath(in.Root, in.Path, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	// tee runs as the site user and writes stdin to the file: truncating by
	// default, appending with -a. Running as the user is what makes a symlink to a
	// file the user cannot write simply fail, rather than letting root clobber it.
	argv := []string{teePath}
	if in.Append {
		argv = append(argv, "-a")
	}
	argv = append(argv, "--", file)
	res, err := runAsUserStdin(c, in.Username, "", nil, in.Content, 60*time.Second, argv...)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "write_failed", "Could not write the file.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "write_failed", "Writing the file returned a non-zero exit code.")
	}
	return capability.Result{Data: map[string]any{"path": file, "written": len(in.Content)}}, nil
}

// ── file.mkdir ───────────────────────────────────────────────────────────────

// FileMkdir creates a directory (and parents) as the site user.
type FileMkdir struct{}

func (FileMkdir) Name() string { return "file.mkdir" }

func (FileMkdir) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root, Path, Username string
	}
	if err := decodeRPU(raw, &in.Root, &in.Path, &in.Username); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	dir, err := confinedFilePath(in.Root, in.Path, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	res, err := runAsUser(c, in.Username, "", nil, 20*time.Second, mkdirPath, "-p", "--", dir)
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.Upstream(err, "mkdir_failed", "Could not create the directory.")
	}
	return capability.Result{Data: map[string]any{"path": dir}}, nil
}

// ── file.remove ──────────────────────────────────────────────────────────────

// FileRemove deletes a file or directory tree as the site user. It refuses to
// delete the site root itself.
type FileRemove struct{}

func (FileRemove) Name() string { return "file.remove" }

func (FileRemove) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root, Path, Username string
	}
	if err := decodeRPU(raw, &in.Root, &in.Path, &in.Username); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	target, err := confinedFilePath(in.Root, in.Path, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	if target == path.Clean(in.Root) {
		return capability.Result{}, errx.Validation("refuse_root", "The site root cannot be deleted here.")
	}
	res, err := runAsUser(c, in.Username, "", nil, 2*time.Minute, rmPath, "-rf", "--", target)
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.Upstream(err, "remove_failed", "Could not remove the path.")
	}
	return capability.Result{Data: map[string]any{"removed": target}}, nil
}

// ── file.rename ──────────────────────────────────────────────────────────────

// FileRename moves/renames within the site root as the site user.
type FileRename struct{}

func (FileRename) Name() string { return "file.rename" }

func (FileRename) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root     string `json:"root"`
		From     string `json:"from"`
		To       string `json:"to"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for file.rename.")
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
	// -T treats the destination as a normal file (never "move into dir"), so a
	// rename cannot accidentally nest under an existing directory of the same name.
	res, err := runAsUser(c, in.Username, "", nil, 30*time.Second, mvPath, "-Tf", "--", from, to)
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.Upstream(err, "rename_failed", "Could not move the path.")
	}
	return capability.Result{Data: map[string]any{"from": from, "to": to}}, nil
}

// ── file.chmod ───────────────────────────────────────────────────────────────

// FileChmod changes a path's mode (octal) as the site user.
type FileChmod struct{}

func (FileChmod) Name() string { return "file.chmod" }

func (FileChmod) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root     string `json:"root"`
		Path     string `json:"path"`
		Username string `json:"username"`
		Mode     string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for file.chmod.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	if !reFileMode.MatchString(in.Mode) {
		return capability.Result{}, errx.Validation("bad_mode", "Mode must be 3–4 octal digits.")
	}
	target, err := confinedFilePath(in.Root, in.Path, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	res, err := runAsUser(c, in.Username, "", nil, 20*time.Second, chmodPath, in.Mode, "--", target)
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.Upstream(err, "chmod_failed", "Could not change the mode.")
	}
	return capability.Result{Data: map[string]any{"path": target, "mode": in.Mode}}, nil
}

// ── file.extract ─────────────────────────────────────────────────────────────

// FileExtract unpacks an archive into a destination directory, as the site user.
// The archive kind is decided by the filename suffix and restricted to zip/tar.
type FileExtract struct{}

func (FileExtract) Name() string { return "file.extract" }

func (FileExtract) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Root     string `json:"root"`
		Archive  string `json:"archive"`
		Dest     string `json:"dest"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for file.extract.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	archive, err := confinedFilePath(in.Root, in.Archive, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	dest, err := confinedFilePath(in.Root, in.Dest, c.Policy)
	if err != nil {
		return capability.Result{}, err
	}
	// Make sure the destination exists (as the user), then extract into it.
	if res, err := runAsUser(c, in.Username, "", nil, 20*time.Second, mkdirPath, "-p", "--", dest); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.Upstream(err, "extract_mkdir_failed", "Could not create the extract destination.")
	}

	lower := strings.ToLower(archive)
	var argv []string
	switch {
	case strings.HasSuffix(lower, ".zip"):
		// -o overwrite, -q quiet; unzip confines to -d and does not follow to
		// absolute paths in the archive.
		argv = []string{unzipPath, "-o", "-q", archive, "-d", dest}
	case strings.HasSuffix(lower, ".tar"), strings.HasSuffix(lower, ".tar.gz"),
		strings.HasSuffix(lower, ".tgz"), strings.HasSuffix(lower, ".tar.bz2"),
		strings.HasSuffix(lower, ".tar.xz"):
		// tar auto-detects compression (-a not needed for read); -C sets the dir.
		argv = []string{tarPath, "-xf", archive, "-C", dest}
	default:
		return capability.Result{}, errx.Validation("unsupported_archive", "Only .zip and .tar[.gz|.bz2|.xz] archives can be extracted.")
	}
	res, err := runAsUser(c, in.Username, "", nil, 5*time.Minute, argv...)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "extract_failed", "Could not extract the archive.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "extract_failed",
			"Extracting the archive returned a non-zero exit code: "+logTail(res, 400))
	}
	return capability.Result{Data: map[string]any{"archive": archive, "dest": dest}}, nil
}

// decodeRPU decodes the common {root, path, username} shape into three strings.
func decodeRPU(raw json.RawMessage, root, pth, user *string) error {
	var in struct {
		Root     string `json:"root"`
		Path     string `json:"path"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return errx.Validation("bad_input", "Invalid input for the file operation.")
	}
	*root, *pth, *user = in.Root, in.Path, in.Username
	return nil
}
