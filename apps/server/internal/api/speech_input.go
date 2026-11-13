package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/catalog"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/providers/openai"
	"github.com/qunqin24/openchat/apps/server/internal/usage"
)

// Speech input (后台 → 语音输入, docs/design/speech-input-plan.md): the
// composer's microphone. A recording is turned into text by the
// administrator's transcription model and thrown away; the text goes back to
// the person's input box and is moderated when they send it, like anything
// typed. Nothing is stored.
const settingSpeechInput = "speech_input"

const (
	defaultSpeechSeconds = 300
	minSpeechSeconds     = 10
	maxSpeechSeconds     = 1800
	// speechSecondsSlack is how far past maxSeconds a client's measured
	// length may be before the recording is refused: timers are not exact.
	speechSecondsSlack = 5
	// maxAudioBytes is OpenAI's own limit on an upload.
	maxAudioBytes = 25 << 20
	// transcriptionRequestTimeout bounds one request to a channel.
	transcriptionRequestTimeout = 2 * time.Minute
)

type speechInputConfig struct {
	Enabled    bool   `json:"enabled"`
	Model      string `json:"model"`
	MaxSeconds int    `json:"maxSeconds"`
}

func defaultSpeechInputConfig() speechInputConfig {
	return speechInputConfig{MaxSeconds: defaultSpeechSeconds}
}

func loadSpeechInputConfig(ctx context.Context, q *sqlc.Queries) (speechInputConfig, error) {
	config := defaultSpeechInputConfig()
	raw, err := q.GetSetting(ctx, settingSpeechInput)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return config, nil
	case err != nil:
		return config, err
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return defaultSpeechInputConfig(), err
	}
	return config, nil
}

func (c speechInputConfig) validate() error {
	bad := func(field, message string) error {
		return httpx.NewFieldError(field, "invalid_speech_input_settings", message)
	}
	switch {
	case c.MaxSeconds < minSpeechSeconds || c.MaxSeconds > maxSpeechSeconds:
		return bad("maxSeconds", fmt.Sprintf("the longest recording is %d to %d seconds", minSpeechSeconds, maxSpeechSeconds))
	case c.Enabled && c.Model == "":
		return bad("model", "choose a transcription model before switching speech input on")
	}
	return nil
}

// errNoTranscription is a channel whose protocol has no transcription API
// (Anthropic's, Gemini's): the next channel is tried.
var errNoTranscription = errors.New("api: this channel cannot transcribe")

// transcriptionClientFor builds the speech-to-text adapter for a channel, the
// way imageClientFor builds the image one. Only the OpenAI-shaped wire has
// one.
var transcriptionClientFor = func(channel sqlc.Channel, modelID string) (llm.TranscriptionClient, error) {
	kind := ChannelType(channel.Type)
	wire, ok := wireForModel(kind, channel.ApiFormat, modelID)
	if !ok {
		return nil, fmt.Errorf("api: channel %s has unknown type %q", channel.ID, channel.Type)
	}
	baseURL := endpointFor(kind, channel.BaseUrl)
	if baseURL == "" {
		return nil, fmt.Errorf("api: channel %s has no address", channel.ID)
	}
	var headers map[string]string
	if gateway := gatewayFor(kind); gateway != nil {
		headers = map[string]string{"User-Agent": gateway.UserAgent}
	}
	switch wire {
	case wireAnthropic, wireGemini:
		return nil, errNoTranscription
	default:
		return openai.NewTranscriptions(openai.Config{
			Provider: string(channel.Type), BaseURL: baseURL, APIKey: channel.ApiKey, Headers: headers, SendUser: channel.SendUserID,
		})
	}
}

// transcriptionChannels is where a request for a transcription model goes, in
// the order to try them: the model must be a transcription model, offered to
// who, with money left under its cap, and served by a channel - the same rules
// as imageChannels.
func (s *Server) transcriptionChannels(ctx context.Context, modelID string, who asker) ([]uuid.UUID, error) {
	if modelID == "" {
		return nil, errModelUnavailable
	}
	settings, err := loadModelSettings(ctx, s.q, modelID)
	if err != nil {
		return nil, err
	}
	if settings.Kind != catalog.KindTranscription || (settings.AdminOnly && !who.staff) {
		return nil, errModelUnavailable
	}
	serving, err := s.q.ServingChannels(ctx, modelID)
	if err != nil {
		return nil, err
	}
	if len(serving) == 0 {
		return nil, errModelUnavailable
	}
	if err := s.refusedForCapacity(ctx, modelID, who); err != nil {
		return nil, err
	}
	return servingOrder(serving, rand.Float64), nil
}

