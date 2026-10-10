package api

import (
	"context"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/catalog"
	"github.com/qunqin/openchat/apps/server/internal/llm"
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
	if req.Options.MaxTokens == nil || *req.Options.MaxTokens != anthropicMaxTokens {
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
