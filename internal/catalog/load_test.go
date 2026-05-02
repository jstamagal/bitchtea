package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
)

func TestLoad_PrefersCacheOverEmbedded(t *testing.T) {
	base := t.TempDir()
	path := CachePath(base)
	want := Envelope{
		SchemaVersion: SchemaVersion,
		Source:        "test",
		Providers:     []catwalk.Provider{{Name: "FromCache", ID: "openai"}},
	}
	if err := WriteCache(path, want); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	got := Load(LoadOptions{BaseDir: base})
	if len(got.Providers) != 1 || got.Providers[0].Name != "FromCache" {
		t.Fatalf("Load should return cached providers, got %+v", got.Providers)
	}
	if got.Source != "test" {
		t.Fatalf("Load should preserve source, got %q", got.Source)
	}
}

func TestLoad_FallsBackToEmbeddedWhenCacheMissing(t *testing.T) {
	base := t.TempDir() // empty dir, no cache file
	got := Load(LoadOptions{BaseDir: base})
	if got.Source != "embedded" {
		t.Fatalf("expected embedded source, got %q", got.Source)
	}
	// embedded.GetAll() ships a non-empty list at v0.35.1 — guard against a
	// future version regression by asserting we got something back.
	if len(got.Providers) == 0 {
		t.Fatalf("embedded fallback should have providers")
	}
}

func TestLoad_FallsBackToEmptyEnvelopeWhenEmbeddedSkipped(t *testing.T) {
	base := t.TempDir()
	got := Load(LoadOptions{BaseDir: base, SkipEmbedded: true})
	if len(got.Providers) != 0 {
		t.Fatalf("SkipEmbedded should yield empty providers, got %d", len(got.Providers))
	}
	if got.SchemaVersion != SchemaVersion {
		t.Fatalf("envelope should still be schema-stamped, got %d", got.SchemaVersion)
	}
}

func TestLoad_CorruptCacheFallsThroughToEmbedded(t *testing.T) {
	base := t.TempDir()
	path := CachePath(base)
	// Write garbage.
	if err := writeRaw(path, []byte("not json")); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	got := Load(LoadOptions{BaseDir: base})
	if got.Source != "embedded" {
		t.Fatalf("corrupt cache should fall through to embedded, got source %q", got.Source)
	}
}

// writeRaw is a tiny helper that creates parent dirs so we can plant a
// corrupt file at CachePath without going through WriteCache (which would
// re-marshal to JSON).
func writeRaw(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
