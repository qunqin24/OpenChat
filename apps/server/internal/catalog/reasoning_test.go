package catalog

import (
	"reflect"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

func TestReasoningProfiles(t *testing.T) {
	// Real reasoning_options values from models.dev.
	sonnet := ParseReasoningOptions([]byte(`[{"type":"toggle"},{"type":"effort","values":["low","medium","high","xhigh","max"]}]`))
	haiku45 := ParseReasoningOptions([]byte(`[{"min":1024,"type":"budget_tokens"}]`))
	flash25 := ParseReasoningOptions([]byte(`[{"type":"toggle"},{"max":24576,"min":0,"type":"budget_tokens"}]`))
	gpt51 := ParseReasoningOptions([]byte(`[{"type":"effort","values":["none","low","medium","high","default"]}]`))
	plain := ParseReasoningOptions([]byte(`[]`))

	if got := sonnet.Levels(); !reflect.DeepEqual(got, []llm.ReasoningEffort{"low", "medium", "high", "xhigh", "max"}) {
		t.Errorf("sonnet levels = %v", got)
	}
	if got := sonnet.Options("xhigh"); got == nil || got.Effort != "xhigh" || got.Budget != 0 {
		t.Errorf("sonnet xhigh = %+v, want the effort itself", got)
	}
	if got := sonnet.Options("minimal"); got != nil {
		t.Errorf("sonnet minimal = %+v, want nothing: it is not one of its levels", got)
	}

	if got := haiku45.Options("high"); got == nil || got.Effort != "" || got.Budget != 16384 {
		t.Errorf("haiku 4.5 high = %+v, want a budget and no effort", got)
	}
	if got := haiku45.Options("low"); got == nil || got.Budget != 1024 {
		t.Errorf("haiku 4.5 low = %+v", got)
	}
	if got := flash25.Options("high"); got == nil || got.Budget != 16384 {
		t.Errorf("flash 2.5 high = %+v", got)
	}

	// `default` is models.dev's word for leaving it alone, which is what
	// "automatic" already is; it is not offered as a level of its own.
	if got := gpt51.Levels(); !reflect.DeepEqual(got, []llm.ReasoningEffort{"none", "low", "medium", "high"}) {
		t.Errorf("gpt-5.1 levels = %v", got)
	}

	if plain.Levels() != nil || plain.Options("high") != nil {
		t.Error("a model with no reasoning options was offered a level")
	}
	if ParseReasoningOptions([]byte(`not json`)).Levels() != nil {
		t.Error("unreadable options were offered a level")
	}
}

func TestBudgetOnlyProfile(t *testing.T) {
	// Opus 4.5 as models.dev lists it: effort levels beside a budget.
	opus45 := ParseReasoningOptions([]byte(`[{"type":"effort","values":["low","medium","high"]},{"type":"budget_tokens","min":1024}]`)).BudgetOnly()
	if got := opus45.Options("medium"); got == nil || got.Effort != "" || got.Budget != 4096 {
		t.Errorf("opus 4.5 medium = %+v, want a budget and no effort", got)
	}
	if got := opus45.Levels(); !reflect.DeepEqual(got, []llm.ReasoningEffort{"low", "medium", "high"}) {
		t.Errorf("opus 4.5 levels = %v", got)
	}

	// Effort levels only, no range of its own: the API's floor stands in.
	bare := ParseReasoningOptions([]byte(`[{"type":"effort","values":["low","high"]}]`)).BudgetOnly()
	if got := bare.Options("low"); got == nil || got.Budget != 1024 || got.Effort != "" {
		t.Errorf("bare low = %+v", got)
	}

	// An operator's restriction survives the change of form.
	restricted := ParseReasoningOptions([]byte(`[{"type":"effort","values":["low","medium","high"]},{"type":"budget_tokens","min":1024}]`)).
		Restrict([]llm.ReasoningEffort{"low"}).BudgetOnly()
	if restricted.Options("high") != nil || restricted.Options("low") == nil {
		t.Errorf("restricted profile offers %v", restricted.Levels())
	}
}
