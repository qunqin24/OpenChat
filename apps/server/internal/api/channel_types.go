package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/providers/openai"
)

// channelTypeDef is what an operator picks when adding a channel: a vendor,
// not a protocol.
//
// The two are deliberately not the same list. Most vendors speak the OpenAI
// wire protocol, so `openrouter`, `deepseek` and a dozen others differ from
// `openai` only in their endpoint and their branding — collapsing them into one
// "OpenAI-compatible" option would make the operator supply from memory what we
// already know, and picking the protocol is a question they should not have to
// answer at all.
type channelTypeDef struct {
	ID       ChannelType `json:"id"`
	Label    string      `json:"label"`
	WireApi  WireApi     `json:"wireApi"`
	Endpoint string      `json:"endpoint"`
	KeyHint  string      `json:"keyHint"`
	// IconID names the brand mark, which is not always the type id —
	// `anthropic` is drawn with the Claude mark, `dashscope` with Qwen's.
	//
	// It is a brand, not a models.dev provider id: the two spell several of
	// these vendors differently (`gemini` vs `google`, `zhipu` vs `zhipuai`).
	// The front end resolves it through BRAND_ICONS in
	// `apps/web/src/components/model-icon.tsx`, so a new entry here needs a
	// line there too — without one the vendor silently draws as a letter.
	// Empty is a valid answer, and the relay entry below gives it.
	IconID string `json:"iconId"`
	Doc    string `json:"doc,omitempty"`
	// Group is the tab the picker files the vendor under. It is how an operator
	// narrows forty names down to the kind of thing they are looking for, not a
	// property the server acts on.
	Group ChannelTypeGroup `json:"group"`
	// KeyOptional marks an endpoint that is normally reached without a
	// credential — a service on the operator's own machine. It is served rather
	// than named in the front end so that the model discovery here and the
	// "needs a key" checks there cannot disagree about which vendors those are.
	KeyOptional bool `json:"keyOptional"`
	// Route picks the protocol per model, for a gateway that serves different
	// models on different APIs behind one key. Nil means every model speaks
	// WireApi, which is then also what model discovery and the catalogue use.
	Route func(modelID string) string `json:"-"`
	// Compat is what this vendor's Chat Completions endpoint does differently
	// from the OpenAI baseline: whether it wants earlier thinking handed back,
	// what it calls the output limit, how it takes a thinking level. It is the
	// zero value for a vendor that follows the baseline, which includes every
	// custom endpoint — a flag here is a claim about a named vendor, never a
	// guess about an operator's relay.
	Compat openai.Compat `json:"-"`
	// Gateway is what a gateway asks every client to send: its own user agent,
	// and the conversation's id in a session header. Nil sends neither.
	Gateway *gatewayIdentity `json:"-"`
	// Regions are a vendor's separate sites, mainland China first, each with
	// its own accounts and keys: a key from one is refused by the other, so the
	// operator picks the one their key belongs to. The first is Endpoint.
	Regions []ChannelRegion `json:"-"`
}

// gatewayIdentity is how requests to a gateway identify their client and their
// conversation. OpenCode Go refuses a request without a session header, and
// asks clients to name themselves rather than send a generic library's agent.
type gatewayIdentity struct {
	UserAgent     string
	SessionHeader string
}

