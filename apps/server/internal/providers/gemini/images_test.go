package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImagesOneRequestPerPictureUsageSummed(t *testing.T) {
	picture := testPNG(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasSuffix(r.URL.Path, "/models/gemini-3.1-flash-image:generateContent") || r.URL.Query().Get("alt") != "" || r.URL.Query().Get("key") != "k" {
			t.Errorf("request to %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		var body struct {
			Contents []struct {
				Parts []map[string]any `json:"parts"`
			} `json:"contents"`
			GenerationConfig map[string]any `json:"generationConfig"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Contents) != 1 || len(body.Contents[0].Parts) != 2 || body.Contents[0].Parts[0]["text"] != "a lake" {
			t.Errorf("contents = %+v", body.Contents)
		}
		if cfg, _ := body.GenerationConfig["imageConfig"].(map[string]any); cfg["aspectRatio"] != "2:3" {
			t.Errorf("config = %v", body.GenerationConfig)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{"content": map[string]any{"parts": []map[string]any{
				{"text": "Here it is"}, {"inlineData": map[string]any{"mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(picture)}},
			}}}},
			"usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 1290},
		})
	}))
	defer server.Close()
	client, err := NewImages(Config{Provider: "gemini", BaseURL: server.URL + "/v1beta", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Generate(context.Background(), llm.ImageRequest{
		Model: "gemini-3.1-flash-image", Prompt: "a lake", Count: 2, Aspect: llm.AspectPortrait,
		Images: []llm.ImageInput{{MediaType: "image/png", Data: picture}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(resp.Images) != 2 || !bytes.Equal(resp.Images[1].Data, picture) {
		t.Fatalf("calls = %d, images = %d", calls, len(resp.Images))
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 20 || resp.Usage.OutputTokens != 2580 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestImagesNoPictureIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{"content": map[string]any{"parts": []map[string]any{{"text": "I can't draw that."}}}, "finishReason": "IMAGE_SAFETY"}},
		})
	}))
	defer server.Close()
	client, _ := NewImages(Config{BaseURL: server.URL, APIKey: "k"})
	_, err := client.Generate(context.Background(), llm.ImageRequest{Model: "gemini-3.1-flash-image", Prompt: "x", Count: 1})
	var modelErr *llm.Error
	if err == nil || !llm.IsKind(err, llm.ErrorProvider) {
		t.Fatalf("err = %v", err)
	}
	if e, ok := err.(*llm.Error); ok {
		modelErr = e
	}
	if modelErr == nil || modelErr.Code != "IMAGE_SAFETY" {
		t.Fatalf("code = %v", err)
	}
}
