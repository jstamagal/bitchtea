package llm

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingRoundTripper is a fake base RoundTripper that captures the request
// it receives (so tests can assert the body/headers passed downstream are
// untouched) and returns a canned response/error.
type recordingRoundTripper struct {
	gotMethod   string
	gotURL      string
	gotBody     []byte
	gotHeaders  http.Header
	resp        *http.Response
	err         error
	readBodyErr error
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.gotMethod = req.Method
	r.gotURL = req.URL.String()
	r.gotHeaders = req.Header.Clone()
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			r.readBodyErr = err
		}
		r.gotBody = body
	}
	if r.err != nil {
		return nil, r.err
	}
	return r.resp, nil
}

// makeResp constructs a synthetic *http.Response with the given content type
// and body. Callers are responsible for closing it via the wrapper.
func makeResp(status int, contentType, body string) *http.Response {
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestDebugRoundTripper_CapturesRequestBodyWithoutConsuming verifies the
// debug wrapper captures the request body but the downstream RoundTripper
// still sees the same bytes.
func TestDebugRoundTripper_CapturesRequestBodyWithoutConsuming(t *testing.T) {
	const wantBody = `{"hello":"world"}`

	base := &recordingRoundTripper{
		resp: makeResp(200, "application/json", `{"ok":true}`),
	}

	var got DebugInfo
	rt := newDebugRoundTripper(base, func(info DebugInfo) { got = info })

	req, err := http.NewRequest("POST", "https://example.test/v1/chat", strings.NewReader(wantBody))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()

	// Downstream saw the body unchanged.
	if string(base.gotBody) != wantBody {
		t.Fatalf("downstream body = %q, want %q", base.gotBody, wantBody)
	}
	if base.readBodyErr != nil {
		t.Fatalf("downstream body read error: %v", base.readBodyErr)
	}

	// Debug captured the body too.
	if got.RequestBody != wantBody {
		t.Fatalf("captured request body = %q, want %q", got.RequestBody, wantBody)
	}
	if got.Method != "POST" {
		t.Fatalf("captured method = %q, want POST", got.Method)
	}
	if got.URL != "https://example.test/v1/chat" {
		t.Fatalf("captured URL = %q", got.URL)
	}

	// GetBody must replay the same body (lets http.Client retry).
	if req.GetBody == nil {
		t.Fatal("GetBody not set")
	}
	rc, err := req.GetBody()
	if err != nil {
		t.Fatalf("GetBody: %v", err)
	}
	replayed, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(replayed) != wantBody {
		t.Fatalf("GetBody replay = %q, want %q", replayed, wantBody)
	}
}

// TestDebugRoundTripper_CapturesJSONAndPlainResponseBodies verifies the
// wrapper captures application/json and text/plain response bodies and that
// the caller can still read the response stream after capture.
func TestDebugRoundTripper_CapturesJSONAndPlainResponseBodies(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		{"json", "application/json", `{"answer":42}`},
		{"json with charset", "application/json; charset=utf-8", `{"answer":42}`},
		{"plain", "text/plain", "hello there"},
		{"plain with charset", "text/plain; charset=utf-8", "hello again"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := &recordingRoundTripper{resp: makeResp(200, tc.contentType, tc.body)}

			var got DebugInfo
			rt := newDebugRoundTripper(base, func(info DebugInfo) { got = info })

			req, _ := http.NewRequest("GET", "https://example.test/", nil)
			resp, err := rt.RoundTrip(req)
			if err != nil {
				t.Fatalf("RoundTrip: %v", err)
			}

			// Debug saw the body content.
			if got.ResponseBody != tc.body {
				t.Fatalf("captured response body = %q, want %q", got.ResponseBody, tc.body)
			}
			if got.StatusCode != 200 {
				t.Fatalf("captured status = %d, want 200", got.StatusCode)
			}

			// Caller can still read the body — wrapper must have replaced it
			// with a NopCloser over a fresh reader.
			read, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}
			_ = resp.Body.Close()
			if string(read) != tc.body {
				t.Fatalf("downstream body = %q, want %q", read, tc.body)
			}
		})
	}
}

