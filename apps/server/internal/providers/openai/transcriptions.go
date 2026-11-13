package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// A transcription answers once the whole recording is read, which for a long
// one takes a while; what bounds the call is the caller's context.
const transcriptionHeaderTimeout = 3 * time.Minute

const maxTranscriptionResponseBytes = 8 << 20

// TranscriptionClient speaks the OpenAI-shaped speech-to-text API:
// POST /audio/transcriptions (multipart). OpenAI's whisper-1 and gpt-transcribe
// answer it, and so do most relays and compatible providers.
type TranscriptionClient struct {
	cfg  Config
	base string
	http *http.Client
}

var _ llm.TranscriptionClient = (*TranscriptionClient)(nil)

func NewTranscriptions(cfg Config) (*TranscriptionClient, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	client := cfg.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = transcriptionHeaderTimeout
		client = &http.Client{Transport: transport}
	}
	return &TranscriptionClient{cfg: cfg, base: strings.TrimSuffix(cfg.BaseURL, "/"), http: client}, nil
}

func (c *TranscriptionClient) Transcribe(ctx context.Context, req llm.TranscriptionRequest) (llm.TranscriptionResponse, error) {
	out := llm.TranscriptionResponse{Provider: c.cfg.Provider, ServedModel: req.Model}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "audio"+audioExtension(req.MediaType))
	if err == nil {
		_, err = file.Write(req.Audio)
	}
	if err == nil {
		err = form.WriteField("model", req.Model)
	}
	if err == nil {
		err = form.WriteField("response_format", "json")
	}
	if err == nil && c.cfg.SendUser && req.User != "" {
		err = form.WriteField("user", req.User)
	}
	if err == nil {
		err = form.Close()
	}
	if err != nil {
		return out, llm.Errorf(llm.ErrorProvider, err, "openai transcriptions: encoding the request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/audio/transcriptions", &body)
	if err != nil {
		return out, llm.Errorf(llm.ErrorTransport, err, "openai transcriptions: building the request")
	}
	httpReq.Header.Set("Content-Type", form.FormDataContentType())
	httpReq.Header.Set("Accept", "application/json")
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	for name, value := range c.cfg.Headers {
		httpReq.Header.Set(name, value)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return out, ctxErr
		}
		return out, scrub(llm.Errorf(llm.ErrorTransport, err, "openai transcriptions: calling the endpoint"), c.cfg.APIKey)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, scrub(statusError(resp), c.cfg.APIKey)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTranscriptionResponseBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return out, ctxErr
		}
		return out, llm.Errorf(llm.ErrorTransport, err, "openai transcriptions: reading the response")
	}
	if len(raw) > maxTranscriptionResponseBytes {
		return out, llm.Errorf(llm.ErrorProtocol, nil, "openai transcriptions: the response is too large")
	}
	var payload wireTranscription
	if err := json.Unmarshal(raw, &payload); err != nil {
		return out, llm.Errorf(llm.ErrorProtocol, err, "openai transcriptions: the response is not JSON")
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return out, scrub(&llm.Error{Kind: llm.ErrorProvider, Code: string(payload.Error.Code), Message: payload.Error.Message}, c.cfg.APIKey)
	}
	out.Text = payload.Text
	if payload.Model != "" {
		out.ServedModel = payload.Model
	}
	out.Seconds, out.Usage = payload.Usage.read()
	return out, nil
}

// audioExtension is the file extension a provider reads the format from.
func audioExtension(mediaType string) string {
	base, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		base = strings.ToLower(strings.TrimSpace(strings.SplitN(mediaType, ";", 2)[0]))
	}
	switch base {
	case "audio/webm":
		return ".webm"
	case "audio/mp4", "audio/x-m4a", "audio/m4a":
		return ".m4a"
	case "audio/ogg":
		return ".ogg"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	}
	return ".webm"
}

type wireTranscription struct {
	Text  string                  `json:"text"`
	Model string                  `json:"model"`
	Usage *wireTranscriptionUsage `json:"usage"`
	Error *wireError              `json:"error"`
}

// wireTranscriptionUsage is either `{type: duration, seconds}` (whisper-1,
// gpt-transcribe) or `{type: tokens, input_tokens, output_tokens}`
// (gpt-4o-transcribe).
type wireTranscriptionUsage struct {
	Type         string  `json:"type"`
	Seconds      float64 `json:"seconds"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
}

func (u *wireTranscriptionUsage) read() (float64, *llm.Usage) {
	if u == nil {
		return 0, nil
	}
	switch u.Type {
	case "duration":
		return max(u.Seconds, 0), nil
	case "tokens":
		if u.InputTokens == 0 && u.OutputTokens == 0 {
			return 0, nil
		}
		return 0, &llm.Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
	}
	return 0, nil
}
