package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// TestSetAPIKeyAfterColdStartReachesWire reproduces bt-vwm: the cold-start
// sequence "build client with empty apikey → SetService → SetAPIKey → first
// request" must surface the new API key on the wire. Before the SetService-
// invalidates-cache fix, the cached openai-direct provider stayed in place
// after SetService and the openaicompat path (which actually carries the
// proxy auth header for cliproxyapi) was bypassed.
func TestSetAPIKeyAfterColdStartReachesWire(t *testing.T) {
	var lastAuth atomic.Value
	lastAuth.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop","index":0}]}`)
	}))
	defer srv.Close()

	c := NewClient("", srv.URL, "gpt-4", "openai")
	c.SetService("cliproxyapi")
	c.SetAPIKey("sk-newkey-12345")

	reg := tools.NewRegistry(".", t.TempDir())
	events := make(chan StreamEvent, 100)
	go c.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, reg, events)
	for range events {
	}
	got := lastAuth.Load().(string)
	if got != "Bearer sk-newkey-12345" {
		t.Fatalf("expected Bearer sk-newkey-12345, got %q", got)
	}
}

// TestColdStartManualSetBaseURLAndAPIKey covers the manual /set path from a
// fresh process: the client may already have a cached provider/model built
// from empty base URL + API key, then /set baseurl and /set apikey must force
// the next request to rebuild and carry the new Authorization header.
func TestColdStartManualSetBaseURLAndAPIKey(t *testing.T) {
	var lastAuth atomic.Value
	lastAuth.Store("")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		auth := r.Header.Get("Authorization")
		lastAuth.Store(auth)
		if auth != "Bearer foo" {
			http.Error(w, "unauthorized: Missing Authentication header", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop","index":0}]}`)
	}))
	defer srv.Close()

	c := NewClient("", "", "gpt-4", "openai")
	c.SetService("cliproxyapi")
	if _, err := c.ensureModel(context.Background()); err != nil {
		t.Fatalf("prime empty-key model: %v", err)
	}

	c.SetBaseURL(srv.URL)
	c.SetAPIKey("foo")

	reg := tools.NewRegistry(".", t.TempDir())
	events := make(chan StreamEvent, 100)
	go c.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, reg, events)
	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("stream error after manual SetBaseURL/SetAPIKey: %v", ev.Error)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("expected exactly one request to manual base URL, got %d", hits.Load())
	}
	if got := lastAuth.Load().(string); got != "Bearer foo" {
		t.Fatalf("expected Bearer foo, got %q", got)
	}
}

// TestSetAPIKeyMidSessionRekeysWire is the mid-session swap case. After a
// successful first request with key A, SetAPIKey("B") must cause the next
// request to send key B — the cached provider/transport must be discarded.
func TestSetAPIKeyMidSessionRekeysWire(t *testing.T) {
	var lastAuth atomic.Value
	lastAuth.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop","index":0}]}`)
	}))
	defer srv.Close()

	c := NewClient("sk-old-AAAA", srv.URL, "gpt-4", "openai")
	c.SetService("cliproxyapi")

	reg := tools.NewRegistry(".", t.TempDir())

	// First turn — primes the provider/model cache with the old key.
	events1 := make(chan StreamEvent, 100)
	go c.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, reg, events1)
	for range events1 {
	}
	if got := lastAuth.Load().(string); got != "Bearer sk-old-AAAA" {
		t.Fatalf("first turn auth = %q, want Bearer sk-old-AAAA", got)
	}

	// Mid-session rekey.
	c.SetAPIKey("sk-new-BBBB")

	// Second turn — must use the new key.
	events2 := make(chan StreamEvent, 100)
	go c.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi again"}}, reg, events2)
	for range events2 {
	}
	if got := lastAuth.Load().(string); got != "Bearer sk-new-BBBB" {
		t.Fatalf("second turn auth = %q, want Bearer sk-new-BBBB (rekey did not reach the wire)", got)
	}
}

// TestSetBaseURLMidSessionReroutes confirms SetBaseURL mid-session sends
// the next request to the new host.
func TestSetBaseURLMidSessionReroutes(t *testing.T) {
	var firstHits, secondHits atomic.Int32
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"a"},"finish_reason":"stop","index":0}]}`)
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"b"},"finish_reason":"stop","index":0}]}`)
	}))
	defer srvB.Close()

	c := NewClient("sk-x", srvA.URL, "gpt-4", "openai")
	c.SetService("cliproxyapi")

	reg := tools.NewRegistry(".", t.TempDir())

	events1 := make(chan StreamEvent, 100)
	go c.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, reg, events1)
	for range events1 {
	}
	if firstHits.Load() != 1 || secondHits.Load() != 0 {
		t.Fatalf("first turn routing wrong: A=%d B=%d", firstHits.Load(), secondHits.Load())
	}

	c.SetBaseURL(srvB.URL)

	events2 := make(chan StreamEvent, 100)
	go c.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, reg, events2)
	for range events2 {
	}
	if secondHits.Load() != 1 {
		t.Fatalf("second turn did not hit new base URL: A=%d B=%d (SetBaseURL did not rebuild transport)", firstHits.Load(), secondHits.Load())
	}
}

// TestSetServiceInvalidatesCache locks in the bt-vwm fix: SetService must
// invalidate the cached provider/model because Service influences
// buildProvider's routing switch (openai-direct vs openaicompat for
// Provider == "openai").
func TestSetServiceInvalidatesCache(t *testing.T) {
	c := NewClient("a", "u", "m", "openai")
	primeCache(c)
	c.SetService("cliproxyapi")
	if c.Service != "cliproxyapi" {
		t.Fatalf("Service not updated, got %q", c.Service)
	}
	if cachedNonNil(c) {
		t.Fatal("SetService did not invalidate cached provider/model — routing depends on Service")
	}
}