// TestDebugRoundTripper_SkipsEventStream verifies SSE bodies are NOT consumed
// (so the live stream still works) and the captured ResponseBody is the
// "(stream)" sentinel.
func TestDebugRoundTripper_SkipsEventStream(t *testing.T) {
	cases := []string{"text/event-stream", "text/event-stream; charset=utf-8"}
	for _, ct := range cases {
		t.Run(ct, func(t *testing.T) {
			const liveBody = "data: chunk1\n\ndata: chunk2\n\n"
			base := &recordingRoundTripper{resp: makeResp(200, ct, liveBody)}

			var got DebugInfo
			rt := newDebugRoundTripper(base, func(info DebugInfo) { got = info })

			req, _ := http.NewRequest("GET", "https://example.test/stream", nil)
			resp, err := rt.RoundTrip(req)
			if err != nil {
				t.Fatalf("RoundTrip: %v", err)
			}

			if got.ResponseBody != streamResponseBody {
				t.Fatalf("captured response body = %q, want %q", got.ResponseBody, streamResponseBody)
			}

			// The stream body must be untouched and still readable in full.
			read, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read SSE body: %v", err)
			}
			_ = resp.Body.Close()
			if string(read) != liveBody {
				t.Fatalf("downstream SSE body = %q, want %q", read, liveBody)
			}
		})
	}
}

// TestDebugRoundTripper_RedactsSensitiveHeaders covers the three header
// classes redacted by debug.go and verifies the original request still has
// the un-redacted headers (downstream provider needs the real key).
func TestDebugRoundTripper_RedactsSensitiveHeaders(t *testing.T) {
	base := &recordingRoundTripper{resp: makeResp(200, "application/json", "{}")}

	var got DebugInfo
	rt := newDebugRoundTripper(base, func(info DebugInfo) { got = info })

	req, _ := http.NewRequest("POST", "https://example.test/", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer sk-secret-1234567890")
	req.Header.Set("Proxy-Authorization", "Basic supersecret==")
	req.Header.Set("X-Api-Key", "xak-deadbeef")
	req.Header.Set("Anthropic-Api-Key", "ant-livekey")
	req.Header.Set("Openai-Api-Key", "sk-openai-key")
	req.Header.Set("Api-Key", "raw-key")
	req.Header.Set("X-Trace-Id", "trace-keep-me") // must NOT be redacted
	req.Header.Set("Content-Type", "application/json")

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()

	// Captured (debug-side) headers must be redacted.
	checks := map[string]string{
		"Authorization":       "Bearer [REDACTED]",
		"Proxy-Authorization": "Basic [REDACTED]",
		"X-Api-Key":           "[REDACTED]",
		"Anthropic-Api-Key":   "[REDACTED]",
		"Openai-Api-Key":      "[REDACTED]",
		"Api-Key":             "[REDACTED]",
	}
	for header, want := range checks {
		vals, ok := got.RequestHeaders[http.CanonicalHeaderKey(header)]
		if !ok || len(vals) == 0 {
			t.Fatalf("captured headers missing %s: %+v", header, got.RequestHeaders)
		}
		if vals[0] != want {
			t.Errorf("captured %s = %q, want %q", header, vals[0], want)
		}
	}

	// Non-sensitive headers must pass through unchanged.
	if got.RequestHeaders["X-Trace-Id"][0] != "trace-keep-me" {
		t.Errorf("X-Trace-Id should not be redacted: %v", got.RequestHeaders["X-Trace-Id"])
	}

	// Make sure no original secret leaked into the captured map.
	for _, vals := range got.RequestHeaders {
		for _, v := range vals {
			if strings.Contains(v, "sk-secret") ||
				strings.Contains(v, "supersecret") ||
				strings.Contains(v, "xak-deadbeef") ||
				strings.Contains(v, "ant-livekey") ||
				strings.Contains(v, "sk-openai-key") ||
				strings.Contains(v, "raw-key") {
				t.Fatalf("secret leaked into captured headers: %q", v)
			}
		}
	}

	// Downstream RoundTripper must have received the *real* values (not the
	// redacted ones) so the upstream API actually authenticates.
	if got := base.gotHeaders.Get("Authorization"); got != "Bearer sk-secret-1234567890" {
		t.Errorf("downstream Authorization redacted (should be raw): %q", got)
	}
	if got := base.gotHeaders.Get("X-Api-Key"); got != "xak-deadbeef" {
		t.Errorf("downstream X-Api-Key redacted (should be raw): %q", got)
	}
}

// TestDebugRoundTripper_TransportError makes the inner RoundTripper return
// (nil, err). The wrapper must surface the error, fire the hook (with the
// request info already captured), and not panic.
func TestDebugRoundTripper_TransportError(t *testing.T) {
	wantErr := errors.New("boom: connection refused")
	base := &recordingRoundTripper{err: wantErr}

	var calls int
	var got DebugInfo
	rt := newDebugRoundTripper(base, func(info DebugInfo) {
		calls++
		got = info
	})

	req, _ := http.NewRequest("POST", "https://example.test/v1", strings.NewReader(`{"x":1}`))
	req.Header.Set("Authorization", "Bearer sk-leak")

	resp, err := rt.RoundTrip(req)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if resp != nil {
		t.Fatalf("resp must be nil on transport error, got %+v", resp)
	}
	if calls != 1 {
		t.Fatalf("hook called %d times, want 1", calls)
	}
	// Request side should still be captured (URL, headers, body).
	if got.URL != "https://example.test/v1" {
		t.Errorf("captured URL = %q", got.URL)
	}
	if got.RequestBody != `{"x":1}` {
		t.Errorf("captured body = %q", got.RequestBody)
	}
	if got.RequestHeaders[http.CanonicalHeaderKey("Authorization")][0] != "Bearer [REDACTED]" {
		t.Errorf("Authorization not redacted on error path: %v", got.RequestHeaders["Authorization"])
	}
	// Response side fields should be zero — no upstream response existed.
	if got.StatusCode != 0 || got.ResponseBody != "" || got.ResponseHeaders != nil {
		t.Errorf("response fields must be zero on transport error: %+v", got)
	}
}

// TestDebugRoundTripper_NilResponseNoError covers the rare/odd case where a
// transport returns (nil, nil). The wrapper must not panic, fire the hook
// with request info, and return (nil, nil) cleanly.
func TestDebugRoundTripper_NilResponseNoError(t *testing.T) {
	base := &recordingRoundTripper{resp: nil, err: nil}

	var calls int
	rt := newDebugRoundTripper(base, func(info DebugInfo) { calls++ })

	req, _ := http.NewRequest("GET", "https://example.test/", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil", resp)
	}
	if calls != 1 {
		t.Fatalf("hook called %d times, want 1", calls)
	}
}

// TestDebugRoundTripper_NilHookDoesNotPanic ensures a nil hook is tolerated
// across all branches (success, transport error, nil response) and that errors
// and responses are faithfully propagated through the debug wrapper.
func TestDebugRoundTripper_NilHookDoesNotPanic(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		base := &recordingRoundTripper{resp: makeResp(200, "application/json", "{}")}
		rt := newDebugRoundTripper(base, nil)
		req, _ := http.NewRequest("GET", "https://example.test/", nil)
		resp, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response on success")
		}
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200 status, got %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})
	t.Run("transport error", func(t *testing.T) {
		base := &recordingRoundTripper{err: errors.New("nope")}
		rt := newDebugRoundTripper(base, nil)
		req, _ := http.NewRequest("GET", "https://example.test/", nil)
		resp, err := rt.RoundTrip(req)
		if err == nil {
			t.Fatal("expected error to be propagated")
		}
		if err.Error() != "nope" {
			t.Fatalf("expected 'nope' error, got %v", err)
		}
		if resp != nil {
			t.Fatal("expected nil response on transport error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		base := &recordingRoundTripper{resp: nil, err: nil}
		rt := newDebugRoundTripper(base, nil)
		req, _ := http.NewRequest("GET", "https://example.test/", nil)
		resp, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("expected nil error on nil response, got %v", err)
		}
		if resp != nil {
			t.Fatal("expected nil response to be preserved as nil")
		}
	})
}

