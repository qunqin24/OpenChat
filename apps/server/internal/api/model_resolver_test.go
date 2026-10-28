package api

import (
	"context"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/catalog"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

type capture struct{ req llm.Request }

func (c *capture) Stream(_ context.Context, req llm.Request, _ func(llm.Event) error) (llm.ModelResponse, error) {
	c.req = req
	return llm.ModelResponse{}, nil
}

func shapedRequest(t *testing.T, options string, anthropic bool, effort llm.ReasoningEffort) llm.Request {
	t.Helper()
	settings := catalog.Resolve(catalog.Defaults{
		Kind:        catalog.KindText,
		Reasoning:   true,
		ToolCall:    true,
		OutputLimit: 128000,
		Profile:     catalog.ParseReasoningOptions([]byte(options)),
	}, catalog.Override{})
	return shaped(t, settings, anthropic, effort)
}

func shaped(t *testing.T, settings catalog.Settings, anthropic bool, effort llm.ReasoningEffort) llm.Request {
	t.Helper()
	inner := &capture{}
	client := &catalogShaped{inner: inner, settings: settings, anthropic: anthropic}
	req := llm.Request{Model: "m", Tools: []llm.Tool{{Name: "web_fetch"}}}
	if effort != "" {
		req.Options.Reasoning = &llm.ReasoningOptions{Effort: effort}
	}
	if _, err := client.Stream(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	return inner.req
}

func TestCatalogShapesTheThinkingRequest(t *testing.T) {
	const effortModel = `[{"type":"effort","values":["low","medium","high","xhigh","max"]}]`
	const budgetModel = `[{"min":1024,"type":"budget_tokens"}]`

	if r := shapedRequest(t, effortModel, true, "xhigh").Options.Reasoning; r == nil || r.Effort != "xhigh" {
		t.Errorf("effort model: %+v, want the effort", r)
	}
	if r := shapedRequest(t, budgetModel, true, "high").Options.Reasoning; r == nil || r.Effort != "" || r.Budget != 16384 {
		t.Errorf("budget model: %+v, want a budget instead of the effort", r)
	}
	// Remembered from a model that had it; this one does not.
	if r := shapedRequest(t, effortModel, true, "minimal").Options.Reasoning; r != nil {
		t.Errorf("unoffered level: %+v, want it dropped", r)
	}

	req := shapedRequest(t, effortModel, true, "")
	if req.Options.MaxTokens == nil || *req.Options.MaxTokens != 32000 {
		t.Errorf("anthropic max_tokens = %v, want the catalogue-based ceiling", req.Options.MaxTokens)
	}
	if req := shapedRequest(t, effortModel, false, ""); req.Options.MaxTokens != nil {
		t.Errorf("non-anthropic max_tokens = %v, want it left alone", *req.Options.MaxTokens)
	}
}

func TestOperatorSettingsShapeTheRequest(t *testing.T) {
	defaults := catalog.Defaults{
		Kind:        catalog.KindText,
		Reasoning:   true,
		ToolCall:    true,
		OutputLimit: 128000,
		Profile:     catalog.ParseReasoningOptions([]byte(`[{"type":"effort","values":["low","medium","high","max"]}]`)),
	}
	noTools, limit := false, int64(2000)
	settings := catalog.Resolve(defaults, catalog.Override{
		ToolCall:         &noTools,
		OutputLimit:      &limit,
		ReasoningEfforts: []llm.ReasoningEffort{"low", "medium"},
	})

	if r := shaped(t, settings, false, "max").Options.Reasoning; r != nil {
		t.Errorf("withheld level: %+v, want it dropped", r)
	}
	if r := shaped(t, settings, false, "medium").Options.Reasoning; r == nil || r.Effort != "medium" {
		t.Errorf("allowed level: %+v, want it sent", r)
	}
	req := shaped(t, settings, false, "")
	if len(req.Tools) != 0 {
		t.Errorf("tools = %v, want none for a model that may not call them", req.Tools)
	}
	if req.Options.MaxTokens == nil || *req.Options.MaxTokens != 2000 {
		t.Errorf("max_tokens = %v, want the operator's limit on every protocol", req.Options.MaxTokens)
	}
}

func TestAnthropicCeilingFollowsTheEffort(t *testing.T) {
	const effortModel = `[{"type":"effort","values":["low","medium","high","xhigh","max"]}]`
	for effort, want := range map[llm.ReasoningEffort]int{"": 32000, "low": 32000, "high": 32000, "xhigh": 64000, "max": 64000} {
		req := shapedRequest(t, effortModel, true, effort)
		if req.Options.MaxTokens == nil || *req.Options.MaxTokens != want {
			t.Errorf("effort %q: max_tokens = %v, want %d", effort, req.Options.MaxTokens, want)
		}
	}

	// What the operator set still wins, whatever the effort.
	limit := int64(20000)
	settings := catalog.Resolve(catalog.Defaults{
		Kind: catalog.KindText, Reasoning: true, ToolCall: true, OutputLimit: 128000,
		Profile: catalog.ParseReasoningOptions([]byte(effortModel)),
	}, catalog.Override{OutputLimit: &limit})
	if req := shaped(t, settings, true, "max"); req.Options.MaxTokens == nil || *req.Options.MaxTokens != 20000 {
		t.Errorf("operator limit: max_tokens = %v, want 20000", req.Options.MaxTokens)
	}

	// A catalogue limit under the ceiling is the ceiling.
	small := catalog.Resolve(catalog.Defaults{
		Kind: catalog.KindText, Reasoning: true, ToolCall: true, OutputLimit: 8192,
		Profile: catalog.ParseReasoningOptions([]byte(effortModel)),
	}, catalog.Override{})
	if req := shaped(t, small, true, "max"); req.Options.MaxTokens == nil || *req.Options.MaxTokens != 8192 {
		t.Errorf("small model: max_tokens = %v, want its own limit", req.Options.MaxTokens)
	}
}

func TestOpus45GetsABudgetNotAnEffort(t *testing.T) {
	// models.dev lists both for Opus 4.5. Its effort is not adaptive thinking:
	// that model rejects it, so the budget the catalogue also lists is used.
	const opus45 = `[{"type":"effort","values":["low","medium","high"]},{"type":"budget_tokens","min":1024}]`
	settings := catalog.Resolve(catalog.Defaults{
		Kind: catalog.KindText, Reasoning: true, ToolCall: true, OutputLimit: 64000,
		Profile: catalog.ParseReasoningOptions([]byte(opus45)),
	}, catalog.Override{})

	for model, wantBudget := range map[string]bool{
		"claude-opus-4-5": true, "claude-opus-4-5-20251101": true, "claude-sonnet-4-5": true,
		"claude-opus-4-6": false, "claude-opus-5-5": false,
	} {
		inner := &capture{}
		client := &catalogShaped{inner: inner, settings: settings, anthropic: true}
		req := llm.Request{Model: model, Options: llm.Options{Reasoning: &llm.ReasoningOptions{Effort: "high"}}}
		if _, err := client.Stream(context.Background(), req, nil); err != nil {
			t.Fatal(err)
		}
		r := inner.req.Options.Reasoning
		switch {
		case r == nil:
			t.Errorf("%s: thinking was dropped", model)
		case wantBudget && (r.Effort != "" || r.Budget != 16384):
			t.Errorf("%s: %+v, want a 16384 budget and no effort", model, r)
		case !wantBudget && (r.Effort != "high" || r.Budget != 0):
			t.Errorf("%s: %+v, want the effort", model, r)
		}
	}

	// Another protocol's Claude (a router's) keeps the effort it understands.
	inner := &capture{}
	client := &catalogShaped{inner: inner, settings: settings, anthropic: false}
	req := llm.Request{Model: "claude-opus-4-5", Options: llm.Options{Reasoning: &llm.ReasoningOptions{Effort: "high"}}}
	if _, err := client.Stream(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	if r := inner.req.Options.Reasoning; r == nil || r.Effort != "high" {
		t.Errorf("non-anthropic wire: %+v, want the effort", r)
	}
}
