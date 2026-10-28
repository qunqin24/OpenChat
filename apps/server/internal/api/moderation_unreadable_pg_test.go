package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// Settings that cannot be read in full hold a message back (503), whatever the
// readable ones say - they might be the very ones that decide what a failure
// means - unless the mode itself was read and is off.
func TestUnreadableModerationSettingsHoldMessagesBack(t *testing.T) {
	h, f := newModerationHarness(t, `,"onError":"block"`)
	ctx := context.Background()
	corrupt := func(key, raw string) {
		t.Helper()
		if _, err := h.pool.Exec(ctx, `UPDATE app_settings SET value = $2::jsonb WHERE key = $1`, key, raw); err != nil {
			t.Fatal(err)
		}
	}
	unavailable := func(label string) {
		t.Helper()
		messages := h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, h.conv.ID)
		calls := f.calls()
		rec := h.post("hello there")
		var body blockedBody
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != http.StatusServiceUnavailable || body.Code != "moderation_unavailable" {
			t.Fatalf("%s: %d %s", label, rec.Code, rec.Body)
		}
		if h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, h.conv.ID) != messages {
			t.Errorf("%s: a held-back message left rows", label)
		}
		if f.calls() != calls {
			t.Errorf("%s: the service was asked on settings that could not be read", label)
		}
	}

	// Stored block, one other value unreadable: the defaults (allow) must not
	// stand in for it.
	corrupt(settingModerationStrikes, `"many"`)
	unavailable("an unreadable strike limit")

	// The same with the on-error itself unreadable, and with an unreadable mode.
	corrupt(settingModerationStrikes, `3`)
	corrupt(settingModerationOnError, `"maybe"`)
	unavailable("an unreadable on-error")
	corrupt(settingModerationOnError, `"allow"`)
	corrupt(settingModerationMode, `"telepathy"`)
	unavailable("an unreadable mode")
	corrupt(settingModerationMode, `42`)
	unavailable("a mode that is not text")

	// Moderation known to be off needs none of the other settings.
	corrupt(settingModerationMode, `"off"`)
	corrupt(settingModerationCategories, `"all of them"`)
	rec := h.post("hello there")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("moderation off beside a damaged category list: %d %s", rec.Code, rec.Body)
	}
	var sent CreateMessageResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &sent)
	h.settle(sent.Run.Id)
}