// Labels are brand names and stay untranslated: "SiliconFlow" is what it is
// called in both locales, and the Chinese vendors are listed the way they write
// themselves, which is how an operator will recognise them.
var channelTypes = []channelTypeDef{
	// The model makers themselves: the key is theirs and the models are theirs.
	{ID: "openai", Label: "OpenAI", WireApi: "openai", Group: Vendor,
		Endpoint: "https://api.openai.com/v1", KeyHint: "sk-…", IconID: "openai",
		Doc:    "https://platform.openai.com/api-keys",
		Compat: openai.Compat{MaxCompletionTokens: true}},
	{ID: "anthropic", Label: "Anthropic Claude", WireApi: "anthropic", Group: Vendor,
		Endpoint: "https://api.anthropic.com/v1", KeyHint: "sk-ant-…", IconID: "claude",
		Doc: "https://console.anthropic.com/settings/keys"},
	{ID: "gemini", Label: "Google Gemini", WireApi: "gemini", Group: Vendor,
		Endpoint: "https://generativelanguage.googleapis.com/v1beta", KeyHint: "AIza…", IconID: "gemini",
		Doc: "https://aistudio.google.com/apikey"},

	// Everything below speaks the OpenAI protocol. They are separate entries
	// because the endpoint and the key format differ, and because an operator
	// looks for the name on their invoice.
	{ID: "deepseek", Label: "深度求索 DeepSeek", WireApi: "openai", Group: Vendor,
		Endpoint: "https://api.deepseek.com/v1", KeyHint: "sk-…", IconID: "deepseek",
		// With tools on the request, DeepSeek wants every earlier turn's
		// reasoning_content back; without them it ignores it.
		Compat: openai.Compat{EchoReasoning: true, Thinking: openai.ThinkingSwitch}},
	{ID: "moonshot", Label: "月之暗面 Moonshot", WireApi: "openai", Group: Vendor,
		Endpoint: "https://api.moonshot.cn/v1", KeyHint: "sk-…", IconID: "moonshot",
		// Two sites with separate accounts: platform.moonshot.cn and platform.moonshot.ai.
		Regions: []ChannelRegion{{Id: Cn, Endpoint: "https://api.moonshot.cn/v1"}, {Id: Global, Endpoint: "https://api.moonshot.ai/v1"}},
		Compat:  openai.Compat{EchoReasoning: true, Thinking: openai.ThinkingMoonshot}},
	{ID: "zhipu", Label: "智谱 GLM", WireApi: "openai", Group: Vendor,
		Endpoint: "https://open.bigmodel.cn/api/paas/v4", KeyHint: "…", IconID: "zhipu",
		// Two sites with separate accounts: open.bigmodel.cn and z.ai (docs.z.ai/api-reference/introduction).
		Regions: []ChannelRegion{{Id: Cn, Endpoint: "https://open.bigmodel.cn/api/paas/v4"}, {Id: Global, Endpoint: "https://api.z.ai/api/paas/v4"}},
		// clear_thinking:false makes GLM-4.6+ read the reasoning we send back;
		// by default it drops it (docs.z.ai/guides/capabilities/thinking-mode).
		Compat: openai.Compat{EchoReasoning: true, Thinking: openai.ThinkingGLM}},
	{ID: "dashscope", Label: "阿里云百炼 DashScope", WireApi: "openai", Group: Vendor,
		Endpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1", KeyHint: "sk-…", IconID: "qwen",
		// Two sites with separate accounts: Beijing and Singapore (help.aliyun.com/en/model-studio/base-url).
		Regions: []ChannelRegion{{Id: Cn, Endpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1"}, {Id: Global, Endpoint: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"}},
		Compat:  openai.Compat{EchoReasoning: true, MaxCompletionTokens: true, Thinking: openai.ThinkingDashScope}},
	{ID: "volcengine", Label: "火山方舟 Volcengine", WireApi: "openai", Group: Vendor,
		Endpoint: "https://ark.cn-beijing.volces.com/api/v3", KeyHint: "…", IconID: "volcengine",
		// Two sites with separate accounts: Volcengine Ark in China, BytePlus ModelArk elsewhere.
		Regions: []ChannelRegion{{Id: Cn, Endpoint: "https://ark.cn-beijing.volces.com/api/v3"}, {Id: Global, Endpoint: "https://ark.ap-southeast.bytepluses.com/api/v3"}},
		// ModelArk's deep-reasoning guide says to keep reasoning_content in
		// the history of a multi-turn conversation and let the model decide
		// whether to read it (docs.byteplus.com/en/docs/ModelArk/1449737,
		// the same page as www.volcengine.com/docs/82379/1449737). Thinking
		// is `thinking: {type}` plus `reasoning_effort`, as for DeepSeek.
		Compat: openai.Compat{EchoReasoning: true, Thinking: openai.ThinkingSwitch}},
	{ID: "minimax", Label: "MiniMax", WireApi: "openai", Group: Vendor,
		Endpoint: "https://api.minimaxi.com/v1", KeyHint: "…", IconID: "minimax",
		// Two sites with separate accounts (platform.minimaxi.com in China,
		// platform.minimax.io elsewhere); api.minimax.chat is the old name of
		// the first.
		Regions: []ChannelRegion{
			{Id: Cn, Endpoint: "https://api.minimaxi.com/v1"},
			{Id: Global, Endpoint: "https://api.minimax.io/v1"},
		},
		// MiniMax-M3.1-Flash-Preview returns its thinking in reasoning_content
		// and asks that the whole assistant message, that field included, goes
		// back in a tool loop; the M2.x models keep it inside content as
		// <think>, which is replayed as the text it is
		// (platform.minimax.io/docs/api-reference/text-openai-api).
		Compat: openai.Compat{EchoReasoning: true}},
	{ID: "xai", Label: "xAI Grok", WireApi: "openai", Group: Vendor,
		Endpoint: "https://api.x.ai/v1", KeyHint: "xai-…", IconID: "grok"},
	{ID: "mistral", Label: "Mistral AI", WireApi: "openai", Group: Vendor,
		Endpoint: "https://api.mistral.ai/v1", KeyHint: "…", IconID: "mistral"},
	{ID: "stepfun", Label: "阶跃星辰 StepFun", WireApi: "openai", Group: Vendor,
		Endpoint: "https://api.stepfun.com/v1", KeyHint: "…", IconID: "stepfun",
		// Two sites with separate accounts: stepfun.com and stepfun.ai.
		Regions: []ChannelRegion{{Id: Cn, Endpoint: "https://api.stepfun.com/v1"}, {Id: Global, Endpoint: "https://api.stepfun.ai/v1"}},
	},
	{ID: "hunyuan", Label: "腾讯混元 Hunyuan", WireApi: "openai", Group: Vendor,
		Endpoint: "https://api.hunyuan.cloud.tencent.com/v1", KeyHint: "sk-…", IconID: "hunyuan"},
	{ID: "qianfan", Label: "百度千帆 Qianfan", WireApi: "openai", Group: Vendor,
		Endpoint: "https://qianfan.baidubce.com/v2", KeyHint: "bce-v3/…", IconID: "wenxin"},
	{ID: "mimo", Label: "小米 MiMo", WireApi: "openai", Group: Vendor,
		Endpoint: "https://api.xiaomimimo.com/v1", KeyHint: "…", IconID: "mimo",
		// With thinking on, an assistant message that made tool calls must come
		// back with its full reasoning_content or the API answers 400
		// (mimo.mi.com/static/docs/quick-start/usage-guide/text-generation/deep-thinking.md).
		Compat: openai.Compat{EchoReasoning: true, Thinking: openai.ThinkingToggle}},

	// Hosts that serve other people's models, or a router in front of several
	// of them. Same protocol; the difference an operator cares about is whose
	// invoice it lands on.
	{ID: "openrouter", Label: "OpenRouter", WireApi: "openai", Group: Platform,
		Endpoint: "https://openrouter.ai/api/v1", KeyHint: "sk-or-…", IconID: "openrouter",
		// reasoning_details carries the signatures and encrypted reasoning of
		// whichever model is routed to, and goes back unmodified.
		Compat: openai.Compat{Thinking: openai.ThinkingOpenRouter, ReasoningDetails: true}},
	{ID: "siliconflow", Label: "硅基流动 SiliconFlow", WireApi: "openai", Group: Platform,
		Endpoint: "https://api.siliconflow.cn/v1", KeyHint: "sk-…", IconID: "siliconcloud",
		// Two sites with separate accounts: siliconflow.cn and siliconflow.com (docs.siliconflow.com/en/userguide/quickstart).
		Regions: []ChannelRegion{{Id: Cn, Endpoint: "https://api.siliconflow.cn/v1"}, {Id: Global, Endpoint: "https://api.siliconflow.com/v1"}},
	},
	{ID: "groq", Label: "Groq", WireApi: "openai", Group: Platform,
		Endpoint: "https://api.groq.com/openai/v1", KeyHint: "gsk_…", IconID: "groq"},
	{ID: "together", Label: "Together AI", WireApi: "openai", Group: Platform,
		Endpoint: "https://api.together.xyz/v1", KeyHint: "…", IconID: "together"},
	{ID: "fireworks", Label: "Fireworks AI", WireApi: "openai", Group: Platform,
		Endpoint: "https://api.fireworks.ai/inference/v1", KeyHint: "…", IconID: "fireworks"},
	{ID: "nvidia", Label: "NVIDIA NIM", WireApi: "openai", Group: Platform,
		Endpoint: "https://integrate.api.nvidia.com/v1", KeyHint: "nvapi-…", IconID: "nvidia"},
	{ID: "modelscope", Label: "魔搭 ModelScope", WireApi: "openai", Group: Platform,
		Endpoint: "https://api-inference.modelscope.cn/v1", KeyHint: "…", IconID: "modelscope"},
	{ID: "cerebras", Label: "Cerebras", WireApi: "openai", Group: Platform,
		Endpoint: "https://api.cerebras.ai/v1", KeyHint: "csk-…", IconID: "cerebras"},
	// A subscription gateway that serves each model on the API its table
	// names: Claude, MiniMax and Qwen on Messages, GPT, Grok and Muse Spark on
	// Responses, the other open models on Chat Completions — all under one base URL and one key. Its model list is
	// OpenAI-style, which is what WireApi says. The open models it serves on
	// Chat Completions (GLM, Kimi, DeepSeek) are the ones that want their
	// reasoning handed back. They get no vendor-style thinking controls
	// (GLM's clear_thinking, Kimi's thinking switch): opencode.ai/docs/go lists
	// the endpoints and models but nowhere says which request parameters the
	// gateway forwards, and a field it drops is a silent no-op while one it
	// rejects is a failed request.
	{ID: "opencode-go", Label: "OpenCode Go", WireApi: "openai", Group: Platform,
		Endpoint: "https://opencode.ai/zen/go/v1", KeyHint: "sk-…", IconID: "opencode",
		Doc: "https://opencode.ai/auth", Route: openCodeRoute,
		Compat:  openai.Compat{EchoReasoning: true},
		Gateway: &gatewayIdentity{UserAgent: "OpenChat/1.0", SessionHeader: "x-opencode-session"}},

	// Servers the operator runs. A local one answers without a credential, which
	// is what `KeyOptional` records.
	{ID: "ollama", Label: "Ollama", WireApi: "openai", Group: Custom, KeyOptional: true,
		Endpoint: "http://localhost:11434/v1", KeyHint: "", IconID: "ollama"},
	{ID: "lmstudio", Label: "LM Studio", WireApi: "openai", Group: Custom, KeyOptional: true,
		Endpoint: "http://localhost:1234/v1", KeyHint: "", IconID: "lmstudio"},

	// Last on purpose: it is the answer when none of the names above is the
	// one on the invoice, and it is the only entry with no endpoint to offer.
	{ID: "openai-compatible", Label: "OpenAI 兼容 / 中转", WireApi: "openai", Group: Custom,
		Endpoint: "", KeyHint: "sk-…", IconID: ""},
}

