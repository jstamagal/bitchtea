package llm

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
)

// providerConfig is the minimal slice of Client state buildProvider needs.
// Tests inject directly via this struct.
type providerConfig struct {
	provider string
	service  string // upstream service identity; if set, routes by service instead of host
	apiKey   string
	baseURL  string
	http     *http.Client // nil = SDK default
}

// buildProvider returns a fantasy.Provider for cfg. When cfg.service is set,
// routing is by service identity (Phase 9). Otherwise it falls back to
// host-based routing on cfg.baseURL for backwards compatibility.
func buildProvider(cfg providerConfig) (fantasy.Provider, error) {
	switch cfg.provider {
	case "anthropic":
		return buildAnthropicProvider(cfg)
	case "openai", "":
		if cfg.service != "" {
			return routeByService(cfg)
		}
		return routeOpenAICompatible(cfg)
	}
	return nil, fmt.Errorf("unsupported provider: %q", cfg.provider)
}

// buildAnthropicProvider builds an anthropic.Provider with baseURL override.
func buildAnthropicProvider(cfg providerConfig) (fantasy.Provider, error) {
	baseURL := stripV1Suffix(cfg.baseURL)
	opts := []anthropic.Option{anthropic.WithAPIKey(cfg.apiKey)}
	if baseURL != "" {
		opts = append(opts, anthropic.WithBaseURL(baseURL))
	}
	if cfg.http != nil {
		opts = append(opts, anthropic.WithHTTPClient(cfg.http))
	}
	return anthropic.New(opts...)
}

// routeByService selects the fantasy provider package based on the Service
// identity. This replaces host-based URL sniffing for Phase 9.
func routeByService(cfg providerConfig) (fantasy.Provider, error) {
	switch cfg.service {
	case "openrouter":
		opts := []openrouter.Option{openrouter.WithAPIKey(cfg.apiKey)}
		if cfg.http != nil {
			opts = append(opts, openrouter.WithHTTPClient(cfg.http))
		}
		return openrouter.New(opts...)

	case "vercel":
		opts := []vercel.Option{vercel.WithAPIKey(cfg.apiKey)}
		if cfg.http != nil {
			opts = append(opts, vercel.WithHTTPClient(cfg.http))
		}
		return vercel.New(opts...)

	default:
		// openai, ollama, zai-openai, aihubmix, copilot, xai, custom, etc.
		// All use openaicompat with baseURL override.
		opts := []openaicompat.Option{
			openaicompat.WithAPIKey(cfg.apiKey),
			openaicompat.WithBaseURL(cfg.baseURL),
		}
		if cfg.http != nil {
			opts = append(opts, openaicompat.WithHTTPClient(cfg.http))
		}
		return openaicompat.New(opts...)
	}
}

// routeOpenAICompatible picks between openai/openrouter/vercel/openaicompat
// based on the parsed host of cfg.baseURL. Empty baseURL → upstream OpenAI.
func routeOpenAICompatible(cfg providerConfig) (fantasy.Provider, error) {
	if cfg.baseURL == "" {
		opts := []openai.Option{
			openai.WithAPIKey(cfg.apiKey),
			openai.WithUseResponsesAPI(),
		}
		if cfg.http != nil {
			opts = append(opts, openai.WithHTTPClient(cfg.http))
		}
		return openai.New(opts...)
	}
	cfgHost, err := hostOf(cfg.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL %q: %w", cfg.baseURL, err)
	}

	switch cfgHost {
	case hostOfMust(openai.DefaultURL):
		opts := []openai.Option{
			openai.WithAPIKey(cfg.apiKey),
			openai.WithBaseURL(cfg.baseURL),
			openai.WithUseResponsesAPI(),
		}
		if cfg.http != nil {
			opts = append(opts, openai.WithHTTPClient(cfg.http))
		}
		return openai.New(opts...)

	case hostOfMust(openrouter.DefaultURL):
		opts := []openrouter.Option{openrouter.WithAPIKey(cfg.apiKey)}
		if cfg.http != nil {
			opts = append(opts, openrouter.WithHTTPClient(cfg.http))
		}
		return openrouter.New(opts...)

	case hostOfMust(vercel.DefaultURL):
		opts := []vercel.Option{vercel.WithAPIKey(cfg.apiKey)}
		if cfg.http != nil {
			opts = append(opts, vercel.WithHTTPClient(cfg.http))
		}
		return vercel.New(opts...)

	default:
		// ollama, zai-openai, copilot, aihubmix, avian, cortecs, huggingface,
		// ionet, nebius, synthetic, venice, xai, custom local, etc.
		opts := []openaicompat.Option{
			openaicompat.WithAPIKey(cfg.apiKey),
			openaicompat.WithBaseURL(cfg.baseURL),
		}
		if cfg.http != nil {
			opts = append(opts, openaicompat.WithHTTPClient(cfg.http))
		}
		return openaicompat.New(opts...)
	}
}

// hostOf parses rawURL and returns the lowercased host. Used to compare against
// each fantasy provider's DefaultURL constant.
func hostOf(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return strings.ToLower(u.Host), nil
}

// hostOfMust panics on a malformed constant URL — only used on package
// constants that are known good at compile time.
func hostOfMust(rawURL string) string {
	h, err := hostOf(rawURL)
	if err != nil {
		panic(fmt.Sprintf("hostOfMust: bad URL %q: %v", rawURL, err))
	}
	return h
}

// stripV1Suffix removes a trailing "/v1" or "/v1/" from rawURL. fantasy's
// anthropic.DefaultURL is "https://api.anthropic.com" (no /v1) and the
// underlying SDK appends /v1 itself, so a config that hard-codes /v1 would
// double-prefix. Used only on the anthropic baseURL path.
func stripV1Suffix(rawURL string) string {
	return strings.TrimSuffix(strings.TrimSuffix(rawURL, "/v1/"), "/v1")
}
