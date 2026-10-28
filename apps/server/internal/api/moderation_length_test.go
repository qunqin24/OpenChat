package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// A message is checked whole, however long: the chat request carries all of it,
// so whatever the check leaves out is what a marker is hidden in.

func TestSplitForModeration(t *testing.T) {
	const size = 2_000
	if got := splitForModeration("short", size); len(got) != 1 || got[0] != "short" {
		t.Errorf("%q", got)
	}

	// A marker anywhere - in the first piece, on a cut, in the middle, at the very
	// end - is whole in at least one piece, with no whitespace to cut at.
	const marker = "<<A MARKER OF SOME LENGTH>>"
	base := strings.Repeat("a", 30_000)
	for at := 0; at+len(marker) <= len(base); at += 97 {
		text := base[:at] + marker + base[at+len(marker):]
		found := false
		for _, piece := range splitForModeration(text, size) {
			if utf8.RuneCountInString(piece) > size {
				t.Fatalf("a piece of %d characters", utf8.RuneCountInString(piece))
			}
			found = found || strings.Contains(piece, marker)
		}
		if !found {
			t.Fatalf("a marker at %d is in no piece", at)
		}
	}

	// Nothing is lost at the ends, and a multi-byte character is never cut.
	text := strings.Repeat("好", 9_001)
	pieces := splitForModeration(text, size)
	if len(pieces) < 5 || !strings.HasPrefix(text, pieces[0]) || !strings.HasSuffix(text, pieces[len(pieces)-1]) {
		t.Errorf("%d pieces", len(pieces))
	}
	for _, piece := range pieces {
		if !utf8.ValidString(piece) {
			t.Fatal("a piece is not valid text")
		}
	}

	// A cut is moved back to a break when there is one near it, and a stretch of
	// nothing but whitespace is not a piece.
	words := strings.Repeat("word ", 1_000)
	for _, piece := range splitForModeration(words, size)[:2] {
		if !strings.HasSuffix(piece, " ") {
			t.Errorf("a cut through a word: %q", piece[len(piece)-8:])
		}
	}
	blank := "start" + strings.Repeat(" ", 20_000) + "end"
	got := splitForModeration(blank, size)
	for _, piece := range got {
		if strings.TrimSpace(piece) == "" {
			t.Error("a piece of nothing")
		}
	}
	if joined := strings.Join(got, ""); !strings.Contains(joined, "start") || !strings.Contains(joined, "end") {
		t.Errorf("the ends were lost: %d pieces", len(got))
	}
}

