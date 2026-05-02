// Package catalog provides a bounded, cache-backed refresh path for the
// catwalk model catalog. The cache is the single source of truth for the
// model picker and any consumer that needs richer metadata than the
// hardcoded built-in profiles ship.
//
// Design contract (see docs/phase-5-catalog-audit.md):
//
//   - One JSON file at ~/.bitchtea/catalog/providers.json wrapped in an
//     Envelope that records freshness and the upstream ETag.
//   - Refresh is HTTP-conditional via catwalk's ETag plumbing and is always
//     bounded by the caller's context.
//   - Reads never fail: cache > embedded > empty Envelope.
//   - Network access is off by default. Only opt-in via opts.Enabled (which
//     main.go derives from BITCHTEA_CATWALK_AUTOUPDATE / _URL).
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// SchemaVersion is the on-disk envelope version. Bump only on breaking
// envelope changes — additive fields inside catwalk.Provider are tolerated.
const SchemaVersion = 1

// Envelope wraps the raw catwalk provider list with freshness metadata.
// The shape matches the Phase 5 audit doc; do not reorder fields without
// also updating the doc.
type Envelope struct {
	SchemaVersion int                 `json:"schema_version"`
	FetchedAt     time.Time           `json:"fetched_at"`
	LastChecked   time.Time           `json:"last_checked"`
	ETag          string              `json:"etag"`
	Source        string              `json:"source"`
	Providers     []catwalk.Provider  `json:"providers"`
}

// CachePath returns the absolute path to the providers cache file under the
// given bitchtea base dir (typically ~/.bitchtea).
func CachePath(baseDir string) string {
	return filepath.Join(baseDir, "catalog", "providers.json")
}

// ReadCache loads the envelope from path. A missing file returns
// (zero, fs.ErrNotExist). A corrupt or wrong-schema file returns a non-nil
// error so callers can fall through to the embedded floor.
func ReadCache(path string) (Envelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Envelope{}, err
	}
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	if env.SchemaVersion != SchemaVersion {
		return Envelope{}, fmt.Errorf("unsupported schema_version %d (want %d)", env.SchemaVersion, SchemaVersion)
	}
	return env, nil
}

// WriteCache writes env to path atomically: marshal → write to a sibling
// .tmp file → fsync → rename. Creates the parent directory with 0o700 to
// match the rest of ~/.bitchtea/.
func WriteCache(path string, env Envelope) error {
	if env.SchemaVersion == 0 {
		env.SchemaVersion = SchemaVersion
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("encode envelope: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir cache dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "providers-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// IsCacheMissing reports whether err indicates the cache file simply isn't
// there yet (vs corruption / IO failure).
func IsCacheMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
