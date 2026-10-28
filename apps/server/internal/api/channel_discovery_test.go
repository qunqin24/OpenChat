package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// The default endpoint belongs to the vendor, not to the protocol. The first
// pass sent every OpenAI-compatible vendor through the OpenAI adapter's own
// default, which would have carried DeepSeek's or Groq's key to api.openai.com.
//
// This points one vendor at a local upstream and posts a preview with no base
// URL, so a regression that stopped applying the vendor default would show up
// as the request never arriving — or arriving with the wrong credential.
func TestPreviewUsesVendorDefaultEndpoint(t *testing.T) {
	const key = "vendor-secret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+key {
			t.Errorf("authorization = %q, want the submitted key", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-chat"}]}`))
	}))
	defer upstream.Close()

	setChannelTypeEndpoint(t, "deepseek", upstream.URL+"/v1")

	response := httptest.NewRecorder()
	body := `{"type":"deepseek","apiKey":"` + key + `"}`
	(&Server{}).PreviewChannelModels(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var models []ChannelModelInput
	if err := json.Unmarshal(response.Body.Bytes(), &models); err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ModelId != "deepseek-chat" {
		t.Fatalf("models = %+v", models)
	}
}

// setChannelTypeEndpoint repoints one vendor's default for the duration of a
// test. The table is the single source the API consults, and the mutation is
// global, so the test must not run in parallel with anything that reads it.
func setChannelTypeEndpoint(t *testing.T, id ChannelType, endpoint string) {
	t.Helper()
	for i := range channelTypes {
		if channelTypes[i].ID != id {
			continue
		}
		previous := channelTypes[i].Endpoint
		channelTypes[i].Endpoint = endpoint
		t.Cleanup(func() { channelTypes[i].Endpoint = previous })
		return
	}
	t.Fatalf("no channel type %q to repoint", id)
}

// An endpoint that lists nothing is refused before anything is stored. The
// refresh path prunes whatever the list does not mention, so an empty answer
// would otherwise read as "the provider stopped serving everything".
func TestDiscoverChannelModelsRefusesEmptyList(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	_, err := discoverChannelModels(context.Background(), "openai-compatible", upstream.URL+"/v1", "preview-secret")
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "channel_listed_nothing" {
		t.Fatalf("err = %v, want channel_listed_nothing", err)
	}
}

func TestPreviewChannelModelsWithoutSaving(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer preview-secret" {
			t.Errorf("unexpected model-list request: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"private/model"},{"id":"gpt-example"}]}`))
	}))
	defer upstream.Close()

	body := `{"type":"openai-compatible","baseUrl":"` + upstream.URL + `/v1","apiKey":"preview-secret"}`
	response := httptest.NewRecorder()
	// No pool, queries or catalogue: a preview must not depend on persistence.
	(&Server{}).PreviewChannelModels(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "preview-secret") {
		t.Fatal("preview leaked the credential")
	}
	var models []ChannelModelInput
	if err := json.Unmarshal(response.Body.Bytes(), &models); err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	for _, model := range models {
		if model.Source != ChannelModelInputSourceFetched {
			t.Errorf("source = %s", model.Source)
		}
	}
}

func TestPreviewChannelModelsErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key preview-secret"}`))
	}))
	defer upstream.Close()

	for _, tc := range []struct{ name, body, code string }{
		{"missing relay URL", `{"type":"openai-compatible","apiKey":"preview-secret"}`, "channel_base_url_invalid"},
		{"missing key", `{"type":"openai"}`, "channel_key_missing"},
		{"unknown type", `{"type":"unknown"}`, "type_invalid"},
		{"rejected key", `{"type":"openai","baseUrl":"` + upstream.URL + `","apiKey":"preview-secret"}`, "channel_key_rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			(&Server{}).PreviewChannelModels(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body)))
			var body struct{ Code string }
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if response.Code < 400 || body.Code != tc.code || strings.Contains(response.Body.String(), "preview-secret") {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

// Anonymous access is a property of the table, not of Ollama: any entry marked
// `KeyOptional` may fetch a list with no credential, and no other may.
func TestPreviewWithoutKeyFollowsKeyOptional(t *testing.T) {
	for _, def := range channelTypes {
		t.Run(string(def.ID), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("an anonymous request should not send an Authorization header")
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"local-model"}]}`))
			}))
			defer upstream.Close()
			response := httptest.NewRecorder()
			body := `{"type":"` + string(def.ID) + `","baseUrl":"` + upstream.URL + `"}`
			(&Server{}).PreviewChannelModels(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
			// The Anthropic and Gemini adapters speak other dialects, but they
			// refuse a missing key before any request, so only the status is
			// asserted for a type that is not key-optional.
			if def.KeyOptional && response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if !def.KeyOptional && response.Code == http.StatusOK {
				t.Fatalf("a keyless preview of %s succeeded", def.ID)
			}
		})
	}
}

func TestCreateChannelRejectsInvalidSelectionBeforeSaving(t *testing.T) {
	for _, models := range []string{
		`[{"modelId":" ","source":"manual"}]`,
		`[{"modelId":"a","source":"unknown"}]`,
		`[{"modelId":"a","source":"fetched"},{"modelId":" a ","source":"manual"}]`,
	} {
		response := httptest.NewRecorder()
		body := `{"name":"test","type":"openai","models":` + models + `}`
		(&Server{}).CreateChannel(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"field":"models"`) {
			t.Errorf("status = %d, body = %s", response.Code, response.Body.String())
		}
	}
}
