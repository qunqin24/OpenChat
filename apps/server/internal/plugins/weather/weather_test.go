package weather

import "testing"

func TestWords(t *testing.T) {
	for name, want := range map[string]string{"上海": "zh", "東京": "zh", "とうきょう": "ja", "서울": "ko", "Москва": "ru", "Paris": "en"} {
		if got := scriptLanguage(name); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	if describe(0) != "clear sky" || describe(95) != "thunderstorm" || describe(42) != "weather code 42" {
		t.Error("weather codes")
	}
	for degrees, want := range map[float64]string{0: "N", 44: "NE", 180: "S", 350: "N", 270: "W"} {
		if got := compass(degrees); got != want {
			t.Errorf("%v: %s, want %s", degrees, got, want)
		}
	}
}
