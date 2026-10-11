package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// classifierAnswer is a complete answer of the classifier model: every
// category with its boolean and its score, the named ones true.
func classifierAnswer(flagged ...string) string {
	categories := map[string]bool{}
	scores := map[string]float64{}
	for _, category := range moderationCategories {
		categories[string(category)] = false
		scores[string(category)] = 0.01
	}
	for _, name := range flagged {
		categories[name], scores[name] = true, 0.97
	}
	raw, _ := json.Marshal(map[string]any{"categories": categories, "scores": scores})
	return string(raw)
}

// An answer is only a verdict when it says, for each category that is checked,
// true or false. What it leaves out is not "false".
func TestVerdictFromNeedsEveryCheckedCategory(t *testing.T) {
	yes, no := true, false
	half := 0.5
	cfg := defaultModerationConfig()
	cfg.Categories = []ModerationCategory{ModerationCategoryViolenceGraphic, ModerationCategoryHate}

	tests := []struct {
		name     string
		minScore float64
		flagged  map[string]*bool
		scores   map[string]*float64
		ok       bool
	}{
		{"complete", 0, map[string]*bool{"violence/graphic": &no, "hate": &yes}, nil, true},
		{"complete, others ignored", 0, map[string]*bool{"violence/graphic": &no, "hate": &no, "made-up": &yes, "sexual": nil}, nil, true},
		{"no categories at all", 0, nil, map[string]*float64{"violence/graphic": &half, "hate": &half}, false},
		{"an empty map", 0, map[string]*bool{}, nil, false},
		{"one category missing", 0, map[string]*bool{"violence/graphic": &no}, nil, false},
		{"a null", 0, map[string]*bool{"violence/graphic": &no, "hate": nil}, nil, false},
		{"a checked category named only by something else", 0, map[string]*bool{"violence/graphic": &no, "sexual": &no}, nil, false},
		{"scores wanted, none given", 0.5, map[string]*bool{"violence/graphic": &no, "hate": &no}, nil, false},
		{"scores wanted, one given", 0.5, map[string]*bool{"violence/graphic": &no, "hate": &no}, map[string]*float64{"hate": &half}, false},
		{"scores wanted, out of range", 0.5, map[string]*bool{"violence/graphic": &no, "hate": &no},
			map[string]*float64{"violence/graphic": &half, "hate": func() *float64 { v := 7.0; return &v }()}, false},
		{"scores wanted and given", 0.5, map[string]*bool{"violence/graphic": &no, "hate": &no},
			map[string]*float64{"violence/graphic": &half, "hate": &half}, true},
	}
	for _, tt := range tests {
		cfg.MinScore = tt.minScore
		_, err := cfg.verdictFrom(tt.flagged, tt.scores)
		if (err == nil) != tt.ok {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
}

func TestParseModelVerdictRefusesIncompleteAnswers(t *testing.T) {
	cfg := defaultModerationConfig() // six categories checked
	if _, err := parseModelVerdict(cfg, classifierAnswer("violence/graphic")); err != nil {
		t.Fatalf("a complete answer: %v", err)
	}
	for name, answer := range map[string]string{
		"empty categories":       `{"categories":{}}`,
		"empty categories, more": `{"categories":{},"scores":{"violence/graphic":0.99}}`,
		"scores only":            `{"scores":{"violence/graphic":0.99}}`,
		"one category":           `{"categories":{"violence/graphic":false},"scores":{"violence/graphic":0.01}}`,
		"words for booleans":     strings.ReplaceAll(classifierAnswer(), "false", `"no"`),
		"nulls":                  strings.ReplaceAll(classifierAnswer(), "false", "null"),
	} {
		if _, err := parseModelVerdict(cfg, answer); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A classifier that answers {"categories":{}} has not said the message is safe:
// the check is unavailable, and goes the way of a failing service.
func TestModelModeIncompleteAnswerIsUnavailable(t *testing.T) {
	for _, answer := range []string{
		`{"categories":{}}`,
		`{"categories":{},"scores":{}}`,
		`{"scores":{"violence/graphic":0.99}}`,
		`{"categories":{"violence/graphic":false}}`,
	} {
		h := newAskHarness(t, openAIWire, func(int) string { return openAIWire.text(answer) })
		resetModeration(t, h)
		if rec := h.patchModeration(fmt.Sprintf(`{"mode":"model","model":%q,"onError":"block"}`, h.model)); rec.Code != http.StatusOK {
			t.Fatalf("configure: %d %s", rec.Code, rec.Body)
		}
		messages := h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, h.conv.ID)
		var body blockedBody
		rec := h.post("hello")
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != http.StatusServiceUnavailable || body.Code != "moderation_unavailable" {
			t.Fatalf("%s, on_error block: %d %s", answer, rec.Code, rec.Body)
		}
		if h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, h.conv.ID) != messages {
			t.Error("a held-back message left rows")
		}
		if rec := h.patchModeration(`{"onError":"allow"}`); rec.Code != http.StatusOK {
			t.Fatal(rec.Body)
		}
		if rec := h.post("hello"); rec.Code != http.StatusAccepted {
			t.Fatalf("%s, on_error allow: %d %s", answer, rec.Code, rec.Body)
		}
	}
}

// The same for OpenAI's endpoint: a result with scores and no booleans (which
// passed when the minimum score was 0), or with a category missing, is not a
// result.
func TestOpenAIModeIncompleteResultIsUnavailable(t *testing.T) {
	h, _ := newModerationHarness(t, `,"onError":"block"`)
	var answer atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(answer.Load().(string)))
	}))
	t.Cleanup(srv.Close)
	if rec := h.patchModeration(fmt.Sprintf(`{"openaiBaseUrl":%q}`, srv.URL)); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}

	scores := map[string]float64{}
	categories := map[string]bool{}
	for _, c := range moderationCategories {
		scores[string(c)], categories[string(c)] = 0.99, false
	}
	marshal := func(result map[string]any) string {
		raw, _ := json.Marshal(map[string]any{"results": []any{result}})
		return string(raw)
	}
	missing := map[string]bool{}
	for name, on := range categories {
		if name != "violence/graphic" {
			missing[name] = on
		}
	}
	for name, body := range map[string]string{
		"scores without booleans": marshal(map[string]any{"flagged": true, "category_scores": scores}),
		"empty categories":        marshal(map[string]any{"categories": map[string]bool{}, "category_scores": scores}),
		"a checked one missing":   marshal(map[string]any{"categories": missing, "category_scores": scores}),
		"a null boolean":          strings.Replace(marshal(map[string]any{"categories": categories}), `"violence/graphic":false`, `"violence/graphic":null`, 1),
		"nothing":                 marshal(map[string]any{}),
	} {
		answer.Store(body)
		if rec := h.patchModeration(`{"onError":"block"}`); rec.Code != http.StatusOK {
			t.Fatal(rec.Body)
		}
		rec := h.post("hello")
		var parsed blockedBody
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
		if rec.Code != http.StatusServiceUnavailable || parsed.Code != "moderation_unavailable" {
			t.Fatalf("%s, on_error block: %d %s", name, rec.Code, rec.Body)
		}
		if rec := h.patchModeration(`{"onError":"allow"}`); rec.Code != http.StatusOK {
			t.Fatal(rec.Body)
		}
		rec = h.post("hello")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s, on_error allow: %d %s", name, rec.Code, rec.Body)
		}
		var sent CreateMessageResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &sent)
		h.settle(sent.Run.Id)
	}

	// A complete result still decides.
	answer.Store(marshal(map[string]any{"categories": categories, "category_scores": scores}))
	rec := h.post("hello")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("a complete result of nothing: %d %s", rec.Code, rec.Body)
	}
}
