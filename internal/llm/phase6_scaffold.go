package llm

import (
	"context"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	fantasy "charm.land/fantasy"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Service identifies the actual upstream service behind a wire-format provider.
// This is runtime-only scaffolding for Phase 6; profile persistence still uses
// the existing Provider/BaseURL shape.
type Service string

const (
	ServiceUnknown      Service = ""
	ServiceOpenAI       Service = "openai"
	ServiceAnthropic    Service = "anthropic"
	ServiceOpenRouter   Service = "openrouter"
	ServiceOllama       Service = "ollama"
	ServiceAIHubMix     Service = "aihubmix"
	ServiceAvian        Service = "avian"
	ServiceCopilot      Service = "copilot"
	ServiceCortecs      Service = "cortecs"
	ServiceHuggingFace  Service = "huggingface"
	ServiceIONet        Service = "ionet"
	ServiceNebius       Service = "nebius"
	ServiceSynthetic    Service = "synthetic"
	ServiceVenice       Service = "venice"
	ServiceVercel       Service = "vercel"
	ServiceXAI          Service = "xai"
	ServiceZAIOpenAI    Service = "zai-openai"
	ServiceZAIAnthropic Service = "zai-anthropic"
)

// RuntimeIdentity captures the provider wire format plus the best-effort
// service identity derived from the current config shape.
type RuntimeIdentity struct {
	APIKey       string
	BaseURL      string
	Model        string
	WireProvider string
	Service      Service
}

// FantasyProviderFactory is the seam for later swapping the bespoke client over
// to fantasy providers without changing the agent loop contract yet.
type FantasyProviderFactory func(context.Context, RuntimeIdentity) (fantasy.Provider, error)

// Phase6Scaffold bundles low-risk helpers for the later provider migration.
// The current runtime does not use these objects for live requests yet.
type Phase6Scaffold struct {
	Identity RuntimeIdentity
}

// NewPhase6Scaffold derives runtime-only provider metadata from the existing
// persisted config shape without changing save/load behavior.
func NewPhase6Scaffold(apiKey, baseURL, model, provider string) Phase6Scaffold {
	return Phase6Scaffold{
		Identity: RuntimeIdentity{
			APIKey:       apiKey,
			BaseURL:      strings.TrimSpace(baseURL),
			Model:        strings.TrimSpace(model),
			WireProvider: strings.TrimSpace(provider),
			Service:      ResolveService(provider, baseURL),
		},
	}
}

// RuntimeIdentity exposes the provider metadata needed by later migration
// steps while the current client still owns the live streaming path.
func (c *Client) RuntimeIdentity() RuntimeIdentity {
	return NewPhase6Scaffold(c.APIKey, c.BaseURL, c.Model, c.Provider).Identity
}

// CatwalkProvider projects the current runtime config into catwalk's provider
// shape so later work can reuse provider metadata and model defaults.
func (s Phase6Scaffold) CatwalkProvider() catwalk.Provider {
	return catwalk.Provider{
		Name:                string(s.Identity.Service),
		ID:                  catwalk.InferenceProvider(s.Identity.Service),
		APIKey:              s.Identity.APIKey,
		APIEndpoint:         s.Identity.BaseURL,
		Type:                s.catwalkType(),
		DefaultLargeModelID: s.Identity.Model,
		DefaultSmallModelID: s.Identity.Model,
		Models: []catwalk.Model{{
			ID:               s.Identity.Model,
			Name:             s.Identity.Model,
			ContextWindow:    0,
			DefaultMaxTokens: 0,
		}},
	}
}

// OpenAIOptions prepares request options for the later openai-go migration.
func (s Phase6Scaffold) OpenAIOptions() []option.RequestOption {
	opts := []option.RequestOption{
		option.WithBaseURL(s.Identity.BaseURL),
	}
	if strings.TrimSpace(s.Identity.APIKey) != "" {
		opts = append(opts, option.WithAPIKey(s.Identity.APIKey))
	}
	return opts
}

// OpenAIClient builds an openai-go client without wiring it into runtime use.
func (s Phase6Scaffold) OpenAIClient() openai.Client {
	return openai.NewClient(s.OpenAIOptions()...)
}

// ResolveService converts the current provider/base-URL pair into a best-effort
// upstream identity. This keeps existing URL sniffing in one place so the later
// migration can replace it with persisted service identity in a single step.
func ResolveService(provider, baseURL string) Service {
	wire := strings.TrimSpace(strings.ToLower(provider))
	base := strings.TrimSpace(strings.ToLower(baseURL))

	switch {
	case wire == "anthropic" && strings.Contains(base, "api.z.ai/api/anthropic"):
		return ServiceZAIAnthropic
	case wire == "anthropic":
		return ServiceAnthropic
	case strings.HasPrefix(base, "http://localhost:11434/"), strings.HasPrefix(base, "http://127.0.0.1:11434/"):
		return ServiceOllama
	case strings.Contains(base, "openrouter.ai"):
		return ServiceOpenRouter
	case strings.Contains(base, "aihubmix.com"):
		return ServiceAIHubMix
	case strings.Contains(base, "api.avian.io"):
		return ServiceAvian
	case strings.Contains(base, "api.githubcopilot.com"):
		return ServiceCopilot
	case strings.Contains(base, "api.cortecs.ai"):
		return ServiceCortecs
	case strings.Contains(base, "router.huggingface.co"):
		return ServiceHuggingFace
	case strings.Contains(base, "api.intelligence.io.solutions"):
		return ServiceIONet
	case strings.Contains(base, "tokenfactory.nebius.com"):
		return ServiceNebius
	case strings.Contains(base, "api.synthetic.new"):
		return ServiceSynthetic
	case strings.Contains(base, "api.venice.ai"):
		return ServiceVenice
	case strings.Contains(base, "ai-gateway.vercel.sh"):
		return ServiceVercel
	case strings.Contains(base, "api.x.ai"):
		return ServiceXAI
	case strings.Contains(base, "api.z.ai/api/coding/paas"):
		return ServiceZAIOpenAI
	case wire == "openai":
		return ServiceOpenAI
	default:
		return ServiceUnknown
	}
}

func (s Phase6Scaffold) catwalkType() catwalk.Type {
	switch s.Identity.Service {
	case ServiceAnthropic, ServiceZAIAnthropic:
		return catwalk.TypeAnthropic
	case ServiceOpenRouter:
		return catwalk.TypeOpenRouter
	default:
		if strings.EqualFold(s.Identity.WireProvider, "openai") {
			return catwalk.TypeOpenAICompat
		}
		return catwalk.TypeOpenAI
	}
}
