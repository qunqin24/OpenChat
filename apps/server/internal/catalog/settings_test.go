package catalog

import (
	"slices"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

func TestDefaultKind(t *testing.T) {
	cases := map[string]struct {
		id     string
		output []string
		want   Kind
	}{
		"chat":       {"claude-haiku-5-5", []string{"text"}, KindText},
		"image":      {"gpt-image-2", []string{"image"}, KindImage},
		"embedding":  {"text-embedding-3-large", nil, KindEmbedding},
		"rerank":     {"bge-reranker-v2", nil, KindRerank},
		"text+image": {"gemini-3-flash-image", []string{"text", "image"}, KindText},
	}
	for name, c := range cases {
		if got := DefaultKind(c.id, c.output); got != c.want {
			t.Errorf("%s: DefaultKind = %s, want %s", name, got, c.want)
		}
	}
}

func TestResolveRestrictsLevels(t *testing.T) {
	defaults := Defaults{
		Kind:      KindText,
		Reasoning: true,
		Profile:   ParseReasoningOptions([]byte(`[{"type":"effort","values":["low","medium","high","max"]}]`)),
	}

	if got := Resolve(defaults, Override{}).Profile.Levels(); len(got) != 4 {
		t.Errorf("untouched: levels %v, want all four", got)
	}

	restricted := Resolve(defaults, Override{ReasoningEfforts: []llm.ReasoningEffort{"max", "low", "minimal"}})
	if got := restricted.Profile.Levels(); !slices.Equal(got, []llm.ReasoningEffort{"low", "max"}) {
		t.Errorf("restricted: levels %v, want the allowed ones the model has, lowest first", got)
	}
	if restricted.Profile.Offers("medium") {
		t.Error("restricted: medium offered, want it withheld")
	}

	none := Resolve(defaults, Override{ReasoningEfforts: []llm.ReasoningEffort{}})
	if got := none.Profile.Levels(); len(got) != 0 {
		t.Errorf("empty list: levels %v, want none", got)
	}

	off := false
	if got := Resolve(defaults, Override{Reasoning: &off}).Profile.Levels(); len(got) != 0 {
		t.Errorf("reasoning off: levels %v, want none", got)
	}
}

func TestResolveVouchesForAnUnknownModel(t *testing.T) {
	on := true
	settings := Resolve(UnknownDefaults("my-relay-model"), Override{
		Reasoning:        &on,
		ReasoningEfforts: []llm.ReasoningEffort{"high", "low"},
	})
	if got := settings.Profile.Levels(); !slices.Equal(got, []llm.ReasoningEffort{"low", "high"}) {
		t.Errorf("levels %v, want the operator's", got)
	}
	if o := settings.Profile.Options("high"); o == nil || o.Effort != "high" {
		t.Errorf("options %+v, want an effort", o)
	}
	if !settings.ToolCall {
		t.Error("an unknown model keeps its tools until the operator says otherwise")
	}
}

func TestResolveLimitsAndModalities(t *testing.T) {
	defaults := Defaults{Kind: KindText, ContextLimit: 200000, OutputLimit: 64000,
		InputModalities: []string{"text", "image", "pdf"}}
	limit := int64(8000)
	settings := Resolve(defaults, Override{OutputLimit: &limit, InputModalities: []string{"video", "image"}})
	if settings.OutputLimit != 8000 || !settings.OutputLimitSet || settings.ContextLimit != 200000 {
		t.Errorf("limits %+v", settings)
	}
	if !slices.Equal(settings.InputModalities, []string{"image", "video"}) {
		t.Errorf("modalities %v", settings.InputModalities)
	}
	if got := Resolve(defaults, Override{}).InputModalities; !slices.Equal(got, []string{"image", "pdf"}) {
		t.Errorf("default modalities %v, want text left out", got)
	}
}
