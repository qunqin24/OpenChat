package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/usage"
)

// speechFixture is a transcription model behind two OpenAI-shaped services:
// the first (higher priority) and a second to fail over to.
type speechFixture struct {
	t        *testing.T
	pool     *pgxpool.Pool
	s        *Server
	modelID  string
	member   sqlc.User
	admin    sqlc.User
	first    http.HandlerFunc
	second   http.HandlerFunc
	firstN   atomic.Int32
	secondN  atomic.Int32
	lastForm atomic.Value // map[string]string of the last request's fields
}

func newSpeechFixture(t *testing.T) *speechFixture {
	t.Helper()
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	f := &speechFixture{t: t, pool: pool}
	tag := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	f.modelID = "gpt-transcribe-test-" + tag
	f.s = &Server{pool: pool, q: q, usage: usage.NewRecorder(q), endUsers: NewEndUsers(pool)}

	serve := func(n *atomic.Int32, handler func() http.HandlerFunc) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			if r.URL.Path != "/v1/audio/transcriptions" {
				http.NotFound(w, r)
				return
			}
			_ = r.ParseMultipartForm(1 << 20)
			fields := map[string]string{}
			for name, values := range r.MultipartForm.Value {
				fields[name] = values[0]
			}
			f.lastForm.Store(fields)
			handler()(w, r)
		}))
		t.Cleanup(server.Close)
		return server
	}
	ok := func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"hello world","usage":{"type":"duration","seconds":29.5}}`))
	}
	f.first, f.second = ok, ok
	one := serve(&f.firstN, func() http.HandlerFunc { return f.first })
	two := serve(&f.secondN, func() http.HandlerFunc { return f.second })

	var channels []uuid.UUID
	for i, server := range []*httptest.Server{one, two} {
		channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
			Name: fmt.Sprintf("speech %d %s", i, tag), Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true,
			BaseUrl: server.URL + "/v1", ApiKey: "sk-test", Priority: int32(2 - i), SendUserID: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		channels = append(channels, channel.ID)
		if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
			ChannelID: channel.ID, ModelIds: []string{f.modelID}, DisplayNames: []string{""}, Sources: []string{"manual"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	for role, into := range map[sqlc.UserRole]*sqlc.User{sqlc.UserRoleUser: &f.member, sqlc.UserRoleAdmin: &f.admin} {
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, $3) RETURNING id, email, name, role`,
			string(role)+tag+"@test.invalid", string(role)+tag, role).Scan(&into.ID, &into.Email, &into.Name, &into.Role); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, "DELETE FROM app_settings WHERE key = $1", settingSpeechInput)
		_, _ = pool.Exec(bg, "DELETE FROM usage_logs WHERE model_id = $1", f.modelID)
		_, _ = pool.Exec(bg, "DELETE FROM model_overrides WHERE model_id = $1", f.modelID)
		_, _ = pool.Exec(bg, "DELETE FROM channels WHERE id = ANY($1)", channels)
		_, _ = pool.Exec(bg, "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{f.member.ID, f.admin.ID})
	})
	// Start from a clean setting whatever an earlier test left.
	_, _ = pool.Exec(ctx, "DELETE FROM app_settings WHERE key = $1", settingSpeechInput)
	f.saveModel(`{"kind":"transcription","audioPrice":0.006}`)
	return f
}

func (f *speechFixture) saveModel(body string) {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.s.UpdateModelSettings(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)), f.modelID)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("save model %s: %d %s", body, rec.Code, rec.Body)
	}
}

func (f *speechFixture) patch(as sqlc.User, body string) *httptest.ResponseRecorder {
	f.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body))
	req = req.WithContext(auth.WithUser(req.Context(), as))
	f.s.UpdateSpeechInputSettings(rec, req)
	return rec
}

func (f *speechFixture) mustEnable() {
	f.t.Helper()
	body := fmt.Sprintf(`{"enabled":true,"model":%q}`, f.modelID)
	if rec := f.patch(f.admin, body); rec.Code != http.StatusOK {
		f.t.Fatalf("enable: %d %s", rec.Code, rec.Body)
	}
}

func (f *speechFixture) features(as sqlc.User) ChatFeatures {
	f.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(auth.WithUser(req.Context(), as))
	f.s.GetChatFeatures(rec, req)
	var out ChatFeatures
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		f.t.Fatalf("features: %d %s", rec.Code, rec.Body)
	}
	return out
}

