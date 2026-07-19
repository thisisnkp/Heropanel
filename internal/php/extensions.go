package php

import (
	"context"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Extensions is the extension state for one PHP version.
//
// Note the scope, which the field names make deliberately hard to misread:
// this is a property of a *version*, not of a site. See ExtensionScopeNote.
type Extensions struct {
	Version   string   `json:"version"`
	Available []string `json:"available"`
	Enabled   []string `json:"enabled"`
}

// ExtensionScopeNote is returned with the extension list so the UI does not have
// to hardcode the caveat — and so it cannot quietly drop it.
//
// It matters because the obvious expectation, in a panel where PHP *version* is
// per-site, is that extensions are per-site too. They are not, and PHP gives no
// hint: an `extension` directive in a pool file is accepted by php-fpm -t and
// then ignored, because the master loads extensions when it starts, before any
// pool exists. An operator who assumes otherwise ends up debugging an extension
// that the panel said was on.
const ExtensionScopeNote = "Extensions apply to every site using this PHP version. " +
	"PHP loads them into the FPM master at startup, so they cannot be set per site."

// ListExtensions returns the extensions installed for a version and which of
// them the FPM SAPI has enabled.
func (s *Service) ListExtensions(ctx context.Context, version string) (*Extensions, error) {
	if version == "" {
		version = DefaultVersion
	}
	if !IsSupported(version) {
		return nil, errx.Validation("unsupported_php_version", "That PHP version is not available.",
			errx.Field{Field: "version", Code: "unsupported", Message: "unsupported version"})
	}
	if s.broker == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; PHP extensions cannot be listed.")
	}

	out, err := s.broker.Invoke(ctx, "php.list_extensions", map[string]any{"version": version})
	if err != nil {
		return nil, err
	}
	return &Extensions{
		Version:   version,
		Available: stringsOf(out["available"]),
		Enabled:   stringsOf(out["enabled"]),
	}, nil
}

// SetExtension enables or disables an extension for a version.
//
// This restarts PHP-FPM for that version, which briefly interrupts every site
// running on it. That is not an implementation shortcut: an extension is linked
// into the master process at exec time, so there is no graceful way to add one
// to a master that is already running. The alternative — reload and report
// success — would leave the extension off while claiming it was on.
func (s *Service) SetExtension(ctx context.Context, version, extension string, enabled bool) (*Extensions, error) {
	if version == "" {
		version = DefaultVersion
	}
	if !IsSupported(version) {
		return nil, errx.Validation("unsupported_php_version", "That PHP version is not available.",
			errx.Field{Field: "version", Code: "unsupported", Message: "unsupported version"})
	}
	if extension == "" {
		return nil, errx.Validation("missing_extension", "An extension name is required.",
			errx.Field{Field: "extension", Code: "required", Message: "required"})
	}
	if s.broker == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; PHP extensions cannot be changed.")
	}

	if _, err := s.broker.Invoke(ctx, "php.set_extension", map[string]any{
		"version":   version,
		"extension": extension,
		"enabled":   enabled,
	}); err != nil {
		return nil, err
	}
	// Report the state as the system now has it, rather than as we asked for it.
	return s.ListExtensions(ctx, version)
}

// stringsOf converts a broker envelope's []any of strings.
func stringsOf(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
