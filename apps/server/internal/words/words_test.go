package words

import "testing"

func TestPersonStripsTheComposersLines(t *testing.T) {
	got := Person("<deep-research/>\n<skill name=\"pdf\"/>\n<image-ref id=\"img_0123abcd\"/>\nwhat is in this")
	if got != "what is in this" {
		t.Fatalf("got %q", got)
	}
	if Person("plain") != "plain" {
		t.Fatal("a plain message changed")
	}
}

func TestStripQuotesKeepsOnlyTheRequest(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"answer quote":   {"<quote>\nsome answer\n</quote>\n\nwhy is that?", "why is that?"},
		"document":       {"<document-selection id=\"d1\" title=\"A &amp; B\">\npassage\n\nmore\n</document-selection>\n\nshorten it", "shorten it"},
		"several, mixed": {"<quote>\none\n</quote>\n\n<document-selection id=\"d\" title=\"t\">\ntwo\n</document-selection>\n\nask", "ask"},
		"only a quote":   {"<quote>\none\n</quote>\n\n", ""},
		"no quote":       {"a <quote> in the middle </quote>", "a <quote> in the middle </quote>"},
		"not at start":   {"hello\n<quote>\nx\n</quote>\n\nbye", "hello\n<quote>\nx\n</quote>\n\nbye"},
	} {
		if got := StripQuotes(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestTypedOrder(t *testing.T) {
	in := "<skill name=\"pdf\"/>\n<image-ref id=\"img_0123abcd\"/>\n<quote>\nq\n</quote>\n\nthe request"
	if got := Typed(in); got != "the request" {
		t.Fatalf("got %q", got)
	}
}
