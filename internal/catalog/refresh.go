package catalog

import (
	"context"
	"errors"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// Default time / TTL knobs. See docs/phase-5-catalog-audit.md.
const (
	// DefaultSoftTTL is how long a cache entry is considered "fresh enough"
	// that no network attempt is made on the next call.
	DefaultSoftTTL = 24 * time.Hour

	// DefaultRefreshTimeout is the recommended caller-side context deadline
	// for Refresh. Wire-up code in main.go should construct a context with
	// at most this much time so startup never blocks indefinitely.
	DefaultRefreshTimeout = 5 * time.Second
)

// Provider names the upstream catwalk client behind a tiny interface so
// tests can stand up an httptest server without dialing the real catwalk.
// Only the one method actually used is mocked.
type Provider interface {
	GetProviders(ctx context.Context, etag string) ([]catwalk.Provider, error)
}

// RefreshOptions is the bag of knobs Refresh consults. Zero values are
// intentionally safe defaults (off, no network).
type RefreshOptions struct {
	// CachePath is the absolute path to the providers.json envelope.
	// Required — Refresh has nowhere else to read or write.
	CachePath string

	// Enabled gates the network call. When false, Refresh returns whatever
	// is on disk (or a zero Envelope) and never dials.
	Enabled bool

	// SourceURL is the upstream catwalk base URL. Recorded into the envelope
	// so future runs can detect when the user changed CATWALK_URL under a
	// stale cache. Empty disables the network even if Enabled is true.
	SourceURL string

	// Client is the catwalk client to use. If nil and SourceURL is set,
	// Refresh constructs catwalk.NewWithURL(SourceURL). Tests pass a fake.
	Client Provider

	// SoftTTL overrides the default 24h freshness window.
	SoftTTL time.Duration

	// Now overrides time.Now for deterministic tests.
	Now func() time.Time
}

// RefreshResult reports what happened. Refresh never returns a non-nil
// error to the caller — transport / decode failures are surfaced via
// Result.Err so the boot path stays silent and the cache stays usable.
type RefreshResult struct {
	Envelope Envelope
	// Updated is true when providers / etag actually changed on disk.
	Updated bool
	// HitNetwork is true when an HTTP round trip was attempted.
	HitNetwork bool
	// NotModified is true when the server returned 304.
	NotModified bool
	// FromCache is true when the result came straight from disk without a
	// network round trip (TTL hit, ctx already expired, or refresh disabled).
	FromCache bool
	// Err carries any non-fatal refresh failure. The Envelope is still
	// populated (stale cache or zero) when Err != nil.
	Err error
}

// Refresh runs the bounded refresh loop. It always returns a Result; it
// never panics and never returns a Go-level error.
//
// Decision tree (in order):
//
//  1. Load existing cache from opts.CachePath (missing or corrupt → zero envelope).
//  2. If refresh is disabled, or ctx is already cancelled, return the
//     loaded cache verbatim.
//  3. If the cache is fresh (LastChecked within SoftTTL), return it
//     without any HTTP call.
//  4. Call client.GetProviders with the cached ETag.
//     - On success: replace providers/etag, bump fetched_at + last_checked,
//       write envelope.
//     - On ErrNotModified: bump last_checked only, write envelope.
//     - On any transport / decode error: return the cached envelope
//       unchanged with Err set. Stale-OK.
func Refresh(ctx context.Context, opts RefreshOptions) RefreshResult {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	softTTL := opts.SoftTTL
	if softTTL <= 0 {
		softTTL = DefaultSoftTTL
	}

	cached, readErr := ReadCache(opts.CachePath)
	if readErr != nil && !IsCacheMissing(readErr) {
		// Corrupt / wrong-schema cache: act like it's missing. The error is
		// not surfaced because the autoupdate path is best-effort.
		cached = Envelope{}
	}

	// Bail out paths that never touch the network.
	if !opts.Enabled || opts.SourceURL == "" {
		return RefreshResult{Envelope: cached, FromCache: true}
	}
	if err := ctx.Err(); err != nil {
		return RefreshResult{Envelope: cached, FromCache: true, Err: err}
	}
	if !cached.LastChecked.IsZero() && now().Sub(cached.LastChecked) < softTTL {
		return RefreshResult{Envelope: cached, FromCache: true}
	}

	client := opts.Client
	if client == nil {
		client = catwalk.NewWithURL(opts.SourceURL)
	}

	providers, err := client.GetProviders(ctx, cached.ETag)
	res := RefreshResult{Envelope: cached, HitNetwork: true}
	switch {
	case errors.Is(err, catwalk.ErrNotModified):
		// Cache contents stay the same; just record that we checked.
		cached.LastChecked = now().UTC()
		cached.Source = opts.SourceURL
		if cached.SchemaVersion == 0 {
			cached.SchemaVersion = SchemaVersion
		}
		if writeErr := WriteCache(opts.CachePath, cached); writeErr != nil {
			res.Err = writeErr
		}
		res.Envelope = cached
		res.NotModified = true
		return res
	case err != nil:
		// Stale cache stays usable. Surface the error without failing.
		res.Err = err
		return res
	}

	// 200 OK with fresh providers.
	updated := Envelope{
		SchemaVersion: SchemaVersion,
		FetchedAt:     now().UTC(),
		LastChecked:   now().UTC(),
		ETag:          cached.ETag, // overwritten below if client surfaced one
		Source:        opts.SourceURL,
		Providers:     providers,
	}
	// catwalk's client doesn't expose response headers, but we can recompute
	// the ETag from the marshaled body so subsequent conditional requests
	// have a stable validator. The server's xetag implementation is
	// deterministic over the response body.
	if etag := computeETag(providers); etag != "" {
		updated.ETag = etag
	}
	if writeErr := WriteCache(opts.CachePath, updated); writeErr != nil {
		res.Err = writeErr
	}
	res.Envelope = updated
	res.Updated = true
	return res
}

// computeETag mirrors what catwalk.Etag would produce, used so we can store
// a validator for the next conditional request when the server returned 200.
// On marshal failure we return "" and let the next request go un-tagged.
func computeETag(providers []catwalk.Provider) string {
	data, err := jsonMarshal(providers)
	if err != nil {
		return ""
	}
	return catwalk.Etag(data)
}
