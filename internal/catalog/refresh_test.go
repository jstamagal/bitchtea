package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// fakeClient lets tests count calls and pick the response without reaching
// the real catwalk service. It implements the Provider interface.
type fakeClient struct {
	calls       int32
	providers   []catwalk.Provider
	err         error
	wantETagSet bool
	gotETag     string
}

func (f *fakeClient) GetProviders(ctx context.Context, etag string) ([]catwalk.Provider, error) {
	atomic.AddInt32(&f.calls, 1)
	f.gotETag = etag
	if f.err != nil {
		return nil, f.err
	}
	return f.providers, nil
}

func TestRefresh_200_ReplacesCacheAndStoresETag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	fake := &fakeClient{
		providers: []catwalk.Provider{{Name: "OpenAI", ID: "openai"}},
	}
	res := Refresh(context.Background(), RefreshOptions{
		CachePath: path,
		Enabled:   true,
		SourceURL: "https://example.invalid",
		Client:    fake,
	})
	if res.Err != nil {
		t.Fatalf("Refresh err: %v", res.Err)
	}
	if !res.HitNetwork || !res.Updated || res.NotModified || res.FromCache {
		t.Fatalf("flags wrong: %+v", res)
	}
	if atomic.LoadInt32(&fake.calls) != 1 {
		t.Fatalf("want 1 HTTP call, got %d", fake.calls)
	}
	if len(res.Envelope.Providers) != 1 || res.Envelope.Providers[0].ID != "openai" {
		t.Fatalf("providers not replaced: %+v", res.Envelope.Providers)
	}
	if res.Envelope.ETag == "" {
		t.Fatalf("etag not stored")
	}
	if res.Envelope.FetchedAt.IsZero() || res.Envelope.LastChecked.IsZero() {
		t.Fatalf("timestamps not bumped: %+v", res.Envelope)
	}
	// File on disk reflects the same.
	disk, err := ReadCache(path)
	if err != nil {
		t.Fatalf("ReadCache: %v", err)
	}
	if disk.ETag != res.Envelope.ETag || len(disk.Providers) != 1 {
		t.Fatalf("disk not updated: %+v", disk)
	}
}

func TestRefresh_NotModified_BumpsLastCheckedOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	original := Envelope{
		SchemaVersion: SchemaVersion,
		FetchedAt:     time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		LastChecked:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		ETag:          `"abc"`,
		Source:        "https://example.invalid",
		Providers:     []catwalk.Provider{{Name: "Anthropic", ID: "anthropic"}},
	}
	if err := WriteCache(path, original); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	fake := &fakeClient{err: catwalk.ErrNotModified}
	frozen := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	res := Refresh(context.Background(), RefreshOptions{
		CachePath: path,
		Enabled:   true,
		SourceURL: "https://example.invalid",
		Client:    fake,
		Now:       func() time.Time { return frozen },
	})
	if res.Err != nil {
		t.Fatalf("Refresh err: %v", res.Err)
	}
	if !res.NotModified || res.Updated {
		t.Fatalf("flags wrong: %+v", res)
	}
	if fake.gotETag != `"abc"` {
		t.Fatalf("client did not receive cached etag, got %q", fake.gotETag)
	}
	if !res.Envelope.LastChecked.Equal(frozen) {
		t.Fatalf("last_checked not bumped: %v", res.Envelope.LastChecked)
	}
	if !res.Envelope.FetchedAt.Equal(original.FetchedAt) {
		t.Fatalf("fetched_at should not change on 304, got %v", res.Envelope.FetchedAt)
	}
	if res.Envelope.ETag != original.ETag {
		t.Fatalf("etag should not change on 304, got %q", res.Envelope.ETag)
	}
	if len(res.Envelope.Providers) != 1 || res.Envelope.Providers[0].ID != "anthropic" {
		t.Fatalf("providers should be untouched, got %+v", res.Envelope.Providers)
	}
}

func TestRefresh_TransportError_ReturnsStaleCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	stale := Envelope{
		SchemaVersion: SchemaVersion,
		FetchedAt:     time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		LastChecked:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		ETag:          `"old"`,
		Source:        "https://example.invalid",
		Providers:     []catwalk.Provider{{Name: "Stale", ID: "openai"}},
	}
	if err := WriteCache(path, stale); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	wantErr := errors.New("dial tcp: lookup catwalk.invalid: no such host")
	fake := &fakeClient{err: wantErr}
	res := Refresh(context.Background(), RefreshOptions{
		CachePath: path,
		Enabled:   true,
		SourceURL: "https://example.invalid",
		Client:    fake,
	})
	if !errors.Is(res.Err, wantErr) {
		t.Fatalf("want surfaced transport error, got %v", res.Err)
	}
	if res.Updated || res.NotModified {
		t.Fatalf("flags wrong on transport error: %+v", res)
	}
	if !res.HitNetwork {
		t.Fatalf("HitNetwork should be true on transport failure")
	}
	if len(res.Envelope.Providers) != 1 || res.Envelope.Providers[0].ID != "openai" {
		t.Fatalf("stale cache should be returned: %+v", res.Envelope)
	}
	if !res.Envelope.LastChecked.Equal(stale.LastChecked) {
		t.Fatalf("last_checked should not move on transport failure")
	}
}