func (f *speechFixture) transcribe(as sqlc.User, contentType string, seconds int, body []byte) *httptest.ResponseRecorder {
	f.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req = req.WithContext(auth.WithUser(req.Context(), as))
	f.s.Transcribe(rec, req, TranscribeParams{Seconds: seconds})
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code  string `json:"code"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != "" {
		return body.Code
	}
	return body.Error.Code
}

type speechRow struct {
	Kind, Status string
	Seconds      *float64
	Price, Cost  *float64
	Estimated    bool
	Channel      string
}

func (f *speechFixture) rows() []speechRow {
	f.t.Helper()
	rows, err := f.pool.Query(context.Background(), `
		SELECT kind, status, audio_seconds::float8, audio_price::float8, cost::float8, estimated, channel_name
		FROM usage_logs WHERE model_id = $1 ORDER BY created_at, id`, f.modelID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []speechRow
	for rows.Next() {
		var r speechRow
		if err := rows.Scan(&r.Kind, &r.Status, &r.Seconds, &r.Price, &r.Cost, &r.Estimated, &r.Channel); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func closeTo(a *float64, want float64) bool { return a != nil && *a > want-1e-9 && *a < want+1e-9 }

func TestSpeechInputSettings(t *testing.T) {
	f := newSpeechFixture(t)
	get := func() SpeechInputSettings {
		rec := httptest.NewRecorder()
		f.s.GetSpeechInputSettings(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		var out SpeechInputSettings
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("get: %d %s", rec.Code, rec.Body)
		}
		return out
	}
	if got := get(); got.Enabled || got.Model != "" || got.MaxSeconds != 300 || got.ModelPriced {
		t.Errorf("default = %+v", got)
	}
	for _, body := range []string{
		`{"enabled":true}`,                            // on with no model
		`{"maxSeconds":9}`,                            // too short
		`{"maxSeconds":1801}`,                         // too long
		`{"model":"no-such-model-` + f.modelID + `"}`, // served by nobody
	} {
		rec := f.patch(f.admin, body)
		if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "invalid_speech_input_settings" {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	// A model that is not a transcription model is refused.
	f.saveModel(`{"kind":"text"}`)
	if rec := f.patch(f.admin, fmt.Sprintf(`{"model":%q}`, f.modelID)); rec.Code != http.StatusBadRequest {
		t.Errorf("text model accepted: %d %s", rec.Code, rec.Body)
	}
	f.saveModel(`{"kind":"transcription","audioPrice":0.006}`)

	rec := f.patch(f.admin, fmt.Sprintf(`{"enabled":true,"model":%q,"maxSeconds":60}`, f.modelID))
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if got := get(); !got.Enabled || got.Model != f.modelID || got.MaxSeconds != 60 || !got.ModelPriced {
		t.Errorf("round trip = %+v", got)
	}
	// A partial change keeps the rest.
	f.patch(f.admin, `{"maxSeconds":120}`)
	if got := get(); !got.Enabled || got.Model != f.modelID || got.MaxSeconds != 120 {
		t.Errorf("partial = %+v", got)
	}
	// A model with no price of any kind is not priced.
	f.saveModel(`{"kind":"transcription"}`)
	if get().ModelPriced {
		t.Error("an unpriced model reads as priced")
	}
}

func TestAudioPriceOnTheModel(t *testing.T) {
	f := newSpeechFixture(t)
	rec := httptest.NewRecorder()
	f.s.UpdateModelSettings(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"kind":"transcription","audioPrice":0.0045}`)), f.modelID)
	var saved ModelOverride
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	if rec.Code != http.StatusOK || saved.AudioPrice == nil || *saved.AudioPrice != 0.0045 || saved.Kind == nil || *saved.Kind != ModelKindTranscription {
		t.Fatalf("saved: %d %s", rec.Code, rec.Body)
	}
	for _, body := range []string{`{"audioPrice":-1}`, `{"audioPrice":101}`, `{"audioPrice":0.0000001}`} {
		rec := httptest.NewRecorder()
		f.s.UpdateModelSettings(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)), f.modelID)
		if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "invalid_audio_price" {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	// A price alone is a decision worth keeping.
	f.saveModel(`{"audioPrice":0.01}`)
	row, err := f.s.q.GetModelOverride(context.Background(), f.modelID)
	if err != nil || !closeTo(numberPtr(row.AudioPrice), 0.01) {
		t.Errorf("override = %+v, %v", row, err)
	}
}