// speechModelPriced says whether a recording on modelID costs something the
// ledger can count: a price per minute, or the catalogue's token prices.
func (s *Server) speechModelPriced(ctx context.Context, modelID string) bool {
	if modelID == "" {
		return false
	}
	prices, err := s.q.UsagePricing(ctx, sqlc.UsagePricingParams{ModelID: modelID})
	if err != nil {
		return false
	}
	return prices.AudioPrice.Valid || (prices.InputPrice.Valid && prices.OutputPrice.Valid)
}

// speechInputFor says whether this person may dictate: on, with a model, and
// that model a transcription model offered to them. It returns the settings
// that apply.
func (s *Server) speechInputFor(ctx context.Context, user sqlc.User) (speechInputConfig, bool) {
	config, err := loadSpeechInputConfig(ctx, s.q)
	if err != nil {
		slog.WarnContext(ctx, "read speech input settings", "error", err)
		return config, false
	}
	if !config.Enabled || config.Model == "" {
		return config, false
	}
	settings, err := loadModelSettings(ctx, s.q, config.Model)
	if err != nil {
		return config, false
	}
	admin := user.Role == sqlc.UserRoleAdmin
	return config, settings.Kind == catalog.KindTranscription && (!settings.AdminOnly || admin)
}

func (s *Server) toSpeechInputSettings(ctx context.Context, c speechInputConfig) SpeechInputSettings {
	return SpeechInputSettings{
		Enabled: c.Enabled, Model: c.Model, MaxSeconds: c.MaxSeconds, ModelPriced: s.speechModelPriced(ctx, c.Model),
	}
}

func (s *Server) GetSpeechInputSettings(w http.ResponseWriter, r *http.Request) {
	config, err := loadSpeechInputConfig(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, s.toSpeechInputSettings(r.Context(), config))
}

func (s *Server) UpdateSpeechInputSettings(w http.ResponseWriter, r *http.Request) {
	var body UpdateSpeechInputSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	config, err := loadSpeechInputConfig(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Enabled != nil {
		config.Enabled = *body.Enabled
	}
	if body.Model != nil {
		config.Model = strings.TrimSpace(*body.Model)
	}
	if body.MaxSeconds != nil {
		config.MaxSeconds = *body.MaxSeconds
	}
	if err := config.validate(); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if config.Model != "" {
		// A transcription model some channel serves, whether or not it has
		// money left: saving is not transcribing.
		if _, err := s.transcriptionChannels(ctx, config.Model, asker{staff: true, admin: true, ignoreCap: true}); err != nil {
			httpx.Fail(w, r, httpx.NewFieldError("model", "invalid_speech_input_settings", "not a transcription model any channel serves"))
			return
		}
	}
	value, _ := json.Marshal(config)
	if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingSpeechInput, Value: value}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	slog.InfoContext(ctx, "speech input settings changed", "enabled", config.Enabled, "model", config.Model,
		"actor", auth.MustUser(ctx).Email)
	httpx.JSON(w, http.StatusOK, s.toSpeechInputSettings(ctx, config))
}

var (
	errSpeechInputOff   = httpx.NewError(http.StatusConflict, "speech_input_off", "speech input is not available")
	errUnsupportedAudio = httpx.NewError(http.StatusUnsupportedMediaType, "unsupported_audio",
		"that kind of recording is not accepted")
	errAudioTooLarge     = httpx.NewError(http.StatusRequestEntityTooLarge, "audio_too_large", "the recording is too large")
	errAudioTooLong      = httpx.NewError(http.StatusRequestEntityTooLarge, "audio_too_long", "the recording is too long")
	errEmptyAudio        = httpx.NewError(http.StatusBadRequest, "empty_audio", "the recording is empty")
	errTranscription     = httpx.NewError(http.StatusBadGateway, "transcription_failed", "the recording could not be transcribed")
	errTranscriptionBusy = httpx.NewError(http.StatusTooManyRequests, "model_rate_limited",
		"the transcription service is busy; try again in a moment")
)

