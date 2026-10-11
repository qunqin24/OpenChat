package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/storage"
)

// The size of a message is the sender's to choose, so a message too big to be
// checked whole must never read as "the service failed": the on-error setting
// may let that through, and padding would be a way past the check. It is
// refused (413 message_too_large_for_moderation) in every mode and for every
// setting; a service that really fails still follows on_error.

// tooLarge checks a response is the 413 of a message too big to be checked.
func tooLarge(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var body blockedBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusRequestEntityTooLarge || body.Code != "message_too_large_for_moderation" {
		t.Fatalf("want 413 message_too_large_for_moderation, got %d %s", rec.Code, rec.Body)
	}
}

func unavailable(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var body blockedBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body.Code != "moderation_unavailable" {
		t.Fatalf("want 503 moderation_unavailable, got %d %s", rec.Code, rec.Body)
	}
}

// accepted checks the message was accepted and lets its run finish.
func (h *askHarness) acceptedMessage(rec *httptest.ResponseRecorder) {
	h.t.Helper()
	var sent CreateMessageResponse
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &sent) != nil {
		h.t.Fatalf("want 202, got %d %s", rec.Code, rec.Body)
	}
	h.settle(sent.Run.Id)
}

// classifierHarness is an askHarness in model mode: the model is the classifier
// (it flags a text containing BLOCKME, or answers with nonsense while
// `garbage` is set) and, for the chat, says "fine".
func classifierHarness(t *testing.T, onError string) (h *askHarness, garbage *atomic.Bool) {
	t.Helper()
	garbage = &atomic.Bool{}
	h = newAskHarness(t, openAIWire, func(n int) string {
		h.mu.Lock()
		body := h.bodies[n-1]
		h.mu.Unlock()
		if strings.Contains(body, "content-safety classifier") {
			switch {
			case garbage.Load():
				return openAIWire.text("I cannot tell.")
			case strings.Contains(body, "BLOCKME"):
				return openAIWire.text(classifierAnswer("violence/graphic"))
			}
			return openAIWire.text(classifierAnswer())
		}
		return openAIWire.text("fine")
	})
	resetModeration(t, h)
	h.server.usage = h.server.runner.Recorder()
	h.server.endUsers = NewEndUsers(h.pool)
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM usage_logs WHERE user_id = $1`, h.user.ID)
	})
	if rec := h.patchModeration(fmt.Sprintf(`{"mode":"model","model":%q,"onError":%q,"strikes":0}`, h.model, onError)); rec.Code != http.StatusOK {
		t.Fatalf("configure: %d %s", rec.Code, rec.Body)
	}
	return h, garbage
}

// A text refused on its own is refused when padded to any length, however the
// service is set to fail - and the padded text is not asked about in part.
func TestPaddingDoesNotPassTheModelCheck(t *testing.T) {
	for _, onError := range []string{"allow", "block"} {
		t.Run(onError, func(t *testing.T) {
			h, garbage := classifierHarness(t, onError)
			messages := func() int {
				return h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status <> 'blocked'`, h.conv.ID)
			}

			if got := refused(t, h.post("BLOCKME")); strings.Join(got, ",") != "violence/graphic" {
				t.Fatalf("the text alone: %v", got)
			}
			asked, written := h.chatRequests(), messages()
			// Nine pieces' worth: more than a check makes requests for.
			tooMany := "BLOCKME " + strings.Repeat("a", 200_000)
			tooLarge(t, h.post(tooMany))
			// And past the absolute limit.
			tooLarge(t, h.post("BLOCKME "+strings.Repeat("a", moderationMaxRunes)))
			if h.chatRequests() != asked {
				t.Error("a text that cannot be checked whole was sent to a model")
			}
			if messages() != written {
				t.Error("a refused message left rows")
			}
			if h.count(`SELECT count(*) FROM moderation_events WHERE user_id = $1`, h.user.ID) != 1 {
				t.Error("being too large to check was logged as a violation")
			}

			// A service that really fails still follows the setting.
			garbage.Store(true)
			rec := h.post("hello there")
			if onError == "allow" {
				h.acceptedMessage(rec)
			} else {
				unavailable(t, rec)
			}
		})
	}
}

func TestPaddingDoesNotPassTheOpenAICheck(t *testing.T) {
	for _, onError := range []string{"allow", "block"} {
		t.Run(onError, func(t *testing.T) {
			h, f := newModerationHarness(t, fmt.Sprintf(`,"onError":%q,"strikes":0`, onError))
			if got := refused(t, h.post("BLOCKME")); strings.Join(got, ",") != "violence/graphic" {
				t.Fatalf("the text alone: %v", got)
			}
			calls := f.calls()
			tooLarge(t, h.post("BLOCKME "+strings.Repeat("a", moderationMaxRunes)))
			if f.calls() != calls {
				t.Error("part of a text that cannot be checked whole was sent for a check")
			}

			f.setStatus(http.StatusInternalServerError)
			rec := h.post("hello there")
			if onError == "allow" {
				h.acceptedMessage(rec)
			} else {
				unavailable(t, rec)
			}
		})
	}
}