func TestSpeechInputFeatures(t *testing.T) {
	f := newSpeechFixture(t)
	if got := f.features(f.member); got.SpeechInput != nil {
		t.Errorf("off, yet offered: %+v", got.SpeechInput)
	}
	f.mustEnable()
	if got := f.features(f.member); got.SpeechInput == nil || got.SpeechInput.MaxSeconds != 300 {
		t.Errorf("on, offered %+v", got.SpeechInput)
	}
	// Switched on with the model gone from the setting.
	f.patch(f.admin, `{"enabled":false}`)
	if got := f.features(f.member); got.SpeechInput != nil {
		t.Error("switched off, yet offered")
	}
	f.patch(f.admin, `{"enabled":true}`)
	// Kept for administrators: a normal user is not offered it.
	f.saveModel(`{"kind":"transcription","audioPrice":0.006,"adminOnly":true}`)
	if got := f.features(f.member); got.SpeechInput != nil {
		t.Error("an admin-only model is offered to a member")
	}
	if got := f.features(f.admin); got.SpeechInput == nil {
		t.Error("an admin-only model is not offered to an administrator")
	}
	// No longer a transcription model.
	f.saveModel(`{"kind":"text"}`)
	if got := f.features(f.admin); got.SpeechInput != nil {
		t.Error("a text model is offered for speech")
	}
}

func TestTranscribe(t *testing.T) {
	f := newSpeechFixture(t)
	audio := []byte("not really audio")

	if rec := f.transcribe(f.member, "audio/webm", 10, audio); rec.Code != http.StatusConflict || errorCode(t, rec) != "speech_input_off" {
		t.Fatalf("off: %d %s", rec.Code, rec.Body)
	}
	f.mustEnable()
	f.patch(f.admin, `{"maxSeconds":60}`)

	rec := f.transcribe(f.member, "audio/webm;codecs=opus", 30, audio)
	var result TranscriptionResult
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.Text != "hello world" {
		t.Fatalf("happy path: %d %s", rec.Code, rec.Body)
	}
	form := f.lastForm.Load().(map[string]string)
	if form["model"] != f.modelID || form["user"] == "" {
		t.Errorf("the service was sent %v", form)
	}
	rows := f.rows()
	if len(rows) != 1 || rows[0].Kind != "transcription" || rows[0].Status != "ok" || rows[0].Estimated ||
		!closeTo(rows[0].Seconds, 29.5) || !closeTo(rows[0].Price, 0.006) || !closeTo(rows[0].Cost, 29.5/60*0.006) {
		t.Errorf("ledger = %+v", rows)
	}

	// A provider that reports no length is billed the client's, estimated.
	f.first = func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"text":"quiet"}`)) }
	if rec := f.transcribe(f.member, "audio/mp4", 30, audio); rec.Code != http.StatusOK {
		t.Fatalf("no usage: %d %s", rec.Code, rec.Body)
	}
	if rows := f.rows(); len(rows) != 2 || !rows[1].Estimated || !closeTo(rows[1].Seconds, 30) || !closeTo(rows[1].Cost, 0.003) {
		t.Errorf("ledger = %+v", rows)
	}

	// Refusals before any service is asked.
	asked := f.firstN.Load() + f.secondN.Load()
	for name, c := range map[string]struct {
		contentType string
		seconds     int
		body        []byte
		status      int
		code        string
	}{
		"type":      {"video/mp4", 10, audio, http.StatusUnsupportedMediaType, "unsupported_audio"},
		"no type":   {"", 10, audio, http.StatusUnsupportedMediaType, "unsupported_audio"},
		"too long":  {"audio/webm", 66, audio, http.StatusRequestEntityTooLarge, "audio_too_long"},
		"too large": {"audio/webm", 10, make([]byte, 25<<20+1), http.StatusRequestEntityTooLarge, "audio_too_large"},
		"empty":     {"audio/webm", 10, nil, http.StatusBadRequest, "empty_audio"},
	} {
		rec := f.transcribe(f.member, c.contentType, c.seconds, c.body)
		if rec.Code != c.status || errorCode(t, rec) != c.code {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	// Five seconds over is the allowed slack.
	if rec := f.transcribe(f.member, "audio/ogg", 65, audio); rec.Code != http.StatusOK {
		t.Errorf("slack: %d %s", rec.Code, rec.Body)
	}
	if got := f.firstN.Load() + f.secondN.Load(); got != asked+1 {
		t.Errorf("services were asked %d times, want %d", got-asked, 1)
	}
}

func TestTranscribeFailsOver(t *testing.T) {
	f := newSpeechFixture(t)
	f.mustEnable()
	audio := []byte("audio")
	f.first = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"overloaded"}}`))
	}
	rec := f.transcribe(f.member, "audio/webm", 30, audio)
	var result TranscriptionResult
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.Text != "hello world" {
		t.Fatalf("failover: %d %s", rec.Code, rec.Body)
	}
	if f.firstN.Load() != 1 || f.secondN.Load() != 1 {
		t.Errorf("asked %d and %d times", f.firstN.Load(), f.secondN.Load())
	}
	rows := f.rows()
	if len(rows) != 2 || rows[0].Status != "error" || rows[0].Seconds != nil || rows[0].Cost != nil || rows[1].Status != "ok" {
		t.Errorf("ledger = %+v", rows)
	}

	// Every channel failing: a bad gateway, or the service's own rate limit.
	f.second = f.first
	if rec := f.transcribe(f.member, "audio/webm", 30, audio); rec.Code != http.StatusBadGateway || errorCode(t, rec) != "transcription_failed" {
		t.Errorf("all failed: %d %s", rec.Code, rec.Body)
	}
	f.first = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }
	f.second = f.first
	if rec := f.transcribe(f.member, "audio/webm", 30, audio); rec.Code != http.StatusTooManyRequests || errorCode(t, rec) != "model_rate_limited" {
		t.Errorf("rate limited: %d %s", rec.Code, rec.Body)
	}
}

