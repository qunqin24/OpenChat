package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/catalog"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/providers/gemini"
	"github.com/qunqin24/openchat/apps/server/internal/providers/openai"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
	"github.com/qunqin24/openchat/apps/server/internal/usage"
)

// Image generation (后台 → 图片生成, docs/design/image-generation-plan.md):
// whether models may make pictures, with which image model, and how many. The
// tool is image_tool.go.
const settingImageGeneration = "image_generation"

type imageConfig struct {
	Enabled    bool   `json:"enabled"`
	Model      string `json:"model"`
	MaxPerCall int    `json:"maxPerCall"`
	MaxPerRun  int    `json:"maxPerRun"`
	// Daily is the site's allowance per person per day; 0 is no limit.
	Daily int `json:"daily"`
}

func defaultImageConfig() imageConfig {
	return imageConfig{MaxPerCall: 4, MaxPerRun: 8}
}

func loadImageConfig(ctx context.Context, q *sqlc.Queries) (imageConfig, error) {
	config := defaultImageConfig()
	raw, err := q.GetSetting(ctx, settingImageGeneration)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return config, nil
	case err != nil:
		return config, err
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return defaultImageConfig(), err
	}
	return config, nil
}

func (c imageConfig) validate() error {
	bad := func(field, message string) error {
		return httpx.NewFieldError(field, "invalid_image_settings", message)
	}
	switch {
	case c.MaxPerCall < 1 || c.MaxPerCall > 10:
		return bad("maxPerCall", "pictures per call is 1 to 10")
	case c.MaxPerRun < 1 || c.MaxPerRun > 50:
		return bad("maxPerRun", "pictures per answer is 1 to 50")
	case c.MaxPerRun < c.MaxPerCall:
		return bad("maxPerRun", "pictures per answer cannot be fewer than per call")
	case c.Daily < 0 || c.Daily > 100000:
		return bad("daily", "pictures per day is 0 (no limit) to 100000")
	case c.Enabled && c.Model == "":
		return bad("model", "choose an image model before switching image generation on")
	}
	return nil
}

