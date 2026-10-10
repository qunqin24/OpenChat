package api

import (
	"strings"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/providers/openai"
)

func TestRequestsGoOutOnTheRightProtocol(t *testing.T) {
	for _, tc := range []struct {
		kind   ChannelType
		format string
		model  string
		want   string
	}{
		// One protocol per type, whatever the model.
		{"openai", "chat", "gpt-5.5", wireOpenAI},
		{"anthropic", "chat", "claude-haiku-5-5", wireAnthropic},
		{"gemini", "chat", "gemini-3.5-flash", wireGemini},
		// The OpenAI protocol's request format is the operator's choice…
		{"openai", "responses", "gpt-5.5", wireResponses},
		{"openai-compatible", "responses", "anything", wireResponses},
		// …and other protocols have no such choice to make.
		{"anthropic", "responses", "claude-haiku-5-5", wireAnthropic},
		{"gemini", "responses", "gemini-3.5-flash", wireGemini},
		// OpenCode Go routes each model to the API that serves it, whatever
		// format the channel row carries.
		{"opencode-go", "chat", "claude-haiku-5-5", wireAnthropic},
		{"opencode-go", "chat", "gpt-5.6-luna", wireResponses},
		{"opencode-go", "chat", "Grok-4.7", wireResponses},
		{"opencode-go", "responses", "glm-5.3", wireOpenAI},
		{"opencode-go", "chat", "kimi-k3", wireOpenAI},
	} {
		got, ok := wireForModel(tc.kind, tc.format, tc.model)
		if !ok || got != tc.want {
			t.Errorf("%s/%s/%s = %q, want %q", tc.kind, tc.format, tc.model, got, tc.want)
		}
	}
	if _, ok := wireForModel("nope", "chat", "x"); ok {
		t.Error("an unknown type was routed")
	}
}

func TestOnlyTheOpenAIProtocolOffersAFormatChoice(t *testing.T) {
	for _, def := range channelTypes {
		want := def.WireApi == wireOpenAI && def.Route == nil
		if def.ID == "opencode-go" && want {
			t.Error("OpenCode Go routes per model, so it offers no format choice")
		}
		if (def.ID == "anthropic" || def.ID == "gemini") && want {
			t.Errorf("%s offers a format choice", def.ID)
		}
	}
}

// TestOpenCodeGoRoutesEveryModelItsTableNames is the Endpoints table of
// opencode.ai/docs/go as of 2026-10, row for row. The family prefixes the
// router matches on are a summary of it, and this is what catches the summary
// drifting from the table.
func TestOpenCodeGoRoutesEveryModelItsTableNames(t *testing.T) {
	table := map[string][]string{
		wireResponses: {"grok-4.7", "grok-4.6", "gpt-6-luna", "gpt-5.6-luna", "muse-spark-1.3-contributor", "muse-spark-1.2-contributor"},
		wireAnthropic: {"claude-haiku-5-5", "minimax-m3", "minimax-m2.7", "qwen3.8-max", "qwen3.8-flash", "qwen3.7-plus"},
		wireOpenAI: {"glm-5.3-flash", "glm-5.3", "glm-5.2", "kimi-k3", "kimi-k2.7-code", "kimi-k2.6",
			"longcat-2.0", "longcat-2.5-preview-free", "step-5-preview-free",
			"deepseek-v4.1-flash", "deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp",
			"mimo-v2.6-flash", "mimo-v2.6-pro", "mimo-v2.5", "mimo-v2.5-pro", "hy4-preview", "hy3", "space-bunny"},
	}
	for want, models := range table {
		for _, model := range models {
			if got := openCodeRoute(model); got != want {
				t.Errorf("%s routes to %q, want %q", model, got, want)
			}
		}
	}
}

func TestChannelTypesCarryTheirVendorsQuirks(t *testing.T) {
	// Reasoning goes back to the vendors that read it, in the field they read
	// it from, and to nobody else: most endpoints reject their own
	// reasoning_content coming back in.
	for _, id := range []ChannelType{"deepseek", "moonshot", "zhipu", "dashscope", "opencode-go", "mimo", "volcengine", "minimax"} {
		if !compatFor(id).EchoReasoning {
			t.Errorf("%s does not echo reasoning", id)
		}
	}
	for _, id := range []ChannelType{"openai", "openai-compatible", "openrouter", "ollama", "groq", "xai", "siliconflow", "modelscope"} {
		if compatFor(id).EchoReasoning {
			t.Errorf("%s echoes reasoning", id)
		}
	}

	// The thinking spelling per vendor; every other type, and any custom
	// relay, keeps the plain reasoning_effort.
	styles := map[ChannelType]openai.ThinkingStyle{
		"dashscope":  openai.ThinkingDashScope,
		"deepseek":   openai.ThinkingSwitch,
		"zhipu":      openai.ThinkingGLM,
		"volcengine": openai.ThinkingSwitch,
		"mimo":       openai.ThinkingToggle,
		"moonshot":   openai.ThinkingMoonshot,
		"openrouter": openai.ThinkingOpenRouter,
	}
	for _, def := range channelTypes {
		if got := def.Compat.Thinking; got != styles[def.ID] {
			t.Errorf("%s thinking style = %q, want %q", def.ID, got, styles[def.ID])
		}
	}

	// max_completion_tokens: OpenAI itself and Qwen, which recommends it.
	for _, def := range channelTypes {
		want := def.ID == "openai" || def.ID == "dashscope"
		if def.Compat.MaxCompletionTokens != want {
			t.Errorf("%s max_completion_tokens = %v, want %v", def.ID, def.Compat.MaxCompletionTokens, want)
		}
	}

	// OpenRouter alone sends reasoning_details, and so alone is sent them back.
	for _, def := range channelTypes {
		if want := def.ID == "openrouter"; def.Compat.ReasoningDetails != want {
			t.Errorf("%s reasoning_details = %v, want %v", def.ID, def.Compat.ReasoningDetails, want)
		}
	}

	if compatFor("nope") != (openai.Compat{}) {
		t.Error("an unknown type got a compat")
	}
}

// A vendor's sites: the first is the type's own endpoint (the form chooses it
// by leaving the base URL blank), each site once, every address https.
func TestChannelRegionsStartWithTheEndpoint(t *testing.T) {
	withRegions := 0
	for _, def := range channelTypes {
		if len(def.Regions) == 0 {
			continue
		}
		withRegions++
		if len(def.Regions) < 2 {
			t.Errorf("%s: one region is no choice", def.ID)
		}
		if def.Regions[0].Endpoint != def.Endpoint {
			t.Errorf("%s: first region %q is not the endpoint %q", def.ID, def.Regions[0].Endpoint, def.Endpoint)
		}
		seen := map[ChannelRegionId]bool{}
		for _, region := range def.Regions {
			if seen[region.Id] {
				t.Errorf("%s: region %s twice", def.ID, region.Id)
			}
			seen[region.Id] = true
			if !strings.HasPrefix(region.Endpoint, "https://") {
				t.Errorf("%s: region %s endpoint %q is not https", def.ID, region.Id, region.Endpoint)
			}
		}
	}
	if withRegions < 7 {
		t.Errorf("%d types offer sites, want MiniMax, Moonshot, Zhipu, DashScope, Volcengine, SiliconFlow and StepFun", withRegions)
	}
}
