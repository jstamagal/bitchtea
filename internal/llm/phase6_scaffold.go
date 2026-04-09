package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	fantasy "charm.land/fantasy"
	openai "github.com/charmbracelet/openai-go"
	"github.com/charmbracelet/openai-go/option"
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

// Phase6RequestAdapters projects the current runtime message/tool shapes into
// fantasy-compatible types so the later client swap can reuse the existing
// ChatStreamer seam instead of changing request assembly all at once.
type Phase6RequestAdapters struct {
	Messages []Message
	Tools    []ToolDef
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

// RequestAdapters captures the current request payload in runtime-only adapter
// helpers for the later fantasy/catwalk migration.
func (s Phase6Scaffold) RequestAdapters(messages []Message, tools []ToolDef) Phase6RequestAdapters {
	return Phase6RequestAdapters{
		Messages: append([]Message(nil), messages...),
		Tools:    append([]ToolDef(nil), tools...),
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

// FantasyMessages converts the current wire-format messages into fantasy
// messages without changing the live streaming implementation yet.
func (a Phase6RequestAdapters) FantasyMessages() []fantasy.Message {
	out := make([]fantasy.Message, 0, len(a.Messages))
	for _, msg := range a.Messages {
		out = append(out, fantasy.Message{
			Role:    fantasy.MessageRole(msg.Role),
			Content: fantasyMessageParts(msg),
		})
	}
	return out
}

// FantasyTools converts current tool definitions into fantasy function tools.
func (a Phase6RequestAdapters) FantasyTools() ([]fantasy.Tool, error) {
	out := make([]fantasy.Tool, 0, len(a.Tools))
	for _, tool := range a.Tools {
		schema, err := jsonSchemaObject(tool.Function.Parameters)
		if err != nil {
			return nil, fmt.Errorf("tool %q schema: %w", tool.Function.Name, err)
		}
		out = append(out, fantasy.FunctionTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: schema,
		})
	}
	return out, nil
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

func fantasyMessageParts(msg Message) []fantasy.MessagePart {
	parts := make([]fantasy.MessagePart, 0, 1+len(msg.ToolCalls))
	if msg.Content != "" {
		parts = append(parts, fantasy.TextPart{Text: msg.Content})
	}

	for _, call := range msg.ToolCalls {
		parts = append(parts, fantasy.ToolCallPart{
			ToolCallID: call.ID,
			ToolName:   call.Function.Name,
			Input:      call.Function.Arguments,
		})
	}

	if msg.Role == string(fantasy.MessageRoleTool) {
		return []fantasy.MessagePart{fantasy.ToolResultPart{
			ToolCallID: msg.ToolCallID,
			Output: fantasy.ToolResultOutputContentText{
				Text: msg.Content,
			},
		}}
	}

	return parts
}

func jsonSchemaObject(v interface{}) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}

	if schema, ok := v.(map[string]any); ok {
		return schema, nil
	}

	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, err
	}
	return schema, nil
}
