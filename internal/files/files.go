// Package files is the in-core File Manager: it lets an operator browse, edit,
// upload, and organise a site's files. Every operation is performed by the
// privileged broker *as the site's Linux user* and confined to the site home, so
// the panel (which runs unprivileged, in none of the sites' groups) never reads
// or writes a customer's files directly, and one site can never reach another's.
//
// It is deliberately baremetal-only: a git- or docker-managed site's content is
// owned by its deploy pipeline, and editing a live release out from under an
// atomic-swap deploy would corrupt exactly the guarantee those modes exist to
// provide. The gate lives in resolveEditable.
package files

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// deployBaremetal is the only deploy mode the File Manager operates on. It is
// duplicated here (rather than imported from internal/site) so this package does
// not depend on the site package's types.
const deployBaremetal = "baremetal"

// ErrPathRequired is returned when a file operation that targets a specific path
// (read, write, delete) is called with an empty path. Listing the site root is
// allowed with an empty path, but naming no file to read or write is not.
var ErrPathRequired = errx.Validation("path_required", "A file path is required.")

// Entry is one directory entry.
type Entry struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`  // file | dir | symlink | other
	Size  int64  `json:"size"`  // bytes
	Mode  string `json:"mode"`  // octal permission bits, e.g. "644"
	Mtime int64  `json:"mtime"` // modification time, epoch seconds
}

// Listing is a non-recursive directory listing.
type Listing struct {
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

// SiteRef is the identity + paths a file operation needs, resolved by UID.
type SiteRef struct {
	ID         int64
	UID        string
	LinuxUser  string
	HomeDir    string
	DeployMode string
}

// Sites resolves a site UID to its identity and paths.
type Sites interface {
	Resolve(ctx context.Context, siteUID string) (*SiteRef, error)
}

// Broker is the privileged gateway (a subset of internal/broker.Gateway).
type Broker interface {
	Invoke(ctx context.Context, capability string, input any) (map[string]any, error)
}

// Service is the File Manager application service.
type Service struct {
	sites  Sites
	broker Broker
}

// NewService constructs the File Manager service.
func NewService(sites Sites, broker Broker) *Service {
	return &Service{sites: sites, broker: broker}
}

// ChunkSize is the read/write chunk hpd streams with; it matches the broker's
// per-chunk cap so a single frame always fits the wire.
const ChunkSize = 512 * 1024

// resolveEditable resolves a site and enforces the baremetal-only gate.
func (s *Service) resolveEditable(ctx context.Context, siteUID string) (*SiteRef, error) {
	if s.broker == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The File Manager requires the privileged broker, which is not configured.")
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	if ref.DeployMode != deployBaremetal {
		return nil, errx.Forbidden("not_baremetal",
			"The File Manager is available only for baremetal sites; git- and docker-managed sites are managed through their own pipelines.")
	}
	if ref.LinuxUser == "" || ref.HomeDir == "" {
		return nil, errx.New(errx.KindUnavailable, "site_not_provisioned",
			"This site is not fully provisioned yet.")
	}
	return ref, nil
}

func (s *Service) base(ref *SiteRef, path string) map[string]any {
	return map[string]any{"root": ref.HomeDir, "path": path, "username": ref.LinuxUser}
}

// List returns a directory listing relative to the site home.
func (s *Service) List(ctx context.Context, siteUID, path string) (*Listing, error) {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	res, err := s.broker.Invoke(ctx, "file.list", s.base(ref, path))
	if err != nil {
		return nil, err
	}
	return listingFromResult(res), nil
}

// Read returns one chunk of a file (up to length bytes from offset) and whether
// end of file was reached, so the caller can stream.
func (s *Service) Read(ctx context.Context, siteUID, path string, offset int64, length int) (content []byte, eof bool, err error) {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return nil, false, err
	}
	return s.readChunk(ctx, ref, path, offset, length)
}

// Write writes a chunk to a file. append=false truncates (a fresh save or the
// first upload chunk); append=true appends (subsequent upload chunks).
func (s *Service) Write(ctx context.Context, siteUID, path string, content []byte, appendTo bool) error {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return err
	}
	return s.writeChunk(ctx, ref, path, content, appendTo)
}

// readChunk performs one file.read against an already-resolved site. Kept
// private so Download can loop it without re-resolving the site each chunk.
func (s *Service) readChunk(ctx context.Context, ref *SiteRef, path string, offset int64, length int) (content []byte, eof bool, err error) {
	in := s.base(ref, path)
	in["offset"] = offset
	in["length"] = length
	res, err := s.broker.Invoke(ctx, "file.read", in)
	if err != nil {
		return nil, false, err
	}
	content = decodeContent(res["content"])
	eof, _ = res["eof"].(bool)
	return content, eof, nil
}

// writeChunk performs one file.write against an already-resolved site.
func (s *Service) writeChunk(ctx context.Context, ref *SiteRef, path string, content []byte, appendTo bool) error {
	in := s.base(ref, path)
	in["content"] = content // marshalled to base64 on the wire; binary-safe
	in["append"] = appendTo
	_, err := s.broker.Invoke(ctx, "file.write", in)
	return err
}

// Download streams a whole file to w by looping file.read over successive
// offsets until end of file, resolving the site once. It returns the number of
// bytes written. Chunking keeps every broker frame under the 1 MiB wire cap.
func (s *Service) Download(ctx context.Context, siteUID, path string, w io.Writer) (int64, error) {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return 0, err
	}
	var total int64
	for {
		chunk, eof, err := s.readChunk(ctx, ref, path, total, ChunkSize)
		if err != nil {
			return total, err
		}
		if len(chunk) > 0 {
			n, werr := w.Write(chunk)
			total += int64(n)
			if werr != nil {
				return total, werr
			}
		}
		if eof {
			return total, nil
		}
	}
}

// Upload streams r into a file, truncating it first: the first chunk is a
// truncating write and the rest append, so a re-upload replaces the file rather
// than growing it. An empty body still truncates the file to zero length. It
// returns the number of bytes written and resolves the site once.
func (s *Service) Upload(ctx context.Context, siteUID, path string, r io.Reader) (int64, error) {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return 0, err
	}
	buf := make([]byte, ChunkSize)
	var total int64
	first := true
	for {
		n, rerr := io.ReadFull(r, buf)
		if n > 0 {
			if err := s.writeChunk(ctx, ref, path, buf[:n], !first); err != nil {
				return total, err
			}
			total += int64(n)
			first = false
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return total, errx.Wrap(rerr, errx.KindInternal, "upload_read_failed", "The upload stream could not be read.")
		}
	}
	// A completely empty body never entered the write path above; still truncate
	// the file to zero so "save an empty file" and "clear a file" both work.
	if first {
		if err := s.writeChunk(ctx, ref, path, nil, false); err != nil {
			return 0, err
		}
	}
	return total, nil
}

// Mkdir creates a directory (and parents).
func (s *Service) Mkdir(ctx context.Context, siteUID, path string) error {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return err
	}
	_, err = s.broker.Invoke(ctx, "file.mkdir", s.base(ref, path))
	return err
}

// Remove deletes a file or directory tree.
func (s *Service) Remove(ctx context.Context, siteUID, path string) error {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return err
	}
	_, err = s.broker.Invoke(ctx, "file.remove", s.base(ref, path))
	return err
}

// Rename moves/renames a path within the site. Because both ends are confined
// by the broker, a destination in a different folder is a move — which is why
// there is no separate "move" capability.
func (s *Service) Rename(ctx context.Context, siteUID, from, to string) error {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return err
	}
	_, err = s.broker.Invoke(ctx, "file.rename", map[string]any{
		"root": ref.HomeDir, "from": from, "to": to, "username": ref.LinuxUser,
	})
	return err
}

// ErrDestinationExists is returned when a copy or move would land on a path that
// is already taken and the caller asked to fail rather than rename.
var ErrDestinationExists = errx.Conflict("destination_exists",
	"Something with that name already exists here.")

// OnConflict says what a copy or move should do when the destination is taken.
//
// Neither operation ever overwrites by default: `cp` and `mv` would happily
// clobber the destination, and a paste that silently replaces a file the
// operator had forgotten about is exactly the kind of data loss a file manager
// must not make easy. ConflictRename is what "duplicate this file" uses, where
// landing beside the original is the whole point.
type OnConflict string

const (
	ConflictFail   OnConflict = "fail"   // default
	ConflictRename OnConflict = "rename" // pick "name copy.ext" instead
)

// Copy duplicates a path within the site. It returns the destination actually
// used, which differs from the requested one when onConflict is ConflictRename.
func (s *Service) Copy(ctx context.Context, siteUID, from, to string, onConflict OnConflict) (string, error) {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return "", err
	}
	dest, err := s.resolveDestination(ctx, siteUID, to, onConflict)
	if err != nil {
		return "", err
	}
	_, err = s.broker.Invoke(ctx, "file.copy", map[string]any{
		"root": ref.HomeDir, "from": from, "to": dest, "username": ref.LinuxUser,
	})
	if err != nil {
		return "", err
	}
	return dest, nil
}

// Move relocates a path within the site, without overwriting the destination.
func (s *Service) Move(ctx context.Context, siteUID, from, to string, onConflict OnConflict) (string, error) {
	if from == to {
		return "", errx.Validation("same_path", "The source and the destination are the same path.")
	}
	dest, err := s.resolveDestination(ctx, siteUID, to, onConflict)
	if err != nil {
		return "", err
	}
	if err := s.Rename(ctx, siteUID, from, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// resolveDestination checks whether a destination is free, by listing its
// parent, and applies the conflict policy.
//
// There is a window between this check and the operation itself. That is
// acceptable here: both run inside one site's tree as that site's own user, so
// losing the race means the overwrite this check makes *unlikely* — never a
// write anywhere the caller could not already write.
func (s *Service) resolveDestination(ctx context.Context, siteUID, to string, onConflict OnConflict) (string, error) {
	dir, name := splitPath(to)
	if name == "" {
		return "", ErrPathRequired
	}
	listing, err := s.List(ctx, siteUID, dir)
	if err != nil {
		// A missing or unreadable parent is a real error, but it is the operation's
		// to report — it fails with a better message than this pre-check could.
		return to, nil //nolint:nilerr // deliberate: let the operation report it
	}
	taken := make(map[string]bool, len(listing.Entries))
	for _, e := range listing.Entries {
		taken[e.Name] = true
	}
	if !taken[name] {
		return to, nil
	}
	if onConflict != ConflictRename {
		return "", ErrDestinationExists
	}
	free, err := freeName(taken, name)
	if err != nil {
		return "", err
	}
	return joinRel(dir, free), nil
}

// freeName derives a name nothing occupies yet: "logo.png" becomes
// "logo copy.png", then "logo copy 2.png". The extension is preserved so the
// copy still opens in the same program as the original.
func freeName(taken map[string]bool, base string) (string, error) {
	stem, ext := splitExt(base)
	for i := 1; i < 1000; i++ {
		candidate := stem + " copy" + ext
		if i > 1 {
			candidate = stem + " copy " + strconv.Itoa(i) + ext
		}
		if !taken[candidate] {
			return candidate, nil
		}
	}
	return "", errx.Conflict("no_free_name", "Too many copies of that name already exist.")
}

// joinRel joins a site-relative directory and a name, keeping the site root
// (dir == "") expressible as a bare name.
func joinRel(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// splitPath splits a site-relative path into its parent and its final element.
func splitPath(p string) (dir, name string) {
	p = strings.Trim(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i], p[i+1:]
	}
	return "", p
}

// splitExt splits a filename into stem and extension. Compound archive suffixes
// are kept whole so "site.tar.gz" copies to "site copy.tar.gz" rather than
// "site.tar copy.gz", which no tool would recognise.
func splitExt(name string) (stem, ext string) {
	for _, compound := range []string{".tar.gz", ".tar.bz2", ".tar.xz"} {
		if strings.HasSuffix(strings.ToLower(name), compound) {
			return name[:len(name)-len(compound)], name[len(name)-len(compound):]
		}
	}
	// A leading dot is a dotfile, not an extension: ".env" has no extension.
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[:i], name[i:]
	}
	return name, ""
}

// Chmod changes a path's mode (octal, e.g. "644").
func (s *Service) Chmod(ctx context.Context, siteUID, path, mode string) error {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return err
	}
	in := s.base(ref, path)
	in["mode"] = mode
	_, err = s.broker.Invoke(ctx, "file.chmod", in)
	return err
}

// Extract unpacks an archive within the site into a destination directory.
func (s *Service) Extract(ctx context.Context, siteUID, archive, dest string) error {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return err
	}
	_, err = s.broker.Invoke(ctx, "file.extract", map[string]any{
		"root": ref.HomeDir, "archive": archive, "dest": dest, "username": ref.LinuxUser,
	})
	return err
}

// Compress creates an archive from site-relative entries that share one parent
// folder. It is what makes "download this folder" possible: HTTP hands back one
// file, not a tree.
func (s *Service) Compress(ctx context.Context, siteUID string, sources []string, archive, format string) error {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return err
	}
	_, err = s.broker.Invoke(ctx, "file.compress", map[string]any{
		"root": ref.HomeDir, "sources": sources, "archive": archive,
		"format": format, "username": ref.LinuxUser,
	})
	return err
}

// DownloadArchive zips a directory, streams it to w, and deletes the archive
// afterwards.
//
// Without this, "download this folder" means "compress it, then download the
// file you just made, then remember to delete it" — and the third step never
// happens, so sites accumulate stale archives of themselves. The temporary
// archive is still written to disk (zip needs seekable output; it cannot be
// produced on a pipe), but its lifetime is this request: the cleanup runs even
// when the transfer fails halfway.
//
// The name is unpredictable so that two concurrent downloads of the same folder
// cannot collide, and so the transient file is not a guessable URL for anything
// serving the site's document root.
func (s *Service) DownloadArchive(ctx context.Context, siteUID, dir string, w io.Writer) (int64, error) {
	if _, err := s.resolveEditable(ctx, siteUID); err != nil {
		return 0, err
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return 0, errx.Wrap(err, errx.KindInternal, "archive_name_failed", "Could not prepare the download.")
	}

	// The archive is built in the *parent* of the directory being archived, so it
	// never lands inside the tree it is compressing.
	parent, _ := splitPath(dir)
	archive := joinRel(parent, ".hp-download-"+hex.EncodeToString(suffix)+".zip")

	// Every source of one archive must share a parent (see file.compress), which
	// the site root cannot: it has no parent inside the tree. Archiving the root
	// therefore means archiving its entries, listed here.
	sources := []string{dir}
	if dir == "" {
		listing, err := s.List(ctx, siteUID, "")
		if err != nil {
			return 0, err
		}
		sources = sources[:0]
		for _, e := range listing.Entries {
			sources = append(sources, e.Name)
		}
		if len(sources) == 0 {
			return 0, errx.Validation("empty_directory", "There is nothing here to download.")
		}
	}

	if err := s.Compress(ctx, siteUID, sources, archive, "zip"); err != nil {
		return 0, err
	}
	// Best-effort cleanup on every path out, including a client that disconnects
	// mid-transfer. A context that is already cancelled cannot carry the delete,
	// so it runs on a fresh one.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_ = s.Remove(cleanup, siteUID, archive)
	}()

	return s.Download(ctx, siteUID, archive, w)
}

// ArchiveName is the filename a folder download should be offered under. The
// site root has no name of its own, so it falls back to "site".
func ArchiveName(dir string) string {
	_, name := splitPath(dir)
	if name == "" {
		return "site.zip"
	}
	return name + ".zip"
}

// FixOwnership resets a path's ownership (recursively) to the site's own user.
// The broker constrains the target account; there is no way to name another one.
func (s *Service) FixOwnership(ctx context.Context, siteUID, path string) error {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return err
	}
	_, err = s.broker.Invoke(ctx, "file.chown", s.base(ref, path))
	return err
}

// SearchResult is one hit, with a path relative to the searched directory.
type SearchResult struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size"`
}

