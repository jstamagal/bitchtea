package llm

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"strings"
)

const streamResponseBody = "(stream)"

// newDebugHTTPClient returns an HTTP client that reports request/response
// details through hook without consuming bodies before the provider sees them.
func newDebugHTTPClient(hook func(DebugInfo)) *http.Client {
	return &http.Client{
		Transport: newDebugRoundTripper(http.DefaultTransport, hook),
	}
}

func newDebugRoundTripper(base http.RoundTripper, hook func(DebugInfo)) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return debugRoundTripper{base: base, hook: hook}
}

func newDebugTransport(base http.RoundTripper, hook func(DebugInfo)) http.RoundTripper {
	return newDebugRoundTripper(base, hook)
}

type debugRoundTripper struct {
	base http.RoundTripper
	hook func(DebugInfo)
}

func (t debugRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	info := DebugInfo{
		Method:         req.Method,
		URL:            req.URL.String(),
		RequestHeaders: cloneHeader(req.Header, redactRequestHeader),
	}

	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		info.RequestBody = string(body)
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		if t.hook != nil {
			t.hook(info)
		}
		return resp, err
	}
	if resp == nil {
		if t.hook != nil {
			t.hook(info)
		}
		return resp, err
	}

	info.StatusCode = resp.StatusCode
	info.ResponseHeaders = cloneHeader(resp.Header, nil)

	switch responseBodyKind(resp.Header.Get("Content-Type")) {
	case responseBodyStream:
		info.ResponseBody = streamResponseBody
	case responseBodyCapture:
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		info.ResponseBody = string(body)
		if readErr != nil {
			resp.Body = replayReadCloser{Reader: bytes.NewReader(body), err: readErr}
		} else {
			resp.Body = io.NopCloser(bytes.NewReader(body))
		}
	}

	if t.hook != nil {
		t.hook(info)
	}
	return resp, nil
}

type responseBodyMode int

const (
	responseBodySkip responseBodyMode = iota
	responseBodyCapture
	responseBodyStream
)

func responseBodyKind(contentType string) responseBodyMode {
	mediaType := strings.ToLower(strings.TrimSpace(contentType))
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
		mediaType = strings.ToLower(parsed)
	} else if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = strings.TrimSpace(mediaType[:i])
	}

	switch mediaType {
	case "text/event-stream":
		return responseBodyStream
	case "application/json", "text/plain":
		return responseBodyCapture
	default:
		return responseBodySkip
	}
}

func cloneHeader(h http.Header, valueFn func(string, []string) []string) map[string][]string {
	if len(h) == 0 {
		return nil
	}

	clone := make(map[string][]string, len(h))
	for key, values := range h {
		copied := append([]string(nil), values...)
		if valueFn != nil {
			copied = valueFn(key, copied)
		}
		clone[key] = copied
	}
	return clone
}

func redactRequestHeader(key string, values []string) []string {
	switch http.CanonicalHeaderKey(key) {
	case "Authorization", "Proxy-Authorization":
		return redactAuthValues(values)
	case "X-Api-Key", "Api-Key", "Openai-Api-Key", "Anthropic-Api-Key":
		return repeatedRedaction(values)
	default:
		return values
	}
}

func redactAuthValues(values []string) []string {
	if len(values) == 0 {
		return []string{"[REDACTED]"}
	}

	redacted := make([]string, len(values))
	for i, value := range values {
		scheme, _, ok := strings.Cut(strings.TrimSpace(value), " ")
		if ok && scheme != "" {
			redacted[i] = scheme + " [REDACTED]"
		} else {
			redacted[i] = "[REDACTED]"
		}
	}
	return redacted
}

func repeatedRedaction(values []string) []string {
	if len(values) == 0 {
		return []string{"[REDACTED]"}
	}

	redacted := make([]string, len(values))
	for i := range redacted {
		redacted[i] = "[REDACTED]"
	}
	return redacted
}

type replayReadCloser struct {
	*bytes.Reader
	err error
}

func (r replayReadCloser) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF && r.err != nil {
		return n, r.err
	}
	return n, err
}

func (r replayReadCloser) Close() error {
	return nil
}
