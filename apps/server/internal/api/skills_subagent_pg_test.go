package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// A sub-agent opens the skills its launch named, from the main conversation's
// list, and no others; a name the main agent cannot open itself is refused.
func TestSubagentGetsTheSkillsItsLaunchNamed(t *testing.T) {
	f := newSubFixture(t)
	tag := skillTag()
	given, withheld := "research-notes-"+tag, "private-diary-"+tag
	for _, body := range []string{
		skillBody(given, "How to take research notes.", "GIVEN-SKILL-BODY", nil),
		skillBody(withheld, "Not for sub-agents.", "WITHHELD-BODY", nil),
	} {
		if rec := f.call(http.MethodPost, body, func(w http.ResponseWriter, r *http.Request) { f.server.CreateMySkill(w, r) }); rec.Code != http.StatusCreated {
			t.Fatalf("create skill: %d %s", rec.Code, rec.Body)
		}
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), "DELETE FROM skills WHERE owner_id = $1", f.user.ID) })

	conv := f.newConversation()
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		switch r.kind() {
		case "sub":
			if r.toolResults() == 0 {
				return fakeTurn{Calls: []fakeCall{{Name: tools.ToolSkill, Args: map[string]any{"op": "view", "name": given}}}}
			}
			return fakeTurn{Text: "REPORT done"}
		}
		switch r.toolResults() {
		case 0:
			call := launch("笔记", "Take notes", false)
			call.Args["skills"] = []string{"no-such-skill"}
			return fakeTurn{Calls: []fakeCall{call}}
		case 1:
			call := launch("笔记", "Take notes", false)
			call.Args["skills"] = []string{given}
			return fakeTurn{Calls: []fakeCall{call}}
		}
		return fakeTurn{Text: "The answer."}
	})
	f.send(conv, "take notes on this")
	f.settled(conv)

	mains := f.requests("main")
	if len(mains) < 2 || !strings.Contains(mains[1].all(), `no skill named "no-such-skill"`) {
		t.Fatalf("an unknown skill was not refused: %+v", mains)
	}
	subs := f.requests("sub")
	if len(subs) != 2 {
		t.Fatalf("sub-agent requests = %d", len(subs))
	}
	if !strings.Contains(subs[0].System, "- "+given+": How to take research notes.") || strings.Contains(subs[0].System, withheld) {
		t.Fatalf("the sub-agent's skills section:\n%s", subs[0].System)
	}
	offered := false
	for _, tool := range subs[0].Tools {
		offered = offered || tool == tools.ToolSkill
	}
	if !offered {
		t.Fatalf("the sub-agent was not offered the skill tool: %v", subs[0].Tools)
	}
	if !strings.Contains(subs[1].all(), "GIVEN-SKILL-BODY") {
		t.Fatalf("the sub-agent did not read its skill: %s", subs[1].all())
	}
}