// openCodeRoute is OpenCode Go's table of which API serves which model
// (opencode.ai/docs/go, "Endpoints"), by the family prefixes it uses: Claude,
// MiniMax and Qwen3 are on Messages (the table lists Qwen as @ai-sdk/anthropic),
// GPT, Grok and Muse Spark on Responses, and the rest on Chat Completions.
func openCodeRoute(modelID string) string {
	id := strings.ToLower(modelID)
	switch {
	case strings.HasPrefix(id, "claude-"), strings.HasPrefix(id, "minimax-"), strings.HasPrefix(id, "qwen3."):
		return wireAnthropic
	case strings.HasPrefix(id, "gpt-"), strings.HasPrefix(id, "grok-"), strings.HasPrefix(id, "muse-spark-"):
		return wireResponses
	default:
		return wireOpenAI
	}
}

// The protocols a channel's requests can go out on. `responses` is not a
// WireApi a channel type declares: only a Route chooses it, per model.
const (
	wireOpenAI    = "openai"
	wireAnthropic = "anthropic"
	wireGemini    = "gemini"
	wireResponses = "responses"
)

// gatewayFor is the identity a channel type's requests carry, or nil.
func gatewayFor(t ChannelType) *gatewayIdentity {
	for _, def := range channelTypes {
		if def.ID == t {
			return def.Gateway
		}
	}
	return nil
}

