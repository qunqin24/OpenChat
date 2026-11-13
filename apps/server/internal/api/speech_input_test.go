package api

import "testing"

func TestAudioType(t *testing.T) {
	for header, want := range map[string]bool{
		"audio/webm": true, "audio/webm;codecs=opus": true, "audio/webm; codecs=opus": true, "audio/mp4": true,
		"audio/x-m4a": true, "audio/m4a": true, "audio/ogg;codecs=opus": true, "audio/wav": true, "audio/x-wav": true,
		"audio/mpeg": true, "AUDIO/WEBM": true,
		"": false, "video/webm": false, "audio/flac": false, "application/octet-stream": false, "audio": false,
	} {
		if _, got := audioType(header); got != want {
			t.Errorf("audioType(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestSpeechInputConfigValidate(t *testing.T) {
	for name, c := range map[string]struct {
		config speechInputConfig
		ok     bool
	}{
		"default":        {defaultSpeechInputConfig(), true},
		"shortest":       {speechInputConfig{MaxSeconds: 10}, true},
		"longest":        {speechInputConfig{MaxSeconds: 1800, Enabled: true, Model: "m"}, true},
		"too short":      {speechInputConfig{MaxSeconds: 9}, false},
		"too long":       {speechInputConfig{MaxSeconds: 1801}, false},
		"on, no model":   {speechInputConfig{MaxSeconds: 300, Enabled: true}, false},
		"off with model": {speechInputConfig{MaxSeconds: 300, Model: "m"}, true},
	} {
		if err := c.config.validate(); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAudioPriceParam(t *testing.T) {
	for price, ok := range map[float64]bool{0: true, 0.0045: true, 0.006: true, 100: true, -0.1: false, 100.5: false, 0.0000001: false} {
		p := price
		if _, err := audioPriceParam(&p); (err == nil) != ok {
			t.Errorf("audioPriceParam(%v) = %v", price, err)
		}
	}
	if p, err := audioPriceParam(nil); err != nil || p.Valid {
		t.Errorf("nil price = %+v, %v", p, err)
	}
}
