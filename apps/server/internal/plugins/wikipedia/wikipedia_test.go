package wikipedia

import "testing"

func TestEdition(t *testing.T) {
	for language, want := range map[string][2]string{
		"": {"en", ""}, "zh": {"zh", "zh-cn"}, "zh-TW": {"zh", "zh-tw"}, "zh-hk": {"zh", "zh-hk"}, "ja": {"ja", ""}, "simple": {"simple", ""},
	} {
		wiki, variant, err := edition(language)
		if err != nil || wiki != want[0] || variant != want[1] {
			t.Errorf("%q: %s %s %v", language, wiki, variant, err)
		}
	}
	for _, bad := range []string{"en.evil.com/x", "../", "e"} {
		if _, _, err := edition(bad); err == nil {
			t.Errorf("%q taken as a language", bad)
		}
	}
}
