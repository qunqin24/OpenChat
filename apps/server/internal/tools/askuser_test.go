package tools

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/agent"
)

func TestAskUserChecksWhatIsAsked(t *testing.T) {
	opts := `[{"label":"A"},{"label":"B"}]`
	for name, tc := range map[string]struct {
		args string
		ok   bool
	}{
		"one question":       {`{"questions":[{"question":"Q?","header":"H","options":` + opts + `}]}`, true},
		"no questions":       {`{"questions":[]}`, false},
		"five questions":     {`{"questions":[` + strings.Repeat(`{"question":"Q?","header":"H","options":`+opts+`},`, 4) + `{"question":"Q?","header":"H","options":` + opts + `}]}`, false},
		"one option":         {`{"questions":[{"question":"Q?","header":"H","options":[{"label":"A"}]}]}`, false},
		"five options":       {`{"questions":[{"question":"Q?","header":"H","options":[{"label":"1"},{"label":"2"},{"label":"3"},{"label":"4"},{"label":"5"}]}]}`, false},
		"repeated label":     {`{"questions":[{"question":"Q?","header":"H","options":[{"label":"A"},{"label":"A"}]}]}`, false},
		"empty question":     {`{"questions":[{"question":" ","header":"H","options":` + opts + `}]}`, false},
		"wrong type":         {`{"questions":"which?"}`, false},
		"header is optional": {`{"questions":[{"question":"Q?","options":` + opts + `}]}`, true},
	} {
		err := AskUser{}.Check(json.RawMessage(tc.args))
		var toolErr *agent.ToolError
		if (err == nil) != tc.ok || (err != nil && !errors.As(err, &toolErr)) {
			t.Errorf("%s: Check = %v, want ok=%v with an error for the model", name, err, tc.ok)
		}
	}
}

func TestAskUserResultReadsAsQuestionAndAnswer(t *testing.T) {
	in, err := ParseQuestions(json.RawMessage(`{"questions":[
		{"question":"Where?","header":"Place","options":[{"label":"Beach"},{"label":"City"}]},
		{"question":"Doing?","header":"Plans","multiSelect":true,"options":[{"label":"Food"},{"label":"Hikes"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := in.Result([]Answer{{Selected: []string{"City"}}, {Selected: []string{"Food", "Food", "Hikes"}, Other: "  coffee "}})
	if err != nil {
		t.Fatal(err)
	}
	want := "The user answered your questions.\n\n1. [Place] Where?\n   Chose: City\n\n2. [Plans] Doing?\n   Chose: Food; Hikes\n   Wrote: coffee"
	if out.Content != want {
		t.Fatalf("content:\n%s\nwant:\n%s", out.Content, want)
	}

	for name, answers := range map[string][]Answer{
		"missing answer": {{Selected: []string{"City"}}},
		"unknown label":  {{Selected: []string{"Moon"}}, {Other: "x"}},
		"empty":          {{}, {Other: "x"}},
		"two of one":     {{Selected: []string{"City"}, Other: "also"}, {Other: "x"}},
		"too long":       {{Other: strings.Repeat("x", askMaxOther+1)}, {Other: "x"}},
	} {
		if _, err := in.Result(answers); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("%s: err = %v, want ErrInvalidAnswers", name, err)
		}
	}
	// Free text alone is an answer.
	if _, err := in.Result([]Answer{{Other: "somewhere quiet"}, {Selected: []string{"Hikes"}}}); err != nil {
		t.Errorf("written answer: %v", err)
	}
}
