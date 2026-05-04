package catalog

import (
	"bytes"
	_ "embed"
)

// Canonical catalog: evidence-fetchers/1-select-fetchers/evidence_fetchers_catalog.json (see embedded/README.md).
//
//go:embed embedded/evidence_fetchers_catalog.json
var embeddedJSON []byte

// LoadEmbedded parses the embedded catalog (malformed JSON is a build-time bug).
func LoadEmbedded() (*Catalog, []Script, error) {
	return Load(bytes.NewReader(embeddedJSON))
}

func EmbeddedBytes() []byte {
	cp := make([]byte, len(embeddedJSON))
	copy(cp, embeddedJSON)
	return cp
}
