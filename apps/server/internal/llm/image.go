package llm

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg" // registered for DecodeConfig
	_ "image/png"
	"math"
	"net/http"
	"slices"
	"strconv"
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

// Aspect is the shape asked for, as a width:height ratio every image API can
// be mapped from, or auto. The adapter turns it into the model family's own
// sizes or ratios.
type Aspect string

const (
	AspectAuto      Aspect = "auto"
	AspectSquare    Aspect = "1:1"
	AspectLandscape Aspect = "3:2"
	AspectPortrait  Aspect = "2:3"
	Aspect4x3       Aspect = "4:3"
	Aspect3x4       Aspect = "3:4"
	AspectWide      Aspect = "16:9"
	AspectTall      Aspect = "9:16"
	AspectUltrawide Aspect = "21:9"
)

// Aspects are the ratios a model may ask for, besides auto.
var Aspects = []Aspect{AspectSquare, AspectLandscape, AspectPortrait, Aspect4x3, Aspect3x4, AspectWide, AspectTall, AspectUltrawide}

// ParseAspect reads a tool argument: a ratio of Aspects, or one of the words
// square, landscape and portrait (3:2 and 2:3); auto for anything else.
func ParseAspect(s string) Aspect {
	value := Aspect(strings.ToLower(strings.TrimSpace(s)))
	switch value {
	case "square":
		return AspectSquare
	case "landscape":
		return AspectLandscape
	case "portrait":
		return AspectPortrait
	}
	if slices.Contains(Aspects, value) {
		return value
	}
	return AspectAuto
}

// Ratio is width over height; 0 for auto.
func (a Aspect) Ratio() float64 {
	w, h, ok := strings.Cut(string(a), ":")
	if !ok {
		return 0
	}
	wn, err1 := strconv.Atoi(w)
	hn, err2 := strconv.Atoi(h)
	if err1 != nil || err2 != nil || hn == 0 {
		return 0
	}
	return float64(wn) / float64(hn)
}

// Wide says the shape is landscape, Tall portrait; neither for square or auto.
func (a Aspect) Wide() bool { return a.Ratio() > 1.05 }
func (a Aspect) Tall() bool { r := a.Ratio(); return r > 0 && r < 0.95 }

// NearestAspect is the one of choices closest in shape to width × height.
func NearestAspect(width, height int, choices []Aspect) Aspect {
	if width <= 0 || height <= 0 || len(choices) == 0 {
		return AspectAuto
	}
	want := math.Log(float64(width) / float64(height))
	best, gap := choices[0], math.Inf(1)
	for _, choice := range choices {
		if d := math.Abs(math.Log(choice.Ratio()) - want); d < gap {
			best, gap = choice, d
		}
	}
	return best
}

// Quality is how carefully a picture is made, where the model takes it
// (gpt-image: low to max; dall-e-3: standard or hd). Empty is the provider's
// default.
type Quality string

const (
	QualityAuto   Quality = ""
	QualityLow    Quality = "low"
	QualityMedium Quality = "medium"
	QualityHigh   Quality = "high"
	QualityXHigh  Quality = "xhigh"
	QualityMax    Quality = "max"
)

// Qualities are the levels, lowest first.
var Qualities = []Quality{QualityLow, QualityMedium, QualityHigh, QualityXHigh, QualityMax}

// ParseQuality reads a level; auto (empty) for anything else, ok false for a
// word that is not one.
func ParseQuality(s string) (Quality, bool) {
	value := Quality(strings.ToLower(strings.TrimSpace(s)))
	if value == "" || value == "auto" {
		return QualityAuto, true
	}
	return value, slices.Contains(Qualities, value)
}

// Capped is q no higher than limit; a limit of auto caps nothing.
func (q Quality) Capped(limit Quality) Quality {
	if q == QualityAuto || limit == QualityAuto {
		return q
	}
	if slices.Index(Qualities, q) > slices.Index(Qualities, limit) {
		return limit
	}
	return q
}

// Complexity is how much the image model plans before it renders, where it
// takes that (gpt-image 2.6 on): low suits a focused edit, high dense text
// and busy layouts. Empty is the provider's default (high, for 2.6).
type Complexity string

const (
	ComplexityAuto   Complexity = ""
	ComplexityLow    Complexity = "low"
	ComplexityMedium Complexity = "medium"
	ComplexityHigh   Complexity = "high"
)

// ParseComplexity reads a level; auto (empty) for nothing or "auto", ok
// false for a word that is not one.
func ParseComplexity(s string) (Complexity, bool) {
	value := Complexity(strings.ToLower(strings.TrimSpace(s)))
	switch value {
	case "", "auto":
		return ComplexityAuto, true
	case ComplexityLow, ComplexityMedium, ComplexityHigh:
		return value, true
	}
	return ComplexityAuto, false
}

// MinSide and MaxSide bound an exact size a model may ask for.
const (
	MinSide = 256
	MaxSide = 4096
)

// ParseSize reads an exact size like "1920x1080". ok is false for anything
// else, or for a side outside MinSide..MaxSide.
func ParseSize(s string) (width, height int, ok bool) {
	w, h, found := strings.Cut(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "×", "x")), "x")
	if !found {
		return 0, 0, false
	}
	width, err1 := strconv.Atoi(strings.TrimSpace(w))
	height, err2 := strconv.Atoi(strings.TrimSpace(h))
	if err1 != nil || err2 != nil || width < MinSide || height < MinSide || width > MaxSide || height > MaxSide {
		return 0, 0, false
	}
	return width, height, true
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
	// Width and Height are an exact size asked for, zero when none: the
	// adapter sends it where the model takes sizes, and else the nearest
	// shape (and, for Gemini, the resolution tier) it does take.
	Width, Height int
	// Quality is the level asked for, empty for the provider's default.
	Quality Quality
	// Transparent asks for a picture without a background, where the model
	// can make one (gpt-image), as a PNG.
	Transparent bool
	// Complexity is the planning asked for, empty for the provider's default.
	Complexity Complexity
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
	// Ignored names what was asked for that this model does not take
	// ("quality", "transparent"), for the model to be told.
	Ignored []string
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
// tables — gpt-image's by size and quality (auto taken as medium, the two
// levels above high as more again), Gemini's flat count — and anything else
// gets Gemini's, which is the cheaper guess.
func PictureTokens(model string, aspect Aspect, quality Quality) int {
	id := strings.ToLower(model)
	if !strings.Contains(id, "gpt-image") && !strings.Contains(id, "chatgpt-image") {
		return 1290
	}
	square := map[Quality]int{QualityLow: 272, QualityAuto: 1056, QualityMedium: 1056, QualityHigh: 4160, QualityXHigh: 6240, QualityMax: 8320}[quality]
	if aspect.Wide() || aspect.Tall() {
		return square * 3 / 2
	}
	return square
}
