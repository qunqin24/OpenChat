package usage

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

func ptr(f float64) *float64 { return &f }

func TestCost(t *testing.T) {
	u := &llm.Usage{InputTokens: 1000, OutputTokens: 500, ReasoningTokens: 200, CacheReadTokens: 4000, CacheWriteTokens: 100}

	cases := []struct {
		name   string
		usage  *llm.Usage
		prices Prices
		want   *float64
	}{
		// 1000*3 + 500*15 + 4000*0.3 + 100*3.75, per million. Reasoning is
		// inside the 500 and adds nothing.
		{"all prices", u, Prices{ptr(3), ptr(15), ptr(0.3), ptr(3.75)}, ptr((3000 + 7500 + 1200 + 375) / 1e6)},
		// No cache prices: cached tokens are ordinary input, not free.
		{"no cache prices", u, Prices{Input: ptr(3), Output: ptr(15)}, ptr((3000 + 7500 + 12000 + 300) / 1e6)},
		{"no usage reported", nil, Prices{Input: ptr(3), Output: ptr(15)}, nil},
		{"no input price", u, Prices{Output: ptr(15)}, nil},
		{"no output price", u, Prices{Input: ptr(3)}, nil},
		// A price of zero is a price: the model is free, and says so.
		{"free", u, Prices{Input: ptr(0), Output: ptr(0)}, ptr(0)},
	}
	for _, c := range cases {
		got := Cost(c.usage, c.prices)
		switch {
		case got == nil && c.want == nil:
		case got == nil || c.want == nil:
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		case math.Abs(*got-*c.want) > 1e-12:
			t.Errorf("%s: got %v, want %v", c.name, *got, *c.want)
		}
	}
}

func TestDescribeClassifiesAndScrubs(t *testing.T) {
	cases := []struct {
		err      error
		code     string
		contains string
		absent   string
	}{
		{&llm.Error{Kind: llm.ErrorAuth, Code: "invalid_api_key", Message: "Incorrect API key provided: sk-abcdef1234567890"},
			"model_auth", "invalid_api_key", "sk-abcdef1234567890"},
		{&llm.Error{Kind: llm.ErrorRateLimit, Message: "slow down"}, "model_rate_limited", "slow down", ""},
		{&llm.Error{Kind: llm.ErrorContextLength, Message: "too long"}, "context_too_long", "too long", ""},
		{&llm.Error{Kind: llm.ErrorProvider, Message: "boom"}, "model_error", "boom", ""},
		{errors.New("Post https://x.invalid/v1beta?key=AIzaSyDUMMYDUMMYDUMMY1234: eof"), "model_error", "eof", "AIzaSy"},
		{errors.New("Authorization: Bearer abc.def.ghi rejected"), "model_error", "rejected", "abc.def.ghi"},
		// Words that merely look like labels are left alone.
		{errors.New("the token limit was reached"), "model_error", "the token limit was reached", ""},
	}
	for _, c := range cases {
		code, message := describe(c.err)
		if code != c.code {
			t.Errorf("%v: code %q, want %q", c.err, code, c.code)
		}
		if !strings.Contains(message, c.contains) {
			t.Errorf("%v: message %q lacks %q", c.err, message, c.contains)
		}
		if c.absent != "" && strings.Contains(message, c.absent) {
			t.Errorf("%v: message %q leaks %q", c.err, message, c.absent)
		}
	}
}

func TestCleanTruncates(t *testing.T) {
	got := clean(strings.Repeat("错误 ", 500))
	if n := len([]rune(got)); n > maxMessageRunes+1 {
		t.Fatalf("%d runes", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("not marked as cut: %q", got[len(got)-10:])
	}
}
