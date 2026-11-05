package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newImageClient(t *testing.T, server *httptest.Server, sendUser bool) *ImageClient {
	t.Helper()
	client, err := NewImages(ImageConfig{Config: Config{Provider: "openai", BaseURL: server.URL + "/v1", APIKey: "sk-secret", SendUser: sendUser}})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestImagesGenerateSendsTheRequestAndReadsBase64(t *testing.T) {
	picture := testPNG(t)
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" || r.Header.Get("Authorization") != "Bearer sk-secret" {
			t.Errorf("request went to %s with %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(picture)}},
			"usage": map[string]any{"input_tokens": 12, "output_tokens": 1056},
		})
	}))
	defer server.Close()

	resp, err := newImageClient(t, server, true).Generate(context.Background(), llm.ImageRequest{
		Model: "gpt-image-2", Prompt: "a lake", Count: 1, Aspect: llm.AspectLandscape, User: "u1",
		Extra: map[string]any{"quality": "low"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Images) != 1 || resp.Images[0].MediaType != "image/png" || !bytes.Equal(resp.Images[0].Data, picture) {
		t.Fatalf("images = %+v", resp.Images)
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 1056 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	// gpt-image refuses response_format; the size follows the aspect; the
	// operator's extra and the user id ride along.
	if _, ok := got["response_format"]; ok {
		t.Errorf("sent response_format to gpt-image: %v", got)
	}
	// gpt-image takes the end user as safety_identifier.
	if got["size"] != "1536x1024" || got["quality"] != "low" || got["safety_identifier"] != "u1" || got["user"] != nil || got["n"] != float64(1) {
		t.Errorf("body = %v", got)
	}
}

func TestImagesUserOnlyWhenTheChannelSendsIt(t *testing.T) {
	fields, _ := imageFields(llm.ImageRequest{Model: "seedream-4", Prompt: "x", User: "u1", Aspect: llm.AspectPortrait}, false)
	if _, ok := fields["user"]; ok {
		t.Error("user sent on a channel that does not send it")
	}
	if fields["response_format"] != "b64_json" || fields["size"] != "1680x2512" {
		t.Errorf("fields = %v", fields)
	}
	// A null in the operator's parameters removes a field.
	fields, _ = imageFields(llm.ImageRequest{Model: "some-relay-model", Prompt: "x", Extra: map[string]any{"response_format": nil}}, false)
	if _, ok := fields["response_format"]; ok {
		t.Error("a null extra did not remove the field")
	}
}

func TestImagesDownloadsAnAddressAndReadsSiliconFlowsShape(t *testing.T) {
	picture := testPNG(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/generations":
			_ = json.NewEncoder(w).Encode(map[string]any{"images": []map[string]any{{"url": server.URL + "/files/1.png"}}})
		case "/files/1.png":
			// Same host as the channel: fetched with the channel's key.
			if r.Header.Get("Authorization") != "Bearer sk-secret" {
				t.Errorf("download without the key")
			}
			_, _ = w.Write(picture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	resp, err := newImageClient(t, server, false).Generate(context.Background(), llm.ImageRequest{Model: "Kwai-Kolors/Kolors", Prompt: "x", Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Images) != 1 || !bytes.Equal(resp.Images[0].Data, picture) {
		t.Fatalf("images = %+v", resp.Images)
	}
}

func TestImagesEditIsMultipartWithThePictures(t *testing.T) {
	picture := testPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("prompt") != "make it blue" || r.FormValue("model") != "gpt-image-2" || r.FormValue("n") != "1" {
			t.Errorf("fields = %v", r.MultipartForm.Value)
		}
		files := r.MultipartForm.File["image[]"]
		if len(files) != 2 {
			t.Fatalf("pictures = %d", len(files))
		}
		f, _ := files[0].Open()
		data, _ := io.ReadAll(f)
		if !bytes.Equal(data, picture) || files[0].Header.Get("Content-Type") != "image/png" {
			t.Errorf("first picture not sent as itself")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(picture)}}})
	}))
	defer server.Close()
	_, err := newImageClient(t, server, false).Generate(context.Background(), llm.ImageRequest{
		Model: "gpt-image-2", Prompt: "make it blue", Count: 1,
		Images: []llm.ImageInput{{Filename: "a.png", MediaType: "image/png", Data: picture}, {MediaType: "image/png", Data: picture}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestImagesSafetyRefusalKeepsItsCodeAndNoKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"moderation_blocked","message":"Your request was rejected by the safety system. key sk-secret"}}`)
	}))
	defer server.Close()
	_, err := newImageClient(t, server, false).Generate(context.Background(), llm.ImageRequest{Model: "gpt-image-2", Prompt: "x", Count: 1})
	var modelErr *llm.Error
	if !errors.As(err, &modelErr) || modelErr.Code != "moderation_blocked" || modelErr.Kind != llm.ErrorProvider {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(modelErr.Message, "sk-secret") {
		t.Error("the key leaked into the error")
	}
}

func TestImagesRefusesWhatIsNotAPicture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString([]byte("<html>hi</html>"))}}})
	}))
	defer server.Close()
	_, err := newImageClient(t, server, false).Generate(context.Background(), llm.ImageRequest{Model: "gpt-image-2", Prompt: "x", Count: 1})
	if !llm.IsKind(err, llm.ErrorProtocol) {
		t.Fatalf("err = %v", err)
	}
}

func TestImagesSizesPerFamily(t *testing.T) {
	cases := []struct {
		model  string
		aspect llm.Aspect
		w, h   int
		field  string
		want   string
	}{
		// An exact size goes as asked to the models that take sizes.
		// gpt-image's sizes come to its bounds: sides of 16, ratio, pixels.
		{"gpt-image-2", llm.AspectAuto, 1920, 1080, "size", "1920x1088"},
		{"gpt-image-2", llm.AspectAuto, 4096, 4096, "size", "2880x2880"},
		{"gpt-image-2", llm.AspectAuto, 3000, 600, "size", "3008x1008"},
		{"gpt-image-2", llm.AspectAuto, 512, 512, "size", "816x816"},
		{"seedream-4", llm.AspectAuto, 2560, 1440, "size", "2560x1440"},
		{"some-relay-model", llm.AspectAuto, 800, 600, "size", "800x600"},
		// A ratio becomes the family's own size for it.
		{"gpt-image-2", llm.AspectWide, 0, 0, "size", "1536x1024"},
		{"gpt-image-2", llm.AspectAuto, 0, 0, "size", "auto"},
		{"dall-e-3", llm.AspectTall, 0, 0, "size", "1024x1792"},
		{"seedream-4", llm.AspectWide, 0, 0, "size", "2736x1536"},
		// Grok takes a ratio: an exact size becomes the nearest one.
		{"grok-imagine-image", llm.AspectAuto, 1920, 1080, "aspect_ratio", "16:9"},
		{"grok-imagine-image", llm.Aspect4x3, 0, 0, "aspect_ratio", "4:3"},
	}
	for _, c := range cases {
		fields, _ := imageFields(llm.ImageRequest{Model: c.model, Prompt: "x", Aspect: c.aspect, Width: c.w, Height: c.h}, false)
		if fields[c.field] != c.want {
			t.Errorf("%s %s %dx%d: %s = %v, want %s", c.model, c.aspect, c.w, c.h, c.field, fields[c.field], c.want)
		}
	}
}

func TestImagesQualityAndTransparency(t *testing.T) {
	fields, ignored := imageFields(llm.ImageRequest{Model: "gpt-image-2.5-flare", Prompt: "x", Quality: llm.QualityXHigh, Transparent: true}, false)
	if fields["quality"] != "xhigh" || fields["background"] != "transparent" || fields["output_format"] != "png" || len(ignored) != 0 {
		t.Fatalf("gpt-image: %v %v", fields, ignored)
	}
	fields, ignored = imageFields(llm.ImageRequest{Model: "dall-e-3", Prompt: "x", Quality: llm.QualityMax, Transparent: true}, false)
	if fields["quality"] != "hd" || !slices.Equal(ignored, []string{"transparent"}) {
		t.Fatalf("dall-e-3: %v %v", fields, ignored)
	}
	fields, ignored = imageFields(llm.ImageRequest{Model: "seedream-4", Prompt: "x", Quality: llm.QualityHigh, Transparent: true}, false)
	if _, sent := fields["quality"]; sent || !slices.Equal(ignored, []string{"quality", "transparent"}) {
		t.Fatalf("seedream: %v %v", fields, ignored)
	}
}

func TestImagesComplexityOnlyFromTwoSix(t *testing.T) {
	cases := []struct {
		model string
		sent  bool
	}{
		{"gpt-image-2.6-flare", true},
		{"gpt-image-2.6-sunburst", true},
		{"gpt-image-3", true},
		{"gpt-image-2.5-flare", false},
		{"gpt-image-2", false},
		{"chatgpt-image-latest", false},
		{"seedream-4", false},
	}
	for _, c := range cases {
		fields, ignored := imageFields(llm.ImageRequest{Model: c.model, Prompt: "x", Complexity: llm.ComplexityLow}, false)
		_, sent := fields["complexity"]
		if sent != c.sent || slices.Contains(ignored, "complexity") == c.sent {
			t.Errorf("%s: sent %v, ignored %v", c.model, sent, ignored)
		}
	}
	// A relay that is not OpenAI still gets user.
	fields, _ := imageFields(llm.ImageRequest{Model: "seedream-4", Prompt: "x", User: "u1"}, true)
	if fields["user"] != "u1" || fields["safety_identifier"] != nil {
		t.Fatalf("relay: %v", fields)
	}
}
