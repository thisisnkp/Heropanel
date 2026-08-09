package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// updateBinaries is the set a release replaces. np-installer is in the list
// because self-update depends on it being present and current — an installer
// left behind at an old version is the one component that could not fix itself.
var updateBinaries = []string{"npd", "np-broker", "np-installer"}

// reason renders an error as the one-line explanation the updates card shows.
// It prefers the structured user-facing message over the wrapped internal
// detail, which is the whole reason errx separates them.
func reason(err error) string {
	if err == nil {
		return ""
	}
	if e, ok := errx.As(err); ok {
		return e.Message
	}
	return err.Error()
}

// fetchAttempts bounds how hard a single download tries. Three is chosen for
// what it protects against — a dropped connection or a momentary 5xx — not for
// riding out a sustained outage, which should surface as a failure the operator
// can see rather than a request that hangs for minutes.
const fetchAttempts = 3

// fetch GETs a URL with a small bounded retry. It returns the body, refusing
// anything that is not a 200: a release server answering 404 with an HTML error
// page must not become a "manifest" that then fails signature verification with
// a confusing message.
func (s *Service) fetch(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt-1) * time.Second):
			}
		}
		body, err := s.fetchOnce(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		// A 4xx will not fix itself; only retry transport errors and 5xx.
		if errors.Is(err, errPermanent) {
			break
		}
	}
	return nil, errx.Wrap(lastErr, errx.KindUpstream, "release_fetch_failed",
		"Could not reach the release server: "+reason(lastErr))
}

// errPermanent marks a response that retrying cannot improve.
var errPermanent = errors.New("permanent")

func (s *Service) fetchOnce(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errPermanent, err)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("%s returned HTTP %d", url, resp.StatusCode)
		if resp.StatusCode < 500 {
			return nil, fmt.Errorf("%w: %v", errPermanent, err)
		}
		return nil, err
	}
	return io.ReadAll(resp.Body)
}

// StageDir is where a release's verified artifacts are assembled. It sits under
// npd's own data directory on purpose: npd's systemd unit already grants
// ReadWritePaths there, so staging needs no broker call and no widening of the
// broker's path policy — the privileged step is only ever the swap itself.
func (s *Service) StageDir(version string) string {
	return filepath.Join(s.dataDir, "updates", version)
}

// Stage downloads a release and verifies it end to end, leaving the artifacts
// ready for the installer. Nothing is considered staged until every check has
// passed: the signature over SHA256SUMS, then each binary's hash against that
// manifest.
//
// The download lands in a sibling `.partial` directory and is renamed into
// place only once verified, so an interrupted or tampered download can never be
// mistaken for a staged release by a later Apply.
func (s *Service) Stage(ctx context.Context, version string) (string, error) {
	if err := s.configuredErr(); err != nil {
		return "", err
	}
	if err := validVersion(version); err != nil {
		return "", err
	}

	final := s.StageDir(version)
	tmp := final + ".partial"
	if err := os.RemoveAll(tmp); err != nil {
		return "", errx.Internal(err)
	}
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return "", errx.Internal(err)
	}
	// Any failure past this point leaves nothing half-staged behind.
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(tmp)
		}
	}()

	base := strings.TrimSuffix(s.cfg.BaseURL, "/") + "/" + version

	// 1. The manifest and its signature. Verified before a single binary is
	//    fetched, so an unsigned release costs one round trip rather than the
	//    whole download.
	sums, err := s.fetch(ctx, base+"/SHA256SUMS")
	if err != nil {
		return "", err
	}
	sig, err := s.fetch(ctx, base+"/SHA256SUMS.sig")
	if err != nil {
		return "", err
	}
	if err := verifyDetachedSig(sums, string(sig), s.pub); err != nil {
		return "", errx.New(errx.KindValidation, "release_untrusted",
			"The release manifest is not signed by the pinned release key: "+err.Error())
	}
	want := parseChecksums(sums)

	// 2. Each binary, hashed against the now-trusted manifest.
	for _, name := range updateBinaries {
		body, err := s.fetch(ctx, base+"/"+name)
		if err != nil {
			return "", err
		}
		dst := filepath.Join(tmp, name)
		if err := os.WriteFile(dst, body, 0o755); err != nil {
			return "", errx.Internal(err)
		}
		sum, listed := want[name]
		if !listed {
			return "", errx.New(errx.KindValidation, "release_incomplete",
				fmt.Sprintf("The release manifest lists no checksum for %s, so it cannot be trusted.", name))
		}
		if err := verifyChecksum(dst, sum); err != nil {
			return "", errx.New(errx.KindValidation, "release_corrupt",
				fmt.Sprintf("%s does not match the signed manifest: %v", name, err))
		}
	}

	// 3. Keep the manifest beside the binaries: the installer re-verifies the
	//    whole chain itself rather than trusting that npd already did.
	if err := os.WriteFile(filepath.Join(tmp, "SHA256SUMS"), sums, 0o644); err != nil {
		return "", errx.Internal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "SHA256SUMS.sig"), sig, 0o644); err != nil {
		return "", errx.Internal(err)
	}

	if err := os.RemoveAll(final); err != nil {
		return "", errx.Internal(err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return "", errx.Internal(err)
	}
	ok = true
	s.log.Info("self-update: release staged and verified", "version", version, "dir", final)
	return final, nil
}

// parseChecksums reads sha256sum output: "<hex>␠␠<name>", where sha256sum marks
// binary-mode entries with a leading '*' on the name.
func parseChecksums(b []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) != 2 {
			continue
		}
		out[filepath.Base(strings.TrimPrefix(f[1], "*"))] = strings.ToLower(f[0])
	}
	return out
}

// validVersion keeps a version string safe to use as a path segment and a URL
// segment. It is the guard that stops a manifest — or an operator — naming
// "../../etc" as a version and having the stage directory land elsewhere.
func validVersion(v string) error {
	if v == "" || len(v) > 64 {
		return errx.Validation("invalid_version", "A release version is required.")
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r == '.' || r == '-' || r == '+' || r == '_':
		default:
			return errx.Validation("invalid_version",
				fmt.Sprintf("Release version %q contains characters that are not allowed.", v))
		}
	}
	if strings.Contains(v, "..") {
		return errx.Validation("invalid_version", "A release version may not contain \"..\".")
	}
	return nil
}
