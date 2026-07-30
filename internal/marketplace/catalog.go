package marketplace

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/thisisnkp/heropanel/pkg/proto"
)

// Catalog is a marketplace feed: the set of module manifests offered for
// install. Each manifest is independently signed by its publisher, so the
// catalog file itself needs no signature — tampering with any entry breaks that
// entry's signature and the entry is refused at verify time. The catalog is thus
// a plain, cache-friendly index whose trust lives in its contents, not its
// transport.
type Catalog struct {
	Modules []proto.Manifest `json:"modules"`
}

// ParseCatalog decodes a catalog document. It does not verify the entries —
// verification happens per-module at browse and install time, against the live
// keyring — so a catalog that mixes trusted and untrusted entries parses fine
// and each entry is judged on its own signature.
func ParseCatalog(b []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("marketplace: parse catalog: %w", err)
	}
	return &c, nil
}

// LoadCatalog reads and parses a catalog file from disk. A missing file is a
// distinct, nameable error so the operator is told the feed path is wrong rather
// than that the marketplace is simply empty.
func LoadCatalog(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("marketplace: read catalog %s: %w", path, err)
	}
	return ParseCatalog(b)
}

// find returns the manifest for a slug, if the catalog offers it.
func (c *Catalog) find(slug string) (proto.Manifest, bool) {
	if c == nil {
		return proto.Manifest{}, false
	}
	for _, m := range c.Modules {
		if m.Metadata.Slug == slug {
			return m, true
		}
	}
	return proto.Manifest{}, false
}
