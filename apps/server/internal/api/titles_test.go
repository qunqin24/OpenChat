package api

import (
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"期货入门":                        "期货入门",
		"  “期货入门”。 ":                  "期货入门",
		"Title: **Go concurrency**.":  "Go concurrency",
		"<think>hmm</think>\n\n周末去哪玩": "周末去哪玩",
		"标题：「今日新闻」\n多余的一行":            "今日新闻",
		"Futures (basics)":            "Futures (basics)",
		"   ":                         "",
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
	long := cleanTitle("一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十一二三四五")
	if r := []rune(long); len(r) != titleMaxRunes+1 || r[len(r)-1] != '…' {
		t.Errorf("long title not clipped: %q", long)
	}
}

// Every end of a run names its conversation; only one still running does not.
func TestTitleAfterEveryEnd(t *testing.T) {
	for status, want := range map[sqlc.RunStatus]bool{
		sqlc.RunStatusCompleted: true, sqlc.RunStatusAwaitingInput: true,
		sqlc.RunStatusCancelled: true, sqlc.RunStatusFailed: true,
		sqlc.RunStatusRunning: false,
	} {
		if got := titleAfter(status); got != want {
			t.Errorf("titleAfter(%s) = %v, want %v", status, got, want)
		}
	}
	if got := questionTitle("\n  请帮我写一封辞职信。\n内容要礼貌"); got != "请帮我写一封辞职信" {
		t.Errorf("questionTitle = %q", got)
	}
}
