package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// guestIDs keeps a guest's memories apart from other requests' and gives the
// browser back the ids it chose.
//
// The memories of a request are rows of user_memories under the request's
// transient user, and their ids are a table-wide key. The browser's own ids
// cannot be the rows' ids: two requests from one browser (different
// conversations, the same list of memories) would insert the same ids, and the
// second would be refused as if its conversation were busy. So every id the
// browser sends for a memory (in the list, and in the snapshot the
// conversation was started with) is staged under a fresh id of this request's
// own, and everything that leaves the server for the browser is turned back:
// the browser sees exactly the ids it sent. A memory the model creates during
// the run has no browser id; it keeps the one the server gave it, which is a
// uuid the browser stores and sends back next time like any other, and is
// staged under yet another id in that request.
//
// The ids are replaced in the encoded frames rather than in each type that can
// carry one: the last frame (snapshot, memories, memory events) and the
// memory tool's result details (streamed in a batch, held in a row, repeated
// in a resync snapshot) all name them, and a new field that names a memory
// should not leak unless someone remembers to translate it too. A uuid is 36
// characters of hex and dashes, which JSON never escapes, and one that is not
// an id of this request is not touched.
type guestIDs struct {
	staged  map[uuid.UUID]uuid.UUID // browser id -> staged id
	browser map[uuid.UUID]uuid.UUID // staged id -> browser id
	back    *strings.Replacer       // staged id -> browser id, in encoded text
}

func newGuestIDs() *guestIDs {
	return &guestIDs{staged: map[uuid.UUID]uuid.UUID{}, browser: map[uuid.UUID]uuid.UUID{}}
}

// server is the id the browser's memory is staged under: the same one every
// time it is asked for during the request. Planning only; seal ends it. The
// ids are version 7, so they ascend in the order they are first asked for: the
// memories are written at one instant and listed by id (ListMemories), so the
// list keeps the order the browser sent it in.
func (g *guestIDs) server(browserID uuid.UUID) uuid.UUID {
	if id, ok := g.staged[browserID]; ok {
		return id
	}
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	g.staged[browserID] = id
	g.browser[id] = browserID
	return id
}

// seal ends the planning: no more ids are staged, and what leaves is rewritten.
func (g *guestIDs) seal() {
	pairs := make([]string, 0, 2*len(g.browser))
	for staged, browser := range g.browser {
		if staged != browser {
			pairs = append(pairs, staged.String(), browser.String())
		}
	}
	if len(pairs) > 0 {
		g.back = strings.NewReplacer(pairs...)
	}
}

// text turns the staged ids in an encoded text back into the browser's.
func (g *guestIDs) text(s string) string {
	if g == nil || g.back == nil {
		return s
	}
	return g.back.Replace(s)
}

// forBrowser is a message as the browser will hold it: the ids turned back, the
// way send writes it. A receipt has to be made from this form and no other -
// the browser stores what it was sent and returns exactly that, and a receipt
// for the staged ids would be for a row it never had (a memory tool's result
// names the memory it changed, and that memory has the browser's id there).
func (g *guestIDs) forBrowser(m Message) Message {
	if g == nil || g.back == nil {
		return m
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var out Message
	if json.Unmarshal([]byte(g.back.Replace(string(raw))), &out) != nil {
		return m
	}
	return out
}

// send writes a frame to the browser with the ids turned back.
func (g *guestIDs) send(sse *httpx.SSEWriter, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("sse: marshal event: %w", err)
	}
	if g.back != nil {
		payload = []byte(g.back.Replace(string(payload)))
	}
	return sse.Frame(payload)
}