// Search walks a subtree for entries matching query. mode is "name" (default) or
// "content". Results are capped by the broker; Truncated says whether they were.
type Search struct {
	Path      string         `json:"path"`
	Entries   []SearchResult `json:"entries"`
	Truncated bool           `json:"truncated"`
}

// Search runs a recursive search under path, as the site user.
func (s *Service) Search(ctx context.Context, siteUID, path, query, mode string) (*Search, error) {
	ref, err := s.resolveEditable(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	in := s.base(ref, path)
	in["query"] = query
	in["mode"] = mode
	res, err := s.broker.Invoke(ctx, "file.search", in)
	if err != nil {
		return nil, err
	}
	out := &Search{Entries: []SearchResult{}}
	if p, ok := res["path"].(string); ok {
		out.Path = p
	}
	out.Truncated, _ = res["truncated"].(bool)
	rawEntries, _ := res["entries"].([]any)
	for _, e := range rawEntries {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		out.Entries = append(out.Entries, SearchResult{
			Name: str(m["name"]),
			Path: str(m["path"]),
			Kind: str(m["kind"]),
			Size: toInt64(m["size"]),
		})
	}
	return out, nil
}

// listingFromResult converts the broker's generic result map into a Listing.
// Numbers arrive as float64 (JSON), so they are converted explicitly.
func listingFromResult(res map[string]any) *Listing {
	l := &Listing{Entries: []Entry{}}
	if p, ok := res["path"].(string); ok {
		l.Path = p
	}
	raw, _ := res["entries"].([]any)
	for _, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		l.Entries = append(l.Entries, Entry{
			Name:  str(m["name"]),
			Kind:  str(m["kind"]),
			Size:  toInt64(m["size"]),
			Mode:  str(m["mode"]),
			Mtime: toInt64(m["mtime"]),
		})
	}
	return l
}

func decodeContent(v any) []byte {
	switch t := v.(type) {
	case string: // base64 (Go marshals []byte as base64)
		b, err := base64.StdEncoding.DecodeString(t)
		if err != nil {
			return nil
		}
		return b
	case []byte:
		return t
	default:
		return nil
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func toInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	default:
		return 0
	}
}
