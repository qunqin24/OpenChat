package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

func newTranscriptionClient(t *testing.T, server *httptest.Server, sendUser bool) *TranscriptionClient {
	t.Helper()
	client, err := NewTranscriptions(Config{Provider: "openai", BaseURL: server.URL + "/v1", APIKey: "sk-secret", SendUser: sendUser})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestTranscribeSendsAMultipartFileAndReadsDuration(t *testing.T) {
	var fields = map[string]string{}
	var filename string
	var audio []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer sk-secret" {
			t.Errorf("request went to %s with %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		for name, values := range r.MultipartForm.Value {
			fields[name] = values[0]
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		filename = header.Filename
		audio, _ = io.ReadAll(file)
		_, _ = w.Write([]byte(`{"text":"hello there","usage":{"type":"duration","seconds":3.5}}`))
	}))
	defer server.Close()

	resp, err := newTranscriptionClient(t, server, true).Transcribe(context.Background(), llm.TranscriptionRequest{
		Model: "gpt-transcribe", Audio: []byte("RIFFdata"), MediaType: "audio/webm;codecs=opus", User: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello there" || resp.Seconds != 3.5 || resp.Usage != nil || resp.ServedModel != "gpt-transcribe" {
		t.Errorf("response = %+v", resp)
	}
	if fields["model"] != "gpt-transcribe" || fields["response_format"] != "json" || fields["user"] != "u1" {
		t.Errorf("fields = %v", fields)
	}
	if filename != "audio.webm" || string(audio) != "RIFFdata" {
		t.Errorf("file = %q %q", filename, audio)
	}
}

func TestTranscribeReadsTokensAndOmitsUserWhenNotSent(t *testing.T) {
	var fields = map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		for name, values := range r.MultipartForm.Value {
			fields[name] = values[0]
		}
		_, _ = w.Write([]byte(`{"text":"hi","usage":{"type":"tokens","input_tokens":40,"output_tokens":6,"total_tokens":46}}`))
	}))
	defer server.Close()

	resp, err := newTranscriptionClient(t, server, false).Transcribe(context.Background(), llm.TranscriptionRequest{
		Model: "gpt-4o-transcribe", Audio: []byte("x"), MediaType: "audio/mp4", User: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["user"]; ok {
		t.Errorf("user was sent: %v", fields)
	}
	if resp.Seconds != 0 || resp.Usage == nil || resp.Usage.InputTokens != 40 || resp.Usage.OutputTokens != 6 {
		t.Errorf("response = %+v", resp)
	}
}

func TestTranscribeNoUsageIsNoUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"hi"}`))
	}))
	defer server.Close()
	resp, err := newTranscriptionClient(t, server, false).Transcribe(context.Background(), llm.TranscriptionRequest{Model: "whisper-1", Audio: []byte("x"), MediaType: "audio/ogg"})
	if err != nil || resp.Text != "hi" || resp.Seconds != 0 || resp.Usage != nil {
		t.Errorf("resp = %+v, err = %v", resp, err)
	}
}

func TestTranscribeMapsErrors(t *testing.T) {
	for status, want := range map[int]llm.ErrorKind{
		http.StatusUnauthorized:    llm.ErrorAuth,
		http.StatusTooManyRequests: llm.ErrorRateLimit,
		http.StatusBadGateway:      llm.ErrorProvider,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"nope sk-secret","code":"x"}}`))
		}))
		_, err := newTranscriptionClient(t, server, false).Transcribe(context.Background(), llm.TranscriptionRequest{Model: "m", Audio: []byte("x"), MediaType: "audio/wav"})
		server.Close()
		var modelErr *llm.Error
		if !errors.As(err, &modelErr) || modelErr.Kind != want {
			t.Errorf("status %d: err = %v, want kind %v", status, err, want)
			continue
		}
		if strings.Contains(err.Error(), "sk-secret") {
			t.Errorf("status %d: the key leaked: %v", status, err)
		}
	}
}

func TestAudioExtension(t *testing.T) {
	for in, want := range map[string]string{
		"audio/webm": ".webm", "audio/webm;codecs=opus": ".webm", "audio/mp4": ".m4a", "audio/x-m4a": ".m4a",
		"audio/ogg; codecs=opus": ".ogg", "audio/wav": ".wav", "audio/x-wav": ".wav", "audio/mpeg": ".mp3", "AUDIO/WEBM": ".webm",
	} {
		if got := audioExtension(in); got != want {
			t.Errorf("audioExtension(%q) = %q, want %q", in, got, want)
		}
	}
}