// imageChannels is where a request for an image model goes, in the order to
// try them: the model must be an image model (kind), offered to who, with
// money left under its cap, and served by a channel - the same rules as
// pickChannel, whose order (servingOrder) it keeps for failing over.
func (s *Server) imageChannels(ctx context.Context, modelID string, who asker) ([]uuid.UUID, error) {
	if modelID == "" {
		return nil, errModelUnavailable
	}
	settings, err := loadModelSettings(ctx, s.q, modelID)
	if err != nil {
		return nil, err
	}
	if settings.Kind != catalog.KindImage || (settings.AdminOnly && !who.staff) {
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

// errNoImages is a channel whose protocol has no image API (Anthropic's).
var errNoImages = errors.New("api: this channel cannot make pictures")

// imageClientFor builds the image adapter for a channel, the way clientFor
// builds the chat one. A picture an OpenAI-shaped provider answers with by
// address on another host is fetched through GuardedClient: the address is a
// third party's.
var imageClientFor = func(channel sqlc.Channel, modelID string) (llm.ImageClient, error) {
	kind := ChannelType(channel.Type)
	wire, ok := wireForModel(kind, channel.ApiFormat, modelID)
	if !ok {
		return nil, fmt.Errorf("api: channel %s has unknown type %q", channel.ID, channel.Type)
	}
	baseURL := endpointFor(kind, channel.BaseUrl)
	if baseURL == "" {
		return nil, agent.ErrNoModel
	}
	provider := string(channel.Type)
	var headers map[string]string
	if gateway := gatewayFor(kind); gateway != nil {
		headers = map[string]string{"User-Agent": gateway.UserAgent}
	}
	switch wire {
	case wireAnthropic:
		return nil, errNoImages
	case wireGemini:
		return gemini.NewImages(gemini.Config{Provider: provider, BaseURL: baseURL, APIKey: channel.ApiKey, Headers: headers})
	default:
		return openai.NewImages(openai.ImageConfig{
			Config:   openai.Config{Provider: provider, BaseURL: baseURL, APIKey: channel.ApiKey, Headers: headers, SendUser: channel.SendUserID},
			Download: tools.GuardedClient(time.Minute),
		})
	}
}

// imageModelPriced says whether pictures of modelID cost something the
// ledger can count: a price per picture, or the catalogue's output price.
func (s *Server) imageModelPriced(ctx context.Context, modelID string) bool {
	if modelID == "" {
		return false
	}
	prices, err := s.q.UsagePricing(ctx, sqlc.UsagePricingParams{ModelID: modelID})
	if err != nil {
		return false
	}
	return prices.ImagePrice.Valid || (prices.InputPrice.Valid && prices.OutputPrice.Valid)
}

// imageGenerationFor says whether models may make pictures in this person's
// runs: on, with a model, and that model offered to them.
func (s *Server) imageGenerationFor(ctx context.Context, user sqlc.User) bool {
	config, err := loadImageConfig(ctx, s.q)
	if err != nil {
		slog.WarnContext(ctx, "read image generation settings", "error", err)
		return false
	}
	if !config.Enabled || config.Model == "" {
		return false
	}
	admin := user.Role == sqlc.UserRoleAdmin
	settings, err := loadModelSettings(ctx, s.q, config.Model)
	if err != nil {
		return false
	}
	return settings.Kind == catalog.KindImage && (!settings.AdminOnly || admin)
}

func (s *Server) toImageSettings(ctx context.Context, c imageConfig) ImageGenerationSettings {
	return ImageGenerationSettings{
		Enabled: c.Enabled, Model: c.Model, MaxPerCall: c.MaxPerCall, MaxPerRun: c.MaxPerRun, Daily: c.Daily,
		ModelPriced: s.imageModelPriced(ctx, c.Model),
	}
}

func (s *Server) GetImageGenerationSettings(w http.ResponseWriter, r *http.Request) {
	config, err := loadImageConfig(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, s.toImageSettings(r.Context(), config))
}

func (s *Server) UpdateImageGenerationSettings(w http.ResponseWriter, r *http.Request) {
	var body UpdateImageGenerationSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	config, err := loadImageConfig(ctx, s.q)
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
	if body.MaxPerCall != nil {
		config.MaxPerCall = *body.MaxPerCall
	}
	if body.MaxPerRun != nil {
		config.MaxPerRun = *body.MaxPerRun
	}
	if body.Daily != nil {
		config.Daily = *body.Daily
	}
	if err := config.validate(); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if config.Model != "" {
		// An image model some channel serves, whether or not it has money
		// left: saving is not making a picture.
		if _, err := s.imageChannels(ctx, config.Model, asker{staff: true, admin: true, ignoreCap: true}); err != nil {
			httpx.Fail(w, r, httpx.NewFieldError("model", "invalid_image_settings", "not an image model any channel serves"))
			return
		}
	}
	value, _ := json.Marshal(config)
	if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingImageGeneration, Value: value}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	slog.InfoContext(ctx, "image generation settings changed", "enabled", config.Enabled, "model", config.Model,
		"actor", auth.MustUser(ctx).Email)
	httpx.JSON(w, http.StatusOK, s.toImageSettings(ctx, config))
}

// TestImageGeneration makes one picture as the tool would, in no
// conversation, and shows it.
func (s *Server) TestImageGeneration(w http.ResponseWriter, r *http.Request) {
	var body ImageGenerationTestRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	user := auth.MustUser(ctx)
	config, err := loadImageConfig(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	started := time.Now()
	fail := func(why string) {
		httpx.JSON(w, http.StatusOK, ImageGenerationTestResult{Ok: false, Error: &why, DurationMs: time.Since(started).Milliseconds()})
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		fail("the prompt is empty")
		return
	}
	made, err := s.makePictures(ctx, pictureJob{
		user: user, modelID: config.Model, who: asker{staff: true, admin: true},
		req: llm.ImageRequest{Prompt: prompt, Count: 1, Aspect: llm.AspectSquare},
	})
	if err != nil {
		fail(pictureFailure(err))
		return
	}
	picture := made.images[0]
	url := "data:" + picture.MediaType + ";base64," + base64.StdEncoding.EncodeToString(picture.Data)
	_, width, height, _ := llm.SniffPicture(picture.Data)
	httpx.JSON(w, http.StatusOK, ImageGenerationTestResult{
		Ok: true, Image: &url, Width: &width, Height: &height, Cost: made.cost,
		DurationMs: time.Since(started).Milliseconds(),
	})
}

// pictureJob is one image request, from whichever caller: the tool or the
// test button.
type pictureJob struct {
	user    sqlc.User
	modelID string
	who     asker
	req     llm.ImageRequest
	// The run and conversation it is for, when it is for one.
	runID          *uuid.UUID
	conversationID *uuid.UUID
}

type madePictures struct {
	images []llm.GeneratedImage
	model  string
	cost   *float64
}

// errPictureRefused is the image service turning the prompt down on its own
// safety grounds, which is the same on every channel.
type errPictureRefused struct{ cause error }

func (e errPictureRefused) Error() string {
	return "the image service refused the prompt: " + e.cause.Error()
}
func (e errPictureRefused) Unwrap() error { return e.cause }

// makePictures asks the image model for pictures, metered (Begin, Reserve,
// Generate, EndImages), trying the next channel when one fails before making
// anything - the run's own failover rule.
func (s *Server) makePictures(ctx context.Context, job pictureJob) (madePictures, error) {
	order, err := s.imageChannels(ctx, job.modelID, job.who)
	if err != nil {
		return madePictures{}, err
	}
	job.req.Model = job.modelID
	job.req.User = s.endUsers.Account(ctx, job.user.ID)
	tokens := llm.PictureTokens(job.modelID, job.req.Aspect)
	var last error
	for _, channelID := range order {
		channel, err := s.q.GetChannel(ctx, channelID)
		if err != nil {
			last = err
			continue
		}
		client, err := imageClientFor(channel, job.modelID)
		if err != nil {
			last = err
			continue
		}
		id := channelID
		meter := s.usage.Begin(usage.Call{
			Kind: usage.KindImage, UserID: job.user.ID, ConversationID: job.conversationID, RunID: job.runID,
			ChannelID: &id, ModelID: job.modelID,
			InputEstimate: llm.Widen(llm.EstimateText(job.req.Prompt) + len(job.req.Images)*1500),
			Images:        job.req.Count, ImageTokens: tokens,
		}, nil)
		if err := meter.Reserve(ctx); err != nil {
			meter.Release()
			if errors.Is(err, usage.ErrCapacity) {
				return madePictures{}, errModelCapacity
			}
			return madePictures{}, err
		}
		callCtx, cancel := context.WithTimeout(ctx, imageRequestTimeout)
		resp, err := client.Generate(callCtx, job.req)
		cancel()
		meter.EndImages(ctx, resp, len(resp.Images), err)
		if len(resp.Images) > 0 {
			return madePictures{images: resp.Images, model: resp.ServedModel, cost: s.pictureCost(ctx, job.modelID, len(resp.Images), resp.Usage)}, nil
		}
		if ctx.Err() != nil {
			return madePictures{}, ctx.Err()
		}
		if refusal(err) {
			return madePictures{}, errPictureRefused{cause: err}
		}
		last = err
	}
	if last == nil {
		last = errModelUnavailable
	}
	return madePictures{}, last
}

// imageRequestTimeout bounds one image request: a high-quality picture can
// take a minute or two, and a request still going after this is not coming.
const imageRequestTimeout = 4 * time.Minute

// refusal says an image request failed on the service's own refusal of what
// was asked, which another channel would refuse too.
func refusal(err error) bool {
	var modelErr *llm.Error
	if !errors.As(err, &modelErr) || modelErr.Kind != llm.ErrorProvider || modelErr.Retryable {
		return false
	}
	code := strings.ToLower(modelErr.Code)
	message := strings.ToLower(modelErr.Message)
	return strings.Contains(code, "moderation") || strings.Contains(code, "safety") || strings.Contains(code, "content_policy") ||
		code == "prohibited_content" || code == "blocklist" || code == "other" ||
		strings.Contains(message, "safety system") || strings.Contains(message, "content policy") ||
		strings.Contains(message, "the prompt was refused") || strings.Contains(message, "made no picture")
}

// pictureCost is what the test page shows a picture cost: by the picture when
// the model has a price per picture, else by tokens when the provider said.
func (s *Server) pictureCost(ctx context.Context, modelID string, images int, used *llm.Usage) *float64 {
	prices, err := s.q.UsagePricing(ctx, sqlc.UsagePricingParams{ModelID: modelID})
	if err != nil {
		return nil
	}
	if each := numberPtr(prices.ImagePrice); each != nil {
		cost := float64(images) * *each
		return &cost
	}
	return usage.Cost(used, usage.Prices{Input: numberPtr(prices.InputPrice), Output: numberPtr(prices.OutputPrice)})
}

// pictureFailure is why pictures could not be made, in words for an
// administrator or a model.
func pictureFailure(err error) string {
	var refused errPictureRefused
	switch {
	case errors.Is(err, errModelCapacity):
		return "the image model has reached its spending limit"
	case errors.Is(err, errModelUnavailable), errors.Is(err, agent.ErrNoModel):
		return "the image model is not available"
	case errors.Is(err, errNoImages):
		return "the image model's channel cannot make pictures"
	case errors.As(err, &refused):
		return "the image service refused this prompt on its own safety grounds"
	case errors.Is(err, context.DeadlineExceeded):
		return "the image service took too long to answer"
	}
	var modelErr *llm.Error
	if errors.As(err, &modelErr) {
		switch modelErr.Kind {
		case llm.ErrorAuth:
			return "the image model's channel rejected its credentials"
		case llm.ErrorRateLimit:
			return "the image service is busy (rate limited); try again in a moment"
		}
		return "the image service failed: " + shortError(err)
	}
	return "the pictures could not be made: " + shortError(err)
}
