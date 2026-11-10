package main

import (
	"strings"
	"unicode"
)

// How an answer is judged: by what it must say, never by a model. Every case
// plants facts that cannot be guessed (a random code, a surname, a date), so
// finding them in the answer is the answer being right, and the grade costs
// nothing and means the same on every run.

// toolRule is what a case expects of the chat_history tool.
type toolRule int

const (
	// toolEither: the case does not care (the list may be enough, or not).
	toolEither toolRule = iota
	// toolMust: what is asked is not in the prompt, only in the history.
	toolMust
	// toolNever: the question has nothing to do with earlier conversations.
	toolNever
)

func (r toolRule) String() string {
	switch r {
	case toolMust:
		return "must"
	case toolNever:
		return "never"
	}
	return "either"
}

// expect is what a trial's answer has to contain: every group, each by any of
// its spellings.
type expect struct {
	Groups [][]string
	// CaseSensitive is for what has to be given back exactly, such as a
	// password.
	CaseSensitive bool
	Tool          toolRule
}

// outcome is a trial's run as the grade sees it.
type outcome struct {
	Status string // the run's: completed, failed, ...
	Answer string
	Calls  int // chat_history calls
}

// grade says whether a trial passed, and if not, why (one reason per miss).
func grade(e expect, o outcome) (bool, []string) {
	var why []string
	if o.Status != "completed" {
		why = append(why, "run "+o.Status)
	}
	answer := normalize(o.Answer, e.CaseSensitive)
	for _, group := range e.Groups {
		found := false
		for _, spelling := range group {
			if strings.Contains(answer, normalize(spelling, e.CaseSensitive)) {
				found = true
				break
			}
		}
		if !found {
			why = append(why, "missing "+strings.Join(group, " / "))
		}
	}
	switch {
	case e.Tool == toolMust && o.Calls == 0:
		why = append(why, "did not use chat_history")
	case e.Tool == toolNever && o.Calls > 0:
		why = append(why, "used chat_history")
	}
	if strings.TrimSpace(o.Answer) == "" && o.Status == "completed" {
		why = append(why, "empty answer")
	}
	return len(why) == 0, why
}

// normalize drops what a model may or may not put around what it says: the
// spacing between Chinese and digits ("12 月" for "12月"), Markdown's emphasis
// and code marks ("姓**闫**"), and the case unless it matters.
func normalize(text string, caseSensitive bool) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsSpace(r) || r == '*' || r == '`' {
			continue
		}
		if !caseSensitive {
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
