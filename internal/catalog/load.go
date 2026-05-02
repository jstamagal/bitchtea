package catalog

import (
	"encoding/json"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"

	"github.com/jstamagal/bitchtea/internal/config"
)

// LoadOptions controls Load's fallback chain.
type LoadOptions struct {
	// BaseDir overrides config.BaseDir() (handy for tests).
	BaseDir string

	// SkipEmbedded disables the embedded floor — used by tests to assert
	// the empty-tail behavior.
	SkipEmbedded bool
}

// Load returns the best Envelope it can produce synchronously, walking the
// strict precedence laid out in docs/phase-5-catalog-audit.md:
//
//  1. On-disk cache at ~/.bitchtea/catalog/providers.json (if present and
//     parseable with a matching schema_version).
//  2. catwalk's compiled-in embedded.GetAll() snapshot.
//  3. Empty Envelope.
//
// Load never returns an error and never blocks on I/O beyond a single file
// read. It is safe to call from startup. Refresh is a separate operation
// that is fired in the background.
func Load(opts LoadOptions) Envelope {
	baseDir := opts.BaseDir
	if baseDir == "" {
		baseDir = config.BaseDir()
	}
	if env, err := ReadCache(CachePath(baseDir)); err == nil {
		return env
	}
	if opts.SkipEmbedded {
		return Envelope{SchemaVersion: SchemaVersion}
	}
	return embeddedEnvelope()
}

// embeddedEnvelope wraps the catwalk-bundled provider snapshot in an
// Envelope so callers see a uniform type. We deliberately leave FetchedAt /
// LastChecked zero — the embedded floor has no notion of freshness.
func embeddedEnvelope() Envelope {
	providers := embedded.GetAll()
	env := Envelope{
		SchemaVersion: SchemaVersion,
		Source:        "embedded",
		Providers:     providers,
	}
	if etag := computeETag(providers); etag != "" {
		env.ETag = etag
	}
	return env
}

// jsonMarshal centralises encoding so tests can swap it out if needed.
// Indentation matches WriteCache for byte-stable output.
func jsonMarshal(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// Type-anchor: keep the unused import wired so a refactor that drops the
// catwalk.Provider field type from Envelope fails at build time. This is a
// nil-safe sanity check, not a runtime guard.
var _ catwalk.Provider
