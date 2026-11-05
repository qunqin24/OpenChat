package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// The images API answers only once the pictures are made, which for a
// high-quality gpt-image is often a minute or more: the chat adapters' header
// timeout would cut it off. What bounds the call is the caller's context.
const imageHeaderTimeout = 5 * time.Minute

// Bounds on what an images response may make us read: the JSON (pictures
// arrive base64 inside it) and each picture fetched by address.
const (
	maxImageResponseBytes = 96 << 20
	maxImageDownloadBytes = 20 << 20
)

// ImageConfig is Config plus how to fetch a picture a provider answers with
// by address on a host that is not the channel's own. Download must be a
// client that only reaches public addresses (tools.GuardedClient): the
// address comes from a third party's response.
type ImageConfig struct {
	Config
	Download *http.Client
}

// ImageClient speaks the OpenAI-shaped images API: POST /images/generations
// (JSON) and /images/edits (multipart). OpenAI's gpt-image, xAI, Volcengine
// Ark, Zhipu, SiliconFlow and most relays answer it, each with small
// differences the decoder tolerates: base64 or an address, `data` or `images`.
type ImageClient struct {
	cfg      ImageConfig
	base     string
	http     *http.Client
	download *http.Client
}

var _ llm.ImageClient = (*ImageClient)(nil)

func NewImages(cfg ImageConfig) (*ImageClient, error) {
	if err := validateConfig(cfg.Config); err != nil {
		return nil, err
	}
	client := cfg.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = imageHeaderTimeout
		client = &http.Client{Transport: transport}
	}
	download := cfg.Download
	if download == nil {
		download = client
	}
	return &ImageClient{cfg: cfg, base: strings.TrimSuffix(cfg.BaseURL, "/"), http: client, download: download}, nil
}

func (c *ImageClient) Generate(ctx context.Context, req llm.ImageRequest) (llm.ImageResponse, error) {
	out := llm.ImageResponse{Provider: c.cfg.Provider, ServedModel: req.Model}
	fields := imageFields(req, c.cfg.SendUser)

	var httpReq *http.Request
	var err error
	if len(req.Images) == 0 {
		body, merr := json.Marshal(fields)
		if merr != nil {
			return out, llm.Errorf(llm.ErrorProvider, merr, "openai images: encoding the request")
		}
		httpReq, err = http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/images/generations", bytes.NewReader(body))
		if err == nil {
			httpReq.Header.Set("Content-Type", "application/json")
		}
	} else {
		body, contentType, merr := editBody(fields, req)
		if merr != nil {
			return out, llm.Errorf(llm.ErrorProvider, merr, "openai images: encoding the request")
		}
		httpReq, err = http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/images/edits", body)
		if err == nil {
			httpReq.Header.Set("Content-Type", contentType)
		}
	}
	if err != nil {
		return out, llm.Errorf(llm.ErrorTransport, err, "openai images: building the request")
	}
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
		return out, llm.Errorf(llm.ErrorTransport, err, "openai images: calling the endpoint")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, scrub(statusError(resp), c.cfg.APIKey)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageResponseBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return out, ctxErr
		}
		return out, llm.Errorf(llm.ErrorTransport, err, "openai images: reading the response")
	}
	if len(raw) > maxImageResponseBytes {
		return out, llm.Errorf(llm.ErrorProtocol, nil, "openai images: the response is too large")
	}
	var payload wireImagesResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return out, llm.Errorf(llm.ErrorProtocol, err, "openai images: the response is not JSON")
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return out, scrub(&llm.Error{Kind: llm.ErrorProvider, Code: string(payload.Error.Code), Message: payload.Error.Message}, c.cfg.APIKey)
	}
	out.Usage = payload.Usage.toUsage()
	if payload.Model != "" {
		out.ServedModel = payload.Model
	}

	items := append(payload.Data, payload.Images...)
	for _, item := range items {
		if out.Revised == "" && item.RevisedPrompt != "" {
			out.Revised = item.RevisedPrompt
		}
		data, err := c.picture(ctx, item)
		if err != nil {
			return out, err
		}
		mediaType, _, _, ok := llm.SniffPicture(data)
		if !ok {
			return out, llm.Errorf(llm.ErrorProtocol, nil, "openai images: the provider answered with something that is not a PNG, JPEG or WebP picture")
		}
		out.Images = append(out.Images, llm.GeneratedImage{MediaType: mediaType, Data: data})
	}
	if len(out.Images) == 0 {
		return out, llm.Errorf(llm.ErrorProtocol, nil, "openai images: the response has no pictures")
	}
	return out, nil
}

