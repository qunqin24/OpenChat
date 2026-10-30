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
// remembers it, and its system prompt tells the model the answer is read on a
// narrow screen; a browser's run says nothing of the kind.
func TestARunFromThePhoneAppIsLaidOutForIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fromApp bool
	}{{"browser", false}, {"phone app", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newAskHarness(t, openAIWire, func(int) string { return openAIWire.text("好的。") })
			r := h.request(http.MethodPost, fmt.Sprintf(`{"content":"hello","model":%q}`, h.model))
			r = r.WithContext(auth.WithFromApp(r.Context(), tc.fromApp))
			rec := httptest.NewRecorder()
			h.server.CreateMessage(rec, r, h.conv.ID)
			var out CreateMessageResponse
			h.accepted(rec, &out)
			h.settle(out.Run.Id)

			run, err := h.q.GetRun(context.Background(), out.Run.Id)
			if err != nil {
				t.Fatal(err)
			}
			if run.FromApp != tc.fromApp {
				t.Errorf("run.from_app = %v, want %v", run.FromApp, tc.fromApp)
			}
			h.mu.Lock()
			body := h.bodies[0]
			h.mu.Unlock()
			if got := strings.Contains(body, "Reading on a phone"); got != tc.fromApp {
				t.Errorf("phone guide in the system prompt = %v, want %v", got, tc.fromApp)
			}
		})
	}
}
