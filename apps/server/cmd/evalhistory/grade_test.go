package main

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

func TestGrade(t *testing.T) {
	e := expect{Groups: [][]string{{"12月18日", "12月18号"}, {"西湖国宾馆"}}, Tool: toolMust}

	if ok, why := grade(e, outcome{Status: "completed", Answer: "最后定在 **12 月 18 号**，在`西湖国宾馆`。", Calls: 2}); !ok {
		t.Errorf("a right answer failed: %v", why)
	}
	ok, why := grade(e, outcome{Status: "completed", Answer: "定在12月18日。", Calls: 0})
	if ok || len(why) != 2 || !strings.HasPrefix(why[0], "missing 西湖国宾馆") || why[1] != "did not use chat_history" {
		t.Errorf("%v %v", ok, why)
	}
	if ok, why := grade(e, outcome{Status: "failed"}); ok || why[0] != "run failed" {
		t.Errorf("%v %v", ok, why)
	}

	never := expect{Tool: toolNever}
	if ok, _ := grade(never, outcome{Status: "completed", Answer: "光合作用是……", Calls: 1}); ok {
		t.Error("a call where none belongs passed")
	}
	if ok, why := grade(never, outcome{Status: "completed", Answer: " "}); ok || why[0] != "empty answer" {
		t.Errorf("%v %v", ok, why)
	}

	exact := expect{Groups: [][]string{{"Maple-4827-Otter"}}, CaseSensitive: true}
	if ok, _ := grade(exact, outcome{Status: "completed", Answer: "密码是 maple-4827-otter"}); ok {
		t.Error("a password in the wrong case passed")
	}
	if ok, _ := grade(exact, outcome{Status: "completed", Answer: "密码是 `Maple-4827-Otter`。"}); !ok {
		t.Error("the password given back exactly failed")
	}
}

// Every case plants what it asks for, and what it plants is beyond the reach
// of the list of recent conversations exactly when the case says the tool is
// needed.
func TestCasesPlantWhatTheyAsk(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	for _, c := range allCases {
		for trial := range 20 {
			s := c.build(rand.New(rand.NewPCG(7, uint64(trial))), now)
			var said strings.Builder
			newest := time.Time{}
			for _, conv := range s.Convs {
				for _, turn := range conv.Turns {
					said.WriteString(turn.Content + "\n")
				}
				newest = later(newest, conv.At)
			}
			for _, group := range s.Expect.Groups {
				if !strings.Contains(said.String(), group[0]) {
					t.Errorf("%s: %q is asked for and planted nowhere", c.Name, group[0])
				}
			}
			for _, conv := range s.Convs {
				if conv.At.After(now) {
					t.Errorf("%s: a conversation in the future", c.Name)
				}
			}
			// The fillers are what pushes the planted conversations out of
			// the list (40 at most): a case that needs the tool has all of
			// its own behind more than that; pickup's is newer than them all.
			newer := 0
			for _, f := range fillers(now) {
				if len(s.Convs) > 0 && f.At.After(newest) {
					newer++
				}
			}
			if s.Expect.Tool == toolMust && newer <= 40 {
				t.Errorf("%s: only %d conversations newer than what it plants", c.Name, newer)
			}
			if c.Name == "pickup" && newer != 0 {
				t.Errorf("pickup: %d conversations newer than yesterday's", newer)
			}
		}
	}
	if fs := fillers(now); len(fs) <= 40 {
		t.Errorf("%d fillers; the list holds 40", len(fs))
	}
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