func TestTranscribeCapacity(t *testing.T) {
	f := newSpeechFixture(t)
	f.mustEnable()
	recorder := usage.NewRecorder(f.s.q)
	recorder.SetReserver(NewSpendReserver(f.pool))
	f.s.usage = recorder
	// $1 a minute against a cap of 50 cents: a minute of audio is held at $1
	// and refused before any service is asked.
	f.saveModel(`{"kind":"transcription","audioPrice":1,"capacity":0.5,"capacityPeriod":"total","capacityAdminExempt":false}`)
	rec := f.transcribe(f.member, "audio/webm", 60, []byte("audio"))
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "model_capacity_reached" {
		t.Fatalf("held: %d %s", rec.Code, rec.Body)
	}
	if f.firstN.Load()+f.secondN.Load() != 0 {
		t.Error("a service was asked")
	}
	// Ten seconds is held at 17 cents and goes.
	if rec := f.transcribe(f.member, "audio/webm", 10, []byte("audio")); rec.Code != http.StatusOK {
		t.Fatalf("small: %d %s", rec.Code, rec.Body)
	}
	// A cap already reached refuses at once.
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO usage_logs (kind, status, model_id, cost) VALUES ('transcription', 'ok', $1, 5)`, f.modelID); err != nil {
		t.Fatal(err)
	}
	before := f.firstN.Load()
	if rec := f.transcribe(f.member, "audio/webm", 1, []byte("audio")); rec.Code != http.StatusConflict || errorCode(t, rec) != "model_capacity_reached" {
		t.Errorf("reached: %d %s", rec.Code, rec.Body)
	}
	if f.firstN.Load() != before {
		t.Error("a service was asked past the cap")
	}
}

func TestTranscribeChannelWithoutTranscription(t *testing.T) {
	f := newSpeechFixture(t)
	f.mustEnable()
	// The first channel cannot transcribe (an Anthropic-wire one); the next does.
	original := transcriptionClientFor
	t.Cleanup(func() { transcriptionClientFor = original })
	var tried []string
	transcriptionClientFor = func(channel sqlc.Channel, modelID string) (llm.TranscriptionClient, error) {
		tried = append(tried, channel.Name)
		if len(tried) == 1 {
			return nil, errNoTranscription
		}
		return original(channel, modelID)
	}
	rec := f.transcribe(f.member, "audio/webm", 30, []byte("audio"))
	if rec.Code != http.StatusOK || len(tried) != 2 {
		t.Errorf("%d %s, tried %v", rec.Code, rec.Body, tried)
	}
}