func TestRefresh_FreshCache_SkipsHTTPCall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	fresh := Envelope{
		SchemaVersion: SchemaVersion,
		FetchedAt:     now.Add(-1 * time.Hour),
		LastChecked:   now.Add(-1 * time.Hour), // only 1h old, well under 24h
		ETag:          `"fresh"`,
		Providers:     []catwalk.Provider{{Name: "Cached", ID: "openai"}},
	}
	if err := WriteCache(path, fresh); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	fake := &fakeClient{providers: []catwalk.Provider{{Name: "Network", ID: "openai"}}}
	res := Refresh(context.Background(), RefreshOptions{
		CachePath: path,
		Enabled:   true,
		SourceURL: "https://example.invalid",
		Client:    fake,
		Now:       func() time.Time { return now },
	})
	if atomic.LoadInt32(&fake.calls) != 0 {
		t.Fatalf("fresh cache should not trigger HTTP, got %d calls", fake.calls)
	}
	if !res.FromCache {
		t.Fatalf("FromCache should be true on TTL hit: %+v", res)
	}
	if res.HitNetwork || res.Updated {
		t.Fatalf("flags wrong on TTL hit: %+v", res)
	}
	if res.Envelope.Providers[0].Name != "Cached" {
		t.Fatalf("expected cached envelope, got %+v", res.Envelope)
	}
}

func TestRefresh_CtxAlreadyExpired_ReturnsCacheNoHTTP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	stale := Envelope{
		SchemaVersion: SchemaVersion,
		LastChecked:   time.Now().Add(-72 * time.Hour),
		Providers:     []catwalk.Provider{{Name: "Stale", ID: "openai"}},
	}
	if err := WriteCache(path, stale); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	fake := &fakeClient{providers: []catwalk.Provider{{Name: "Network", ID: "openai"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := Refresh(ctx, RefreshOptions{
		CachePath: path,
		Enabled:   true,
		SourceURL: "https://example.invalid",
		Client:    fake,
	})
	if atomic.LoadInt32(&fake.calls) != 0 {
		t.Fatalf("expired ctx should not trigger HTTP, got %d calls", fake.calls)
	}
	if !res.FromCache || res.HitNetwork {
		t.Fatalf("flags wrong on expired ctx: %+v", res)
	}
	if !errors.Is(res.Err, context.Canceled) {
		t.Fatalf("want ctx error surfaced, got %v", res.Err)
	}
	if len(res.Envelope.Providers) != 1 || res.Envelope.Providers[0].Name != "Stale" {
		t.Fatalf("cache should be returned even with expired ctx: %+v", res.Envelope)
	}
}

func TestRefresh_Disabled_ReturnsCacheNoHTTP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	cached := Envelope{SchemaVersion: SchemaVersion, Providers: []catwalk.Provider{{ID: "openai"}}}
	if err := WriteCache(path, cached); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	fake := &fakeClient{}
	res := Refresh(context.Background(), RefreshOptions{
		CachePath: path,
		Enabled:   false, // off
		SourceURL: "https://example.invalid",
		Client:    fake,
	})
	if atomic.LoadInt32(&fake.calls) != 0 {
		t.Fatalf("disabled refresh must not call client")
	}
	if !res.FromCache || res.HitNetwork {
		t.Fatalf("flags wrong with disabled refresh: %+v", res)
	}
}

func TestRefresh_AgainstHTTPTestServer_End2End(t *testing.T) {
	// Stand up a real (loopback-only) catwalk-shaped server so we exercise
	// the actual catwalk.Client wire path without mocking the Provider
	// interface — gives us coverage of the "Client is nil → catwalk.NewWithURL"
	// branch and proves we can refresh fully offline.
	payload := []catwalk.Provider{{Name: "Test", ID: "openai"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/providers" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	res := Refresh(context.Background(), RefreshOptions{
		CachePath: path,
		Enabled:   true,
		SourceURL: srv.URL,
		// Client left nil on purpose.
	})
	if res.Err != nil {
		t.Fatalf("end-to-end refresh err: %v", res.Err)
	}
	if !res.Updated || !res.HitNetwork {
		t.Fatalf("flags wrong end-to-end: %+v", res)
	}
	if len(res.Envelope.Providers) != 1 || res.Envelope.Providers[0].Name != "Test" {
		t.Fatalf("payload not stored: %+v", res.Envelope.Providers)
	}
	if res.Envelope.Source != srv.URL {
		t.Fatalf("source url not stamped: %q", res.Envelope.Source)
	}
}