// picture is one item's bytes, inline or fetched from where it points.
func (c *ImageClient) picture(ctx context.Context, item wireImageItem) ([]byte, error) {
	if item.B64JSON != "" {
		data, err := base64.StdEncoding.DecodeString(item.B64JSON)
		if err != nil {
			return nil, llm.Errorf(llm.ErrorProtocol, err, "openai images: a picture is not base64")
		}
		return data, nil
	}
	if item.URL == "" {
		return nil, llm.Errorf(llm.ErrorProtocol, nil, "openai images: a picture has neither data nor an address")
	}
	address, err := url.Parse(item.URL)
	if err != nil || (address.Scheme != "https" && address.Scheme != "http") {
		return nil, llm.Errorf(llm.ErrorProtocol, err, "openai images: a picture's address is not a web address")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return nil, llm.Errorf(llm.ErrorTransport, err, "openai images: building the download")
	}
	client := c.download
	// The channel's own host may need the channel's key, and may be on an
	// address only the operator can reach: it is fetched as the request was.
	if base, err := url.Parse(c.base); err == nil && strings.EqualFold(base.Host, address.Host) {
		client = c.http
		if c.cfg.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// Never the address in the error: it may be a signed link.
		return nil, llm.Errorf(llm.ErrorTransport, nil, "openai images: downloading a picture failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "openai images: downloading a picture answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageDownloadBytes+1))
	if err != nil {
		return nil, llm.Errorf(llm.ErrorTransport, nil, "openai images: reading a picture failed")
	}
	if len(data) > maxImageDownloadBytes {
		return nil, llm.Errorf(llm.ErrorProtocol, nil, "openai images: a picture is too large")
	}
	return data, nil
}

// imageFields is the request's parameters, by name. Extra goes last, so an
// operator's per-model setting wins; a null in it removes a field we would
// otherwise send (a relay that rejects response_format, say).
func imageFields(req llm.ImageRequest, sendUser bool) map[string]any {
	fields := map[string]any{"model": req.Model, "prompt": req.Prompt, "n": max(req.Count, 1)}
	id := strings.ToLower(req.Model)
	exact := req.Width > 0 && req.Height > 0
	// The shape asked for: the exact size's, else the aspect.
	shape := req.Aspect
	if exact {
		shape = llm.NearestAspect(req.Width, req.Height, llm.Aspects)
	}
	switch {
	case strings.Contains(id, "gpt-image") || strings.Contains(id, "chatgpt-image"):
		// Always base64; the parameter itself is refused. An exact size goes
		// as asked - the newer models take any within their limits, and one
		// that does not says so, for the model to try a shape instead.
		switch {
		case exact:
			fields["size"] = fmt.Sprintf("%dx%d", req.Width, req.Height)
		case shape == llm.AspectAuto:
			fields["size"] = "auto"
		default:
			fields["size"] = classicSize(shape, "1024x1024", "1536x1024", "1024x1536")
		}
	case strings.Contains(id, "dall-e-3"):
		fields["response_format"] = "b64_json"
		fields["size"] = classicSize(shape, "1024x1024", "1792x1024", "1024x1792")
	case strings.Contains(id, "grok"):
		// Takes a ratio, not a size.
		fields["response_format"] = "b64_json"
		if shape != llm.AspectAuto {
			fields["aspect_ratio"] = string(shape)
		}
	case strings.Contains(id, "seedream"):
		fields["response_format"] = "b64_json"
		switch {
		case exact:
			fields["size"] = fmt.Sprintf("%dx%d", req.Width, req.Height)
		case shape == llm.AspectAuto:
			fields["size"] = "2048x2048"
		default:
			fields["size"] = sizeOfArea(shape, 2048*2048)
		}
	default:
		fields["response_format"] = "b64_json"
		switch {
		case exact:
			fields["size"] = fmt.Sprintf("%dx%d", req.Width, req.Height)
		case shape != llm.AspectAuto:
			fields["size"] = classicSize(shape, "1024x1024", "1536x1024", "1024x1536")
		}
	}
	if sendUser && req.User != "" {
		fields["user"] = req.User
	}
	for name, value := range req.Extra {
		if value == nil {
			delete(fields, name)
			continue
		}
		fields[name] = value
	}
	return fields
}

