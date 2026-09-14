package api

import (
	"net/http"

	"github.com/qunqin/openchat/apps/server/internal/httpx"
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
	// `claude` is drawn with the Anthropic mark, `palm` with Gemini's.
	IconID string `json:"iconId"`
	Doc    string `json:"doc,omitempty"`
}

// Labels are brand names and stay untranslated: "SiliconFlow" is what it is
// called in both locales, and the Chinese vendors are listed the way they write
// themselves, which is how an operator will recognise them.
var channelTypes = []channelTypeDef{
	{ID: "openai", Label: "OpenAI", WireApi: "openai",
		Endpoint: "https://api.openai.com/v1", KeyHint: "sk-…", IconID: "openai",
		Doc: "https://platform.openai.com/api-keys"},
	{ID: "anthropic", Label: "Anthropic Claude", WireApi: "anthropic",
		Endpoint: "https://api.anthropic.com/v1", KeyHint: "sk-ant-…", IconID: "claude",
		Doc: "https://console.anthropic.com/settings/keys"},
	{ID: "gemini", Label: "Google Gemini", WireApi: "gemini",
		Endpoint: "https://generativelanguage.googleapis.com/v1beta", KeyHint: "AIza…", IconID: "gemini",
		Doc: "https://aistudio.google.com/apikey"},

	// Everything below speaks the OpenAI protocol. They are separate entries
	// because the endpoint and the key format differ, and because an operator
	// looks for the name on their invoice.
	{ID: "openrouter", Label: "OpenRouter", WireApi: "openai",
		Endpoint: "https://openrouter.ai/api/v1", KeyHint: "sk-or-…", IconID: "openrouter"},
	{ID: "deepseek", Label: "深度求索 DeepSeek", WireApi: "openai",
		Endpoint: "https://api.deepseek.com/v1", KeyHint: "sk-…", IconID: "deepseek"},
	{ID: "siliconflow", Label: "硅基流动 SiliconFlow", WireApi: "openai",
		Endpoint: "https://api.siliconflow.cn/v1", KeyHint: "sk-…", IconID: "siliconcloud"},
	{ID: "moonshot", Label: "月之暗面 Moonshot", WireApi: "openai",
		Endpoint: "https://api.moonshot.cn/v1", KeyHint: "sk-…", IconID: "moonshot"},
	{ID: "zhipu", Label: "智谱 GLM", WireApi: "openai",
		Endpoint: "https://open.bigmodel.cn/api/paas/v4", KeyHint: "…", IconID: "zhipu"},
	{ID: "dashscope", Label: "阿里云百炼 DashScope", WireApi: "openai",
		Endpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1", KeyHint: "sk-…", IconID: "qwen"},
	{ID: "volcengine", Label: "火山方舟 Volcengine", WireApi: "openai",
		Endpoint: "https://ark.cn-beijing.volces.com/api/v3", KeyHint: "…", IconID: "volcengine"},
	{ID: "minimax", Label: "MiniMax", WireApi: "openai",
		Endpoint: "https://api.minimax.chat/v1", KeyHint: "…", IconID: "minimax"},
	{ID: "xai", Label: "xAI Grok", WireApi: "openai",
		Endpoint: "https://api.x.ai/v1", KeyHint: "xai-…", IconID: "grok"},
	{ID: "groq", Label: "Groq", WireApi: "openai",
		Endpoint: "https://api.groq.com/openai/v1", KeyHint: "gsk_…", IconID: "groq"},
	{ID: "ollama", Label: "Ollama", WireApi: "openai",
		Endpoint: "http://localhost:11434/v1", KeyHint: "", IconID: "ollama"},

	// Last on purpose: it is the answer when none of the names above is the
	// one on the invoice, and it is the only entry with no endpoint to offer.
	{ID: "openai-compatible", Label: "OpenAI 兼容 / 中转", WireApi: "openai",
		Endpoint: "", KeyHint: "sk-…", IconID: ""},
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
		}
		if t.Doc != "" {
			doc := t.Doc
			info.Doc = &doc
		}
		out = append(out, info)
	}
	httpx.JSON(w, http.StatusOK, out)
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

// defaultEndpointFor is the base URL used when the operator left it blank.
func defaultEndpointFor(t ChannelType) string {
	for _, def := range channelTypes {
		if def.ID == t {
			return def.Endpoint
		}
	}
	return ""
}