// audioTypes are the recordings a browser makes and the service takes.
var audioTypes = map[string]bool{
	"audio/webm": true, "audio/mp4": true, "audio/x-m4a": true, "audio/m4a": true,
	"audio/ogg": true, "audio/wav": true, "audio/x-wav": true, "audio/mpeg": true,
}

// audioType is the media type of a Content-Type header without its
// parameters (;codecs=opus), or false when it is not one the service takes.
func audioType(header string) (string, bool) {
	base, _, err := mime.ParseMediaType(header)
	if err != nil {
		return "", false
	}
	return base, audioTypes[base]
}

// Transcribe turns a recording into text.
func (s *Server) Transcribe(w http.ResponseWriter, r *http.Request, params TranscribeParams) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	config, ok := s.speechInputFor(ctx, user)
	if !ok {
		httpx.Fail(w, r, errSpeechInputOff)
		return
	}
	if _, ok := audioType(r.Header.Get("Content-Type")); !ok {
		httpx.Fail(w, r, errUnsupportedAudio)
		return
	}
	if params.Seconds < 0 {
		httpx.Fail(w, r, httpx.NewFieldError("seconds", "invalid_seconds", "the length is not a number of seconds"))
		return
	}
	if params.Seconds > config.MaxSeconds+speechSecondsSlack {
		httpx.Fail(w, r, errAudioTooLong)
		return
	}
	audio, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAudioBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.Fail(w, r, errAudioTooLarge)
			return
		}
		httpx.Fail(w, r, err)
		return
	}
	if len(audio) == 0 {
		httpx.Fail(w, r, errEmptyAudio)
		return
	}

	// The type with its parameters, as the browser named it: the adapter
	// reads the extension off the base type.
	text, err := s.transcribe(ctx, transcriptionJob{
		user: user, modelID: config.Model, who: askerFrom(ctx),
		audio: audio, mediaType: r.Header.Get("Content-Type"), seconds: float64(params.Seconds),
	})
	switch {
	case err == nil:
		httpx.JSON(w, http.StatusOK, TranscriptionResult{Text: text})
	case ctx.Err() != nil:
		// The person went away; nothing to answer.
	case errors.Is(err, errModelUnavailable):
		httpx.Fail(w, r, errSpeechInputOff)
	case errors.Is(err, errModelCapacity):
		httpx.Fail(w, r, errModelCapacity)
	default:
		slog.WarnContext(ctx, "transcription failed", "model", config.Model, "error", err)
		var modelErr *llm.Error
		if errors.As(err, &modelErr) && modelErr.Kind == llm.ErrorRateLimit {
			httpx.Fail(w, r, errTranscriptionBusy)
			return
		}
		httpx.Fail(w, r, errTranscription)
	}
}

type transcriptionJob struct {
	user      sqlc.User
	modelID   string
	who       asker
	audio     []byte
	mediaType string
	seconds   float64
}

// transcribe asks the transcription model for the text, metered (Begin,
// Reserve, Transcribe, EndTranscription), trying the next channel when one
// fails before returning anything - the run's own failover rule.
func (s *Server) transcribe(ctx context.Context, job transcriptionJob) (string, error) {
	order, err := s.transcriptionChannels(ctx, job.modelID, job.who)
	if err != nil {
		return "", err
	}
	req := llm.TranscriptionRequest{
		Model: job.modelID, Audio: job.audio, MediaType: job.mediaType,
		User: s.endUsers.Account(ctx, job.user.ID),
	}
	var last error
	for _, channelID := range order {
		channel, err := s.q.GetChannel(ctx, channelID)
		if err != nil {
			last = err
			continue
		}
		client, err := transcriptionClientFor(channel, job.modelID)
		if err != nil {
			last = err
			continue
		}
		id := channelID
		meter := s.usage.Begin(usage.Call{
			Kind: usage.KindTranscription, UserID: job.user.ID, ChannelID: &id, ModelID: job.modelID,
			AudioSeconds: job.seconds,
		}, nil)
		if err := meter.Reserve(ctx); err != nil {
			meter.Release()
			if errors.Is(err, usage.ErrCapacity) {
				return "", errModelCapacity
			}
			return "", err
		}
		callCtx, cancel := context.WithTimeout(ctx, transcriptionRequestTimeout)
		resp, err := client.Transcribe(callCtx, req)
		cancel()
		meter.EndTranscription(ctx, resp, err)
		if err == nil {
			return resp.Text, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		last = err
	}
	if last == nil {
		last = errModelUnavailable
	}
	return "", last
}