// TestDebugRoundTripper_UnknownContentTypeSkipsCapture verifies that body
// kinds outside json/text/sse fall through the skip branch (ResponseBody
// stays empty, downstream body remains intact).
func TestDebugRoundTripper_UnknownContentTypeSkipsCapture(t *testing.T) {
	const blob = "\x00\x01\x02binary"
	base := &recordingRoundTripper{resp: makeResp(200, "application/octet-stream", blob)}

	var got DebugInfo
	rt := newDebugRoundTripper(base, func(info DebugInfo) { got = info })

	req, _ := http.NewRequest("GET", "https://example.test/blob", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got.ResponseBody != "" {
		t.Errorf("captured body = %q, want empty for octet-stream", got.ResponseBody)
	}
	read, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(read) != blob {
		t.Errorf("downstream body altered: %q", read)
	}
}

// TestNewDebugRoundTripper_DefaultsToHTTPDefaultTransport verifies the nil
// base case picks http.DefaultTransport.
func TestNewDebugRoundTripper_DefaultsToHTTPDefaultTransport(t *testing.T) {
	rt := newDebugRoundTripper(nil, nil)
	drt, ok := rt.(debugRoundTripper)
	if !ok {
		t.Fatalf("got %T, want debugRoundTripper", rt)
	}
	if drt.base != http.DefaultTransport {
		t.Fatalf("base = %v, want http.DefaultTransport", drt.base)
	}
}

// TestNewDebugHTTPClient verifies the convenience constructor returns a
// usable *http.Client whose Transport is a debug wrapper.
func TestNewDebugHTTPClient(t *testing.T) {
	c := newDebugHTTPClient(func(DebugInfo) {})
	if c == nil || c.Transport == nil {
		t.Fatal("newDebugHTTPClient returned nil/incomplete client")
	}
	if _, ok := c.Transport.(debugRoundTripper); !ok {
		t.Fatalf("Transport = %T, want debugRoundTripper", c.Transport)
	}
}

// TestRedactAuthValues_EmptySlice covers the early-return path that returns
// "[REDACTED]" for an empty input.
func TestRedactAuthValues_EmptySlice(t *testing.T) {
	got := redactAuthValues(nil)
	if len(got) != 1 || got[0] != "[REDACTED]" {
		t.Fatalf("redactAuthValues(nil) = %v", got)
	}
}

// TestRedactAuthValues_NoScheme covers the case where the value has no
// space-separated scheme — we still get "[REDACTED]" (no scheme prefix).
func TestRedactAuthValues_NoScheme(t *testing.T) {
	got := redactAuthValues([]string{"singletoken"})
	if len(got) != 1 || got[0] != "[REDACTED]" {
		t.Fatalf("redactAuthValues = %v", got)
	}
}

// TestRepeatedRedaction_EmptySlice covers the early-return path for repeated
// redaction.
func TestRepeatedRedaction_EmptySlice(t *testing.T) {
	got := repeatedRedaction(nil)
	if len(got) != 1 || got[0] != "[REDACTED]" {
		t.Fatalf("repeatedRedaction(nil) = %v", got)
	}
}

// TestResponseBodyKind_MalformedContentType exercises the fallback that
// strips a stray ';' when mime.ParseMediaType fails.
func TestResponseBodyKind_MalformedContentType(t *testing.T) {
	// mime.ParseMediaType rejects this (no media subtype after the slash
	// before the malformed param) so we hit the manual semicolon strip.
	if got := responseBodyKind("application/json;not a valid param"); got != responseBodyCapture {
		t.Errorf("malformed json content type: kind = %v, want capture", got)
	}
	if got := responseBodyKind(""); got != responseBodySkip {
		t.Errorf("empty content type: kind = %v, want skip", got)
	}
	if got := responseBodyKind("TEXT/EVENT-STREAM"); got != responseBodyStream {
		t.Errorf("uppercase event-stream: kind = %v, want stream", got)
	}
}

// TestDebugRoundTripper_RealHTTPServer is an end-to-end sanity check using
// httptest.NewServer + a real *http.Client wired to a debug transport.
func TestDebugRoundTripper_RealHTTPServer(t *testing.T) {
	var serverSawBody string
	var serverSawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		serverSawBody = string(body)
		serverSawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"reply":"hi"}`))
	}))
	defer srv.Close()

	var got DebugInfo
	client := &http.Client{Transport: newDebugTransport(http.DefaultTransport, func(info DebugInfo) { got = info })}

	req, _ := http.NewRequest("POST", srv.URL+"/v1/chat", strings.NewReader(`{"q":"ping"}`))
	req.Header.Set("Authorization", "Bearer sk-end-to-end")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if string(body) != `{"reply":"hi"}` {
		t.Errorf("client got body = %q", body)
	}
	if serverSawBody != `{"q":"ping"}` {
		t.Errorf("server saw body = %q", serverSawBody)
	}
	if serverSawAuth != "Bearer sk-end-to-end" {
		t.Errorf("server saw auth = %q (should be unredacted)", serverSawAuth)
	}
	if got.RequestBody != `{"q":"ping"}` {
		t.Errorf("debug captured request body = %q", got.RequestBody)
	}
	if got.ResponseBody != `{"reply":"hi"}` {
		t.Errorf("debug captured response body = %q", got.ResponseBody)
	}
	if got.RequestHeaders[http.CanonicalHeaderKey("Authorization")][0] != "Bearer [REDACTED]" {
		t.Errorf("debug Authorization not redacted: %v", got.RequestHeaders["Authorization"])
	}
	if got.StatusCode != 200 {
		t.Errorf("debug status = %d", got.StatusCode)
	}
}
