package api

import (
	"net/http"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// searchMaxRunes bounds what is searched for: a pasted paragraph is not a
// search, and the pattern is matched against every message.
const searchMaxRunes = 200

// How much of a matching message is shown either side of the match: enough
// to recognise the passage on one line of the palette.
const (
	excerptBefore = 24
	excerptAfter  = 96
)

// searchConversations is ListConversations narrowed to what contains the
// search, in the title or anywhere in what was said, paged the same way.
//
// A substring match rather than full-text search: Postgres's text search
// splits words on spaces, which Chinese does not have, and a person looking for
// a conversation types a fragment they remember, not a stemmed word. It scans
// only the caller's own conversations; if that stops being cheap, the answer is
// a pg_trgm index on messages.content.
func (s *Server) searchConversations(w http.ResponseWriter, r *http.Request, userID uuid.UUID, search string, limit int32, cursorAt pgtype.Timestamptz, cursorID *uuid.UUID) {
	if runes := []rune(search); len(runes) > searchMaxRunes {
		search = string(runes[:searchMaxRunes])
	}
	rows, err := s.q.SearchConversations(r.Context(), sqlc.SearchConversationsParams{
		UserID:          &userID,
		Pattern:         "%" + likeEscaper.Replace(search) + "%",
		CursorUpdatedAt: cursorAt,
		CursorID:        cursorID,
		RowLimit:        limit + 1,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	page := ConversationPage{Items: make([]Conversation, 0, limit)}
	if int32(len(rows)) > limit {
		last := rows[limit-1]
		next := encodeCursor(last.UpdatedAt, last.ID)
		page.NextCursor = &next
		rows = rows[:limit]
	}
	for _, row := range rows {
		conv := toConversation(sqlc.Conversation{
			ID: row.ID, UserID: row.UserID, Title: row.Title, Model: row.Model,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt,
			ReasoningEffort: row.ReasoningEffort, WebSearch: row.WebSearch,
			MemorySnapshot: row.MemorySnapshot, Temporary: row.Temporary, CurrentInputID: row.CurrentInputID,
		})
		if id, err := uuid.Parse(row.HitID); err == nil {
			conv.Match = &ConversationMatch{
				MessageId: id,
				Role:      ConversationMatchRole(row.HitRole),
				Excerpt:   excerpt(row.HitContent, search),
			}
		}
		page.Items = append(page.Items, conv)
	}
	httpx.JSON(w, http.StatusOK, page)
}

// likeEscaper makes a search literal inside an ILIKE pattern, whose default
// escape character is the backslash.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// excerpt is text on one line, cut to the words around the first occurrence
// of needle, ignoring case. Where the database matched in a way this does not
// (a case rule of its collation), the excerpt is the message's start.
func excerpt(text, needle string) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	at := indexFold(runes, []rune(strings.Join(strings.Fields(needle), " ")))
	if at < 0 {
		at = 0
	}
	start, end := max(0, at-excerptBefore), min(len(runes), at+len([]rune(needle))+excerptAfter)
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

// indexFold finds needle in haystack rune by rune, ignoring case, so the
// position counts runes of the original text.
func indexFold(haystack, needle []rune) int {
	if len(needle) == 0 {
		return -1
	}
outer:
	for i := 0; i+len(needle) <= len(haystack); i++ {
		for j, r := range needle {
			if unicode.ToLower(haystack[i+j]) != unicode.ToLower(r) {
				continue outer
			}
		}
		return i
	}
	return -1
}
