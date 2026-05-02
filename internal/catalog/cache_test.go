package catalog

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

func TestWriteCacheReadCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	want := Envelope{
		SchemaVersion: SchemaVersion,
		FetchedAt:     time.Date(2026, 5, 1, 18, 42, 11, 0, time.UTC),
		LastChecked:   time.Date(2026, 5, 2, 2, 38, 0, 0, time.UTC),
		ETag:          `"5f1c"`,
		Source:        "https://catwalk.charm.sh",
		Providers: []catwalk.Provider{
			{Name: "OpenAI", ID: "openai"},
			{Name: "Anthropic", ID: "anthropic"},
		},
	}
	if err := WriteCache(path, want); err != nil {
		t.Fatalf("WriteCache: %v", err)
	}
	got, err := ReadCache(path)
	if err != nil {
		t.Fatalf("ReadCache: %v", err)
	}
	if got.SchemaVersion != want.SchemaVersion ||
		!got.FetchedAt.Equal(want.FetchedAt) ||
		!got.LastChecked.Equal(want.LastChecked) ||
		got.ETag != want.ETag ||
		got.Source != want.Source ||
		len(got.Providers) != len(want.Providers) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, want)
	}
	if got.Providers[0].ID != "openai" || got.Providers[1].ID != "anthropic" {
		t.Fatalf("provider list mangled: %+v", got.Providers)
	}
}

func TestReadCacheMissingReturnsErrNotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadCache(filepath.Join(dir, "nope.json"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want fs.ErrNotExist, got %v", err)
	}
	if !IsCacheMissing(err) {
		t.Fatalf("IsCacheMissing should be true for %v", err)
	}
}

func TestReadCacheRejectsWrongSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	bogus := Envelope{SchemaVersion: 99}
	if err := WriteCache(path, bogus); err == nil {
		// WriteCache stamps SchemaVersion=99 verbatim because it's nonzero.
		// Just sanity-check that ReadCache rejects it.
	} else {
		t.Fatalf("WriteCache: %v", err)
	}
	if _, err := ReadCache(path); err == nil {
		t.Fatalf("ReadCache should reject schema_version 99")
	}
}

func TestWriteCacheCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dirs", "providers.json")
	if err := WriteCache(path, Envelope{}); err != nil {
		t.Fatalf("WriteCache: %v", err)
	}
	if _, err := ReadCache(path); err != nil {
		t.Fatalf("ReadCache after nested write: %v", err)
	}
}
