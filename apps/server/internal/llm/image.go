package llm

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg" // registered for DecodeConfig
	_ "image/png"
	"net/http"
	"strings"

	_ "golang.org/x/image/webp"
)

// ImageClient makes pictures. It stands beside ModelClient rather than inside
// it: an image request is one call with no stream of parts, no tools and no
// conversation, and the run layer never treats one as a turn
// (docs/design/image-generation-plan.md).
type ImageClient interface {
	Generate(ctx context.Context, req ImageRequest) (ImageResponse, error)
}

// Aspect is the shape asked for, in words every image API can be mapped from.
// The adapter turns it into the model family's own sizes.
type Aspect string

const (
	AspectAuto      Aspect = "auto"
	AspectSquare    Aspect = "square"
	AspectLandscape Aspect = "landscape"
	AspectPortrait  Aspect = "portrait"
)

// ParseAspect reads a tool argument, defaulting to auto for anything else.
func ParseAspect(s string) Aspect {
	switch Aspect(strings.ToLower(strings.TrimSpace(s))) {
	case AspectSquare:
		return AspectSquare
	case AspectLandscape:
		return AspectLandscape
	case AspectPortrait:
		return AspectPortrait
	}
	return AspectAuto
}

// ImageInput is a picture to edit, already read and checked by the caller
// (agent's attachmentResolver): an adapter never resolves a reference itself.
type ImageInput struct {
	Filename  string
	MediaType string
	Data      []byte
}

type ImageRequest struct {
	Model  string
	Prompt string
	// Images, when there are any, make this an edit of them.
	Images []ImageInput
	// Count is how many pictures to make, at least one.
	Count  int
	Aspect Aspect
	// User is the end-user identifier every model request carries
	// (end_user.go); an adapter sends it only where its channel says to.
	User string
	// Extra is merged into the request body last: the operator's per-model
	// parameters (quality, output_format, …).
	Extra map[string]any
}

// GeneratedImage is one picture, its bytes and what they are.
type GeneratedImage struct {
	MediaType string
	Data      []byte
}

type ImageResponse struct {
	Images []GeneratedImage
	// Usage is what the provider reported, nil when it said nothing.
	Usage       *Usage
	Provider    string
	ServedModel string
	// Revised is the prompt the provider says it actually drew from, when it
	// rewrites prompts (dall-e-3); empty otherwise.
	Revised string
}

// Picture media types we keep. GIF is left out: no image model makes one, and
// an animated file is not what an image tool promises.
var pictureTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}

// SniffPicture says what a generated picture's bytes are, by their content
// rather than by what the provider claimed, and how big it is. ok is false for
// anything that is not a PNG, JPEG or WebP that decodes.
func SniffPicture(data []byte) (mediaType string, width, height int, ok bool) {
	mediaType = http.DetectContentType(data)
	if !pictureTypes[mediaType] {
		return "", 0, 0, false
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return "", 0, 0, false
	}
	return mediaType, config.Width, config.Height, true
}

// PictureTokens is an estimate of what one picture costs in output tokens,
// for a request whose provider reported nothing: the ledger bills it at the
// model's output price (usage.Meter). The figures are the providers' own
// tables — gpt-image's by size at medium quality, Gemini's flat count — and
// anything else gets Gemini's, which is the cheaper guess.
func PictureTokens(model string, aspect Aspect) int {
	id := strings.ToLower(model)
	switch {
	case strings.Contains(id, "gpt-image"), strings.Contains(id, "chatgpt-image"):
		if aspect == AspectLandscape || aspect == AspectPortrait {
			return 1568
		}
		return 1056
	default:
		return 1290
	}
}