// classicSize is the size of a model with three: square, landscape, portrait.
func classicSize(shape llm.Aspect, square, wide, tall string) string {
	switch {
	case shape.Wide():
		return wide
	case shape.Tall():
		return tall
	}
	return square
}

// sizeOfArea is shape at about area pixels, both sides a multiple of 16.
func sizeOfArea(shape llm.Aspect, area int) string {
	ratio := shape.Ratio()
	if ratio <= 0 {
		ratio = 1
	}
	width := int(math.Round(math.Sqrt(float64(area)*ratio)/16)) * 16
	height := int(math.Round(math.Sqrt(float64(area)/ratio)/16)) * 16
	return fmt.Sprintf("%dx%d", width, height)
}

// editBody is the multipart form /images/edits takes: the fields as strings,
// then the pictures. gpt-image takes several as image[]; the older models one
// as image.
func editBody(fields map[string]any, req llm.ImageRequest) (io.Reader, string, error) {
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	for name, value := range fields {
		var text string
		switch v := value.(type) {
		case string:
			text = v
		default:
			encoded, err := json.Marshal(v)
			if err != nil {
				return nil, "", err
			}
			text = string(encoded)
		}
		if err := form.WriteField(name, text); err != nil {
			return nil, "", err
		}
	}
	key := "image"
	if len(req.Images) > 1 || strings.Contains(strings.ToLower(req.Model), "gpt-image") {
		key = "image[]"
	}
	for i, picture := range req.Images {
		name := picture.Filename
		if name == "" {
			name = fmt.Sprintf("image-%d%s", i+1, extensionFor(picture.MediaType))
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, key, strings.ReplaceAll(name, `"`, "")))
		header.Set("Content-Type", picture.MediaType)
		part, err := form.CreatePart(header)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(picture.Data); err != nil {
			return nil, "", err
		}
	}
	if err := form.Close(); err != nil {
		return nil, "", err
	}
	return &buf, form.FormDataContentType(), nil
}

func extensionFor(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ".png"
}

type wireImagesResponse struct {
	Model string          `json:"model"`
	Data  []wireImageItem `json:"data"`
	// SiliconFlow's spelling.
	Images []wireImageItem  `json:"images"`
	Usage  *wireImagesUsage `json:"usage"`
	Error  *wireError       `json:"error"`
}

type wireImageItem struct {
	B64JSON       string `json:"b64_json"`
	URL           string `json:"url"`
	RevisedPrompt string `json:"revised_prompt"`
}

// wireImagesUsage is gpt-image's: input counts text and picture tokens
// together; there is no cache on this endpoint.
type wireImagesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

func (u *wireImagesUsage) toUsage() *llm.Usage {
	if u == nil || (u.InputTokens == 0 && u.OutputTokens == 0) {
		return nil
	}
	return &llm.Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
}