// wireForModel is the protocol a channel's request for modelID goes out on:
// the type's own route if it has one, else its protocol — on the OpenAI
// protocol, in the request format the operator chose for the channel.
func wireForModel(t ChannelType, format, modelID string) (string, bool) {
	for _, def := range channelTypes {
		if def.ID == t {
			if def.Route != nil {
				return def.Route(modelID), true
			}
			if def.WireApi == wireOpenAI && format == string(Responses) {
				return wireResponses, true
			}
			return string(def.WireApi), true
		}
	}
	return "", false
}

// ListChannelTypes serves the table above.
//
// It is an endpoint rather than a constant in the front end so that the two
// cannot drift: the same table decides the default endpoint a form fills in and
// the protocol the server then speaks.
func (s *Server) ListChannelTypes(w http.ResponseWriter, r *http.Request) {
	out := make([]ChannelTypeInfo, 0, len(channelTypes))
	for _, t := range channelTypes {
		info := ChannelTypeInfo{
			Id: t.ID, Label: t.Label, WireApi: t.WireApi,
			Endpoint: t.Endpoint, KeyHint: t.KeyHint, IconId: t.IconID,
			Group: t.Group, KeyOptional: t.KeyOptional,
			// The OpenAI protocol has two request formats to choose between;
			// the others have one, and a routing type chooses per model.
			ApiFormatChoice: t.WireApi == wireOpenAI && t.Route == nil,
		}
		if t.Doc != "" {
			doc := t.Doc
			info.Doc = &doc
		}
		if len(t.Regions) > 0 {
			regions := t.Regions
			info.Regions = &regions
		}
		out = append(out, info)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// compatFor is how a channel type's Chat Completions requests differ from the
// baseline; the zero value for a type that follows it.
func compatFor(t ChannelType) openai.Compat {
	for _, def := range channelTypes {
		if def.ID == t {
			return def.Compat
		}
	}
	return openai.Compat{}
}

// wireApiFor returns the protocol a channel type speaks.
func wireApiFor(t ChannelType) (WireApi, bool) {
	for _, def := range channelTypes {
		if def.ID == t {
			return def.WireApi, true
		}
	}
	return "", false
}

// endpointFor is the base URL a channel is reached at: the operator's, or the
// type's default when left blank. An address that is only a host - no path, as
// people paste a relay's site - gets the path the protocol's endpoints live
// under: the type's default one ("/v1", "/compatible-mode/v1", ...), and for
// a type with no default (a relay) the protocol's ("/v1", Gemini's "/v1beta").
// Without it the requests went to https://relay.example/chat/completions and
// came back 404. An address with any path is used as it is: a relay may serve
// from "/api", "/openai/v1" or anywhere. "" when there is nothing to go on.
func endpointFor(t ChannelType, baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return defaultEndpointFor(t)
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return baseURL
	}
	return strings.TrimSuffix(baseURL, "/") + defaultPathFor(t)
}

// defaultPathFor is the path a type's endpoints live under (see endpointFor).
func defaultPathFor(t ChannelType) string {
	if def, err := url.Parse(defaultEndpointFor(t)); err == nil && def.Host != "" && def.Path != "" && def.Path != "/" {
		return strings.TrimSuffix(def.Path, "/")
	}
	if wire, _ := wireApiFor(t); wire == "gemini" {
		return "/v1beta"
	}
	return "/v1"
}

// defaultEndpointFor is the base URL used when the operator left it blank.
func defaultEndpointFor(t ChannelType) string {
	for _, def := range channelTypes {
		if def.ID == t {
			return def.Endpoint
		}
	}
	return ""
}

// keyOptionalFor reports whether the type is normally reached without a key.
func keyOptionalFor(t ChannelType) bool {
	for _, def := range channelTypes {
		if def.ID == t {
			return def.KeyOptional
		}
	}
	return false
}
