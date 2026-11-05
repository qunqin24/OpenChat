package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// An image model answers when the picture is made, which takes far longer
// than a chat stream takes to start. The caller's context bounds the call.
const imageHeaderTimeout = 5 * time.Minute

const maxImageResponseBytes = 96 << 20

// ImageClient makes pictures with Gemini's own image models (the
// "Nano Banana" family): generateContent asked for image output. One request
// makes one picture, so several are several requests, one after another.
type ImageClient struct {
	cfg  Config
	base *Client
	http *http.Client
}

var _ llm.ImageClient = (*ImageClient)(nil)

func NewImages(cfg Config) (*ImageClient, error) {
	if cfg.HTTPClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = imageHeaderTimeout
		cfg.HTTPClient = &http.Client{Transport: transport}
	}
	base, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return &ImageClient{cfg: cfg, base: base, http: cfg.HTTPClient}, nil
}

func (c *ImageClient) Generate(ctx context.Context, req llm.ImageRequest) (llm.ImageResponse, error) {
	out := llm.ImageResponse{Provider: c.cfg.Provider, ServedModel: req.Model}
	var total llm.Usage
	reported := false
	for i := 0; i < max(req.Count, 1); i++ {
		picture, usage, served, err := c.one(ctx, req)
		if usage != nil {
			reported = true
			total.InputTokens += usage.InputTokens
			total.OutputTokens += usage.OutputTokens
			total.ReasoningTokens += usage.ReasoningTokens
			total.CacheReadTokens += usage.CacheReadTokens
		}
		if served != "" {
			out.ServedModel = served
		}
		if err != nil {
			// What was already made is kept and billed; the rest is not tried.
			if reported {
				out.Usage = &total
			}
			if len(out.Images) > 0 {
				return out, nil
			}
			return out, err
		}
		out.Images = append(out.Images, picture)
	}
	if reported {
		out.Usage = &total
	}
	return out, nil
}

func (c *ImageClient) one(ctx context.Context, req llm.ImageRequest) (llm.GeneratedImage, *llm.Usage, string, error) {
	// A picture comes whole, so the plain method rather than the stream.
	endpoint, err := c.base.endpointFor(req.Model, ":generateContent", false)
	if err != nil {
		return llm.GeneratedImage{}, nil, "", err
	}

	parts := []map[string]any{{"text": req.Prompt}}
	for _, picture := range req.Images {
		parts = append(parts, map[string]any{"inlineData": wireBlob{MIMEType: picture.MediaType, Data: base64.StdEncoding.EncodeToString(picture.Data)}})
	}
	config := map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}
	if ratio := map[llm.Aspect]string{llm.AspectSquare: "1:1", llm.AspectLandscape: "3:2", llm.AspectPortrait: "2:3"}[req.Aspect]; ratio != "" {
		config["imageConfig"] = map[string]any{"aspectRatio": ratio}
	}
	body := map[string]any{
		"contents":         []map[string]any{{"role": "user", "parts": parts}},
		"generationConfig": config,
	}
	for name, value := range req.Extra {
		if value == nil {
			delete(body, name)
			continue
		}
		body[name] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return llm.GeneratedImage{}, nil, "", llm.Errorf(llm.ErrorProvider, err, "gemini images: encoding the request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return llm.GeneratedImage{}, nil, "", llm.Errorf(llm.ErrorTransport, sanitize(err), "gemini images: building the request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for name, value := range c.cfg.Headers {
		httpReq.Header.Set(name, value)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return llm.GeneratedImage{}, nil, "", ctxErr
		}
		return llm.GeneratedImage{}, nil, "", llm.Errorf(llm.ErrorTransport, sanitize(err), "gemini images: calling the endpoint")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return llm.GeneratedImage{}, nil, "", statusError(resp)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageResponseBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return llm.GeneratedImage{}, nil, "", ctxErr
		}
		return llm.GeneratedImage{}, nil, "", llm.Errorf(llm.ErrorTransport, sanitize(err), "gemini images: reading the response")
	}
	if len(raw) > maxImageResponseBytes {
		return llm.GeneratedImage{}, nil, "", llm.Errorf(llm.ErrorProtocol, nil, "gemini images: the response is too large")
	}
	var payload wireImageResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return llm.GeneratedImage{}, nil, "", llm.Errorf(llm.ErrorProtocol, err, "gemini images: the response is not JSON")
	}
	var usage *llm.Usage
	if payload.UsageMetadata != nil {
		usage = convertUsage(payload.UsageMetadata)
	}
	if payload.PromptFeedback != nil && payload.PromptFeedback.BlockReason != "" {
		return llm.GeneratedImage{}, usage, payload.ModelVersion,
			&llm.Error{Kind: llm.ErrorProvider, Code: payload.PromptFeedback.BlockReason, Message: "gemini images: the prompt was refused"}
	}
	for _, cand := range payload.Candidates {
		for _, part := range cand.Content.Parts {
			if part.InlineData == nil || part.InlineData.Data == "" {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
			if err != nil {
				return llm.GeneratedImage{}, usage, payload.ModelVersion, llm.Errorf(llm.ErrorProtocol, err, "gemini images: a picture is not base64")
			}
			mediaType, _, _, ok := llm.SniffPicture(data)
			if !ok {
				return llm.GeneratedImage{}, usage, payload.ModelVersion,
					llm.Errorf(llm.ErrorProtocol, nil, "gemini images: the model answered with something that is not a PNG, JPEG or WebP picture")
			}
			return llm.GeneratedImage{MediaType: mediaType, Data: data}, usage, payload.ModelVersion, nil
		}
	}
	// No picture: the model answered in words (it declines this way), or a
	// safety filter stopped the candidate.
	reason := ""
	if len(payload.Candidates) > 0 {
		reason = payload.Candidates[0].FinishReason
	}
	return llm.GeneratedImage{}, usage, payload.ModelVersion,
		&llm.Error{Kind: llm.ErrorProvider, Code: reason, Message: "gemini images: the model made no picture"}
}

type wireImageResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text       *string   `json:"text"`
				InlineData *wireBlob `json:"inlineData"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata  *usageMetadata  `json:"usageMetadata"`
	ModelVersion   string          `json:"modelVersion"`
	PromptFeedback *promptFeedback `json:"promptFeedback"`
}
