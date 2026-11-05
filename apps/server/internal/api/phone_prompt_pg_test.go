package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
)

// A run started from the phone app (a request signed in with a bearer token)
// remembers it, and its message tells the model the answer is read on a
// narrow screen; a browser's run says nothing of the kind. The note stays on
// that message in later requests, from wherever they come: a history that
// changed between runs would invalidate the thinking blocks sent back.
func TestARunFromThePhoneAppIsLaidOutForIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fromApp bool
	}{{"browser", false}, {"phone app", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newAskHarness(t, openAIWire, func(int) string { return openAIWire.text("好的。") })
			h.server.runner.SetTurnNote(h.server.TurnNote)
			send := func(content string, fromApp bool) {
				r := h.request(http.MethodPost, fmt.Sprintf(`{"content":%q,"model":%q}`, content, h.model))
				r = r.WithContext(auth.WithFromApp(r.Context(), fromApp))
				rec := httptest.NewRecorder()
				h.server.CreateMessage(rec, r, h.conv.ID)
				var out CreateMessageResponse
				h.accepted(rec, &out)
				h.settle(out.Run.Id)
				run, err := h.q.GetRun(context.Background(), out.Run.Id)
				if err != nil {
					t.Fatal(err)
				}
				if run.FromApp != fromApp {
					t.Errorf("run.from_app = %v, want %v", run.FromApp, fromApp)
				}
			}
			last := func(word string) string {
				h.mu.Lock()
				defer h.mu.Unlock()
				for i := len(h.bodies) - 1; i >= 0; i-- {
					if strings.Contains(h.bodies[i], word) {
						return h.bodies[i]
					}
				}
				t.Fatalf("no request carried %q", word)
				return ""
			}

			send("hello", tc.fromApp)
			if got := strings.Count(last("hello"), "sent from the OpenChat phone app"); got != map[bool]int{false: 0, true: 1}[tc.fromApp] {
				t.Errorf("phone notes in the first request = %d", got)
			}
			// The next message comes from a browser; the first keeps its note.
			send("and then", false)
			if got := strings.Count(last("and then"), "sent from the OpenChat phone app"); got != map[bool]int{false: 0, true: 1}[tc.fromApp] {
				t.Errorf("phone notes in the second request = %d", got)
			}
		})
	}
}
