package api

import (
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/browser"
)

func TestAskReason(t *testing.T) {
	post := browser.Element{Tag: "button", FormMethod: "post", InForm: true}
	for _, c := range []struct {
		name    string
		el      browser.Element
		address string
		action  string
		model   bool
		want    string
	}{
		{"a link", browser.Element{Tag: "a", Text: "Next page"}, "https://shop.example/", "click", false, ""},
		{"marked by the model", browser.Element{Tag: "a"}, "https://x.example/", "click", true, "model"},
		{"a posting form", post, "https://x.example/", "click", false, "submit"},
		{"a search form", browser.Element{Tag: "button", FormMethod: "get", InForm: true}, "https://x.example/", "click", false, ""},
		{"a posting search", browser.Element{Tag: "button", FormMethod: "post", Search: true}, "https://x.example/", "click", false, ""},
		{"a plain button in a form", browser.Element{Tag: "button", Type: "button", FormMethod: "post"}, "https://x.example/", "click", false, ""},
		{"a sign-in form", browser.Element{Tag: "button", FormMethod: "post", Password: true}, "https://x.example/", "click", false, "password_form"},
		{"a card form", browser.Element{Tag: "input", Type: "submit", FormMethod: "get", Payment: true}, "https://x.example/", "click", false, "payment"},
		{"on a checkout page", browser.Element{Tag: "div", Text: "Continue"}, "https://x.example/checkout/step2", "click", false, "payment"},
		{"Enter in a posting form", browser.Element{Tag: "textarea", FormMethod: "post"}, "https://x.example/", "press", false, "submit"},
		{"Enter outside a form", browser.Element{Tag: "input"}, "https://x.example/", "press", false, ""},
		{"a button that buys", browser.Element{Tag: "div", Text: "Buy now"}, "https://x.example/", "click", false, "words"},
		{"a button that deletes", browser.Element{Tag: "span", Text: "删除"}, "https://x.example/", "click", false, "words"},
		{"a word inside another", browser.Element{Tag: "a", Text: "Postcards"}, "https://x.example/", "click", false, ""},
	} {
		if got := askReason(c.el, c.address, c.action, c.model); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBrowserAddress(t *testing.T) {
	for raw, want := range map[string]string{
		"example.com":          "https://example.com",
		"http://example.com/a": "http://example.com/a",
	} {
		if got, err := browserAddress(raw); err != nil || got != want {
			t.Errorf("%s: %q %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "file:///etc/passwd", "javascript:alert(1)", "chrome://settings"} {
		if _, err := browserAddress(raw); err == nil {
			t.Errorf("%s accepted", raw)
		}
	}
}
