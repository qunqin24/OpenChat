package api

import "testing"

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