// A classifier that cannot take a piece of the text in its context window says
// something about the size of the text, not about the service.
func TestClassifierContextLimitIsTheSizeOfTheMessage(t *testing.T) {
	h, _ := classifierHarness(t, "allow")
	var model llm.ModelClient = failingClassifier{llm.Errorf(llm.ErrorContextLength, nil, "too long for the window")}
	var channelID uuid.UUID
	if err := h.pool.QueryRow(context.Background(), `SELECT channel_id FROM channel_models WHERE model_id = $1`, h.model).Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadModeration(context.Background(), h.q)
	if err != nil {
		t.Fatal(err)
	}
	who := moderationSubject{userID: h.user.ID, source: ModerationSourceSend}
	_, err = h.server.classify(context.Background(), cfg, who, channelID, model, llm.Request{Model: h.model})
	if !errors.Is(err, errTooLargeToCheck) {
		t.Fatalf("a context-length failure of the classifier: %v", err)
	}
	// Any other failure of it is a failure.
	model = failingClassifier{llm.Errorf(llm.ErrorProvider, nil, "overloaded")}
	if _, err = h.server.classify(context.Background(), cfg, who, channelID, model, llm.Request{Model: h.model}); err == nil || errors.Is(err, errTooLargeToCheck) {
		t.Fatalf("a provider failure of the classifier: %v", err)
	}
}

type failingClassifier struct{ err error }

func (f failingClassifier) Stream(context.Context, llm.Request, func(llm.Event) error) (llm.ModelResponse, error) {
	return llm.ModelResponse{}, f.err
}

// A store whose reads of one key come back "too large" - the object was written
// again, bigger, after the upload was admitted - or fail.
type oversizeStore struct {
	*memoryStore
	big  string
	fail string
}

func (o oversizeStore) Read(ctx context.Context, key string, limit int64) ([]byte, error) {
	switch key {
	case o.big:
		return nil, storage.ErrTooLarge
	case o.fail:
		return nil, errors.New("the store is down")
	}
	return o.memoryStore.Read(ctx, key, limit)
}

// More pictures than a check sends, or one bigger than a check reads, is a
// message too large to check: refused whatever on_error says, and none of its
// files claimed. A store that fails to read one is a failing service.
func TestImagesTooMuchToCheckAreRefused(t *testing.T) {
	h, f := newModerationHarness(t, `,"categories":["sexual"],"onError":"allow"`)
	blobs := &memoryStore{blobs: map[string][]byte{}}
	h.server.store = blobs
	if _, err := h.q.SaveModelOverride(context.Background(), sqlc.SaveModelOverrideParams{ModelID: h.model, InputModalities: []string{"image"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), "DELETE FROM model_overrides WHERE model_id = $1", h.model)
	})
	type file struct {
		id  uuid.UUID
		key string
	}
	upload := func(data string) file {
		t.Helper()
		rec := httptest.NewRecorder()
		h.server.CreateUpload(rec, h.request(http.MethodPost, fmt.Sprintf(`{"filename":"x.png","contentType":"image/png","size":%d}`, len(data))))
		var up Upload
		if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &up) != nil {
			t.Fatalf("upload: %d %s", rec.Code, rec.Body)
		}
		var key string
		if err := h.pool.QueryRow(context.Background(), `SELECT storage_key FROM attachments WHERE id = $1`, up.Id).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if err := blobs.PutObject(context.Background(), key, "image/png", []byte(data)); err != nil {
			t.Fatal(err)
		}
		return file{up.Id, key}
	}
	sendWith := func(files ...file) *httptest.ResponseRecorder {
		var ids []string
		for _, f := range files {
			ids = append(ids, fmt.Sprintf("%q", f.id))
		}
		rec := httptest.NewRecorder()
		h.server.CreateMessage(rec, h.request(http.MethodPost,
			fmt.Sprintf(`{"content":"look","model":%q,"attachmentIds":[%s]}`, h.model, strings.Join(ids, ","))), h.conv.ID)
		return rec
	}
	claimed := func(files []file) (n int) {
		for _, f := range files {
			n += h.count(`SELECT count(*) FROM attachments WHERE id = $1 AND message_id IS NOT NULL`, f.id)
		}
		return n
	}

	// Seven pictures (the cap of a check is six); one of them is the bad one.
	var many []file
	for i := 0; i < moderationMaxImages+1; i++ {
		data := fmt.Sprintf("\x89PNG %d", i)
		if i == 3 {
			data += " BADIMG"
		}
		many = append(many, upload(data))
	}
	calls := f.calls()
	tooLarge(t, sendWith(many...))
	if f.calls() != calls {
		t.Error("some of the pictures were checked, the rest not")
	}
	if claimed(many) != 0 {
		t.Error("a refused message claimed its files")
	}

	// Six are checked.
	h.discardRefused()
	if got := refused(t, sendWith(many[:moderationMaxImages]...)); strings.Join(got, ",") != "sexual" {
		t.Errorf("six pictures, one of them bad: %v", got)
	}

	// One that grew past the limit after it was admitted.
	h.discardRefused()
	fine, grown := upload("\x89PNG fine"), upload("\x89PNG grown")
	h.server.store = oversizeStore{memoryStore: blobs, big: grown.key}
	tooLarge(t, sendWith(fine, grown))

	// A store that cannot read it is a service that failed, which the
	// setting decides.
	h.server.store = oversizeStore{memoryStore: blobs, fail: grown.key}
	if rec := h.patchModeration(`{"onError":"block"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	unavailable(t, sendWith(fine, grown))
}