// What goes to OpenAI for a long text: every piece of it, in as few requests as
// the endpoint takes, and a result for each piece - fewer results than pieces is
// a check that did not happen.
func TestModerateWithOpenAISendsTheWholeText(t *testing.T) {
	var mu sync.Mutex
	var requests [][]string
	short := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, "plain strings only", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, req.Input)
		answers := len(req.Input)
		if short {
			answers--
		}
		mu.Unlock()
		results := make([]any, answers)
		for i, piece := range req.Input[:answers] {
			categories, scores := map[string]bool{}, map[string]float64{}
			for _, c := range moderationCategories {
				categories[string(c)] = strings.Contains(piece, "HIDDEN") && c == ModerationCategoryViolenceGraphic
				scores[string(c)] = 0
			}
			results[i] = map[string]any{"categories": categories, "category_scores": scores}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer srv.Close()
	s := &Server{moderationClient: srv.Client()}
	cfg := defaultModerationConfig()
	cfg.OpenAIKey, cfg.OpenAIBaseURL = "sk-secret-key", srv.URL

	// 250,000 characters with the marker in the middle.
	text := strings.Repeat("a", 125_000) + "HIDDEN" + strings.Repeat("b", 125_000)
	verdict, err := s.moderateWithOpenAI(context.Background(), cfg, text, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.flagged["violence/graphic"] {
		t.Error("a marker in the middle of the text was not found")
	}
	// Found in the first request: the rest is not asked about.
	if len(requests) != 1 || len(requests[0]) != moderationItemsPerRequest {
		t.Errorf("%d requests", len(requests))
	}

	// Clean, the whole text goes: every character of it is in some piece, and
	// 22 pieces take two requests.
	requests = nil
	clean := strings.Repeat("a", 125_000) + "hidden" + strings.Repeat("b", 125_000)
	if verdict, err = s.moderateWithOpenAI(context.Background(), cfg, clean, nil); err != nil || len(cfg.blocked(verdict)) != 0 {
		t.Fatalf("%v %v", verdict, err)
	}
	var pieces int
	for _, request := range requests {
		pieces += len(request)
	}
	if len(requests) != 2 || pieces != len(splitForModeration(clean, moderationChunkRunes)) {
		t.Errorf("%d requests carrying %d pieces", len(requests), pieces)
	}
	if !strings.Contains(strings.Join(requests[0], ""), "aaaa") || !strings.Contains(strings.Join(requests[1], ""), "bbbb") {
		t.Error("the text is not all there")
	}

	// A result missing for a piece fails the check.
	short = true
	if _, err := s.moderateWithOpenAI(context.Background(), cfg, clean, nil); err == nil {
		t.Error("fewer results than pieces was accepted")
	}
}

// The tests below run a message through the handlers.

// A marker in the middle of a long message is found, in either mode, whether
// the settings say to block or to allow when the service fails: it is not a
// failure, the message was read.
func TestLongMessageIsCheckedInTheMiddle(t *testing.T) {
	filler := strings.Repeat("filler words ", 3_500) // 45,500 characters
	long := filler + " BLOCKME " + filler

	t.Run("openai", func(t *testing.T) {
		h, f := newModerationHarness(t, `,"onError":"block","strikes":0`)
		if got := refused(t, h.post(long)); strings.Join(got, ",") != "violence/graphic" {
			t.Errorf("categories %v", got)
		}
		if f.calls() == 0 {
			t.Error("the service was not asked")
		}
		// Nowhere else either: in the first words, the last, and with no breaks.
		for _, text := range []string{"BLOCKME " + filler + filler, filler + filler + " BLOCKME", strings.Repeat("a", 90_000) + "BLOCKME" + strings.Repeat("a", 90_000)} {
			refused(t, h.post(text))
		}
	})

	t.Run("model", func(t *testing.T) {
		var h *askHarness
		h = newAskHarness(t, openAIWire, func(n int) string {
			h.mu.Lock()
			body := h.bodies[n-1]
			h.mu.Unlock()
			if strings.Contains(body, "content-safety classifier") {
				if strings.Contains(body, "BLOCKME") {
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
		if rec := h.patchModeration(fmt.Sprintf(`{"mode":"model","model":%q,"onError":"block"}`, h.model)); rec.Code != http.StatusOK {
			t.Fatalf("configure: %d %s", rec.Code, rec.Body)
		}
		before := h.chatRequests()
		if got := refused(t, h.post(long)); strings.Join(got, ",") != "violence/graphic" {
			t.Errorf("categories %v", got)
		}
		// 91,000 characters are four pieces and the marker is in the second; each
		// is a request of the model, metered (the found marker ends the reading).
		var n int
		if err := h.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM usage_logs WHERE kind = 'moderation' AND user_id = $1`, h.user.ID).Scan(&n); err != nil || n < 2 || n > 3 {
			t.Errorf("%d moderation requests (%v), want 2 or 3", n, err)
		}
		if h.chatRequests()-before != n {
			t.Errorf("the chat model was asked as well")
		}
	})
}

// Past what a check reads a message is refused (413 message_too_large_for_moderation),
// not partly checked, and the on-error setting does not let it by - nor does it
// the model that would need more requests than a check makes: the size is the
// sender's, and "too big to check" is not "the service failed".
func TestOverlongMessageIsNotPartlyChecked(t *testing.T) {
	ctx := context.Background()

	t.Run("openai", func(t *testing.T) {
		h, f := newModerationHarness(t, `,"onError":"allow"`)
		who := moderationSubject{userID: h.user.ID, source: ModerationSourceSend}
		long := strings.Repeat("a ", moderationMaxRunes/2+1)
		if err := h.server.moderate(ctx, who, moderationInput{text: long}); err != errMessageTooLarge {
			t.Fatalf("one character past the limit, on_error allow: %v", err)
		}
		if f.calls() != 0 {
			t.Error("part of a message too long to check was sent for a check")
		}
		// The limit itself is checked, whole.
		if err := h.server.moderate(ctx, who, moderationInput{text: strings.Repeat("a ", moderationMaxRunes/2)}); err != nil {
			t.Fatalf("a message of the longest length: %v", err)
		}
		if f.calls() != 3 {
			t.Errorf("%d requests for 400,000 characters", f.calls())
		}
		// Through the handler, the answer is the 413 and nothing is written.
		messages := h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, h.conv.ID)
		rec := h.post(long)
		var body blockedBody
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != http.StatusRequestEntityTooLarge || body.Code != "message_too_large_for_moderation" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		if h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, h.conv.ID) != messages {
			t.Error("a refused message left rows")
		}
		// An administrator exempted from the check sends what they like.
		if rec := h.patchModeration(`{"exemptAdmins":true}`); rec.Code != http.StatusOK {
			t.Fatal(rec.Body)
		}
		if err := h.server.moderate(ctx, moderationSubject{userID: h.user.ID, admin: true, source: ModerationSourceSend}, moderationInput{text: long}); err != nil {
			t.Errorf("an exempt administrator: %v", err)
		}
	})

	t.Run("model", func(t *testing.T) {
		h := newAskHarness(t, openAIWire, func(int) string { return openAIWire.text(classifierAnswer()) })
		resetModeration(t, h)
		h.server.usage = h.server.runner.Recorder()
		h.server.endUsers = NewEndUsers(h.pool)
		t.Cleanup(func() {
			_, _ = h.pool.Exec(context.Background(), `DELETE FROM usage_logs WHERE user_id = $1`, h.user.ID)
		})
		if rec := h.patchModeration(fmt.Sprintf(`{"mode":"model","model":%q,"onError":"allow"}`, h.model)); rec.Code != http.StatusOK {
			t.Fatalf("configure: %d %s", rec.Code, rec.Body)
		}
		who := moderationSubject{userID: h.user.ID, source: ModerationSourceSend}

		// Eight pieces' worth of text is classified; more than that cannot be.
		fits := strings.Repeat("a", 180_000)
		tooMany := strings.Repeat("a", 200_000)
		if got := len(splitForModeration(fits, moderationModelChunkRunes)); got > moderationModelMaxChunks {
			t.Fatalf("%d pieces for the text that should fit", got)
		}
		if got := len(splitForModeration(tooMany, moderationModelChunkRunes)); got <= moderationModelMaxChunks {
			t.Fatalf("%d pieces for the text that should not fit", got)
		}
		if err := h.server.moderate(ctx, who, moderationInput{text: fits}); err != nil {
			t.Fatalf("a text that fits: %v", err)
		}
		asked := h.chatRequests()
		if asked != len(splitForModeration(fits, moderationModelChunkRunes)) {
			t.Errorf("%d classifier requests", asked)
		}

		// More than a check makes is refused, not passed: on_error allow does
		// not let an account's through (no model was asked), nor does block turn
		// it into an outage.
		if err := h.server.moderate(ctx, who, moderationInput{text: tooMany}); err != errMessageTooLarge {
			t.Errorf("too many pieces, on_error allow: %v", err)
		}
		if rec := h.patchModeration(`{"onError":"block"}`); rec.Code != http.StatusOK {
			t.Fatal(rec.Body)
		}
		if err := h.server.moderate(ctx, who, moderationInput{text: tooMany}); err != errMessageTooLarge {
			t.Errorf("too many pieces, on_error block: %v", err)
		}
		if h.chatRequests() != asked {
			t.Error("the model was asked about a text that cannot be classified whole")
		}
		// And the absolute limit holds in this mode too, whatever on_error says.
		if rec := h.patchModeration(`{"onError":"allow"}`); rec.Code != http.StatusOK {
			t.Fatal(rec.Body)
		}
		if err := h.server.moderate(ctx, who, moderationInput{text: strings.Repeat("a", moderationMaxRunes+1)}); err != errMessageTooLarge {
			t.Errorf("past the limit: %v", err)
		}
	})
}
