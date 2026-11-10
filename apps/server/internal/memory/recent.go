package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/words"
)

// The list of recent conversations a snapshot carries
// (docs/design/chat-history-plan.md, section 3): what the person said in their
// latest conversations, so that a new one can pick up "the thing from
// yesterday". Their own words, verbatim and shortened - no model writes or
// summarises any of it.

const (
	// SettingChatHistory is the app_settings key of the administrator's switch.
	SettingChatHistory = "chat_history"

	// DefaultTitle is what a conversation is called until it is titled; it says
	// nothing about the conversation and is never listed.
	DefaultTitle = "New chat"

	// MaxRecentChats is how many conversations the list names at most.
	MaxRecentChats = 40
	// recentMessageRunes is how long one of the person's messages may be in the
	// list, in characters; longer ones end in an ellipsis.
	recentMessageRunes = 160
	// recentFirstMessages and recentLastMessages are the messages listed of a
	// conversation: the first, and the last five (all of them up to six). What
	// lies between is left out without a mark.
	recentFirstMessages = 1
	recentLastMessages  = 5
	// recentBudgetRunes bounds the whole list, in characters of what the prompt
	// carries per conversation (RecentChat.cost): the newest conversations get
	// their messages, once those no longer fit the older ones are a line each,
	// and when not even that fits the list ends.
	recentBudgetRunes = 6000
	// recentTitleRunes cuts a title that is longer than a title should be.
	recentTitleRunes = 100
	// recentFetch is how many conversations the query reads: more than the list
	// names, because one whose messages are nothing but the composer's lines (a
	// skill picked, nothing typed) has no words to list and is dropped here.
	recentFetch = 2 * MaxRecentChats
)

// RecentChat is one conversation of the list.
type RecentChat struct {
	// Ref is the short id (c1, c2, ...) the chat_history tool refers to the
	// conversation by. Given when the list is taken and never renumbered: a
	// conversation removed later (Store.Resolve, RemoveFromRecentChats) leaves a
	// gap.
	Ref string    `json:"ref"`
	ID  uuid.UUID `json:"id"`
	// Title is empty for a conversation that has none worth showing: still the
	// default, or a title content moderation refused.
	Title string `json:"title,omitempty"`
	// At is the time of the person's last message in it. Kept with the zone it
	// was taken in, so that it prints the same way for as long as the snapshot
	// lives.
	At time.Time `json:"at"`
	// Messages are what the person said, shortened. Empty for a conversation
	// the budget left no room for (the line with its title still is there).
	Messages []string `json:"messages,omitempty"`
}

// heading is the line a conversation starts with: [c1] 2026-11-09 14:20 · title.
func (c RecentChat) heading() string {
	line := fmt.Sprintf("[%s] %s", c.Ref, c.At.Format("2006-01-02 15:04"))
	if c.Title != "" {
		line += " · " + c.Title
	}
	return line
}

// cost is what the conversation takes of the budget: its lines as the prompt
// prints them, with the blank line that follows.
func (c RecentChat) cost() int {
	n := utf8.RuneCountInString(c.heading()) + 2
	for _, m := range c.Messages {
		n += utf8.RuneCountInString(m) + 3 // "- " and the newline
	}
	return n
}

// recentCandidate is a conversation on its way into the list, its title perhaps
// still to be checked.
type recentCandidate struct {
	chat RecentChat
	// title is the title as it is kept, whole; the chat carries it shortened.
	title            string
	checked, blocked *string // the hashes recorded for it
	check            *candidate
}

// shorten is a message as the list carries it: on one line, at most
// recentMessageRunes characters.
func shorten(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= recentMessageRunes {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:recentMessageRunes-1])) + "…"
}

// pickMessages are the messages a conversation lists: the first and the last
// five of what the person said, all of them when that is six or fewer.
func pickMessages(said []string) []string {
	if len(said) <= recentFirstMessages+recentLastMessages {
		return said
	}
	return append([]string{said[0]}, said[len(said)-recentLastMessages:]...)
}

// fitBudget numbers the conversations and applies the budget. Newest first, as
// given. Once one conversation's messages do not fit, no older one gets any
// either: the older, the less detail, always.
func fitBudget(chats []RecentChat) []RecentChat {
	var (
		out       []RecentChat
		used      int
		headlines bool
	)
	for _, c := range chats {
		c.Ref = RecentRef(len(out) + 1)
		if headlines {
			c.Messages = nil
		}
		if used+c.cost() > recentBudgetRunes && len(c.Messages) > 0 {
			c.Messages, headlines = nil, true
		}
		if used+c.cost() > recentBudgetRunes {
			break
		}
		used += c.cost()
		out = append(out, c)
	}
	return out
}

// RecentRef is the short id the nth conversation of a list is known by (from 1).
func RecentRef(n int) string { return fmt.Sprintf("c%d", n) }

// ChatHistoryOn says whether referring to earlier conversations is on for a
// person: the administrator's switch (on unless turned off) and their own.
func (s *Store) ChatHistoryOn(ctx context.Context, userID uuid.UUID) (bool, error) {
	on, err := s.setting(ctx, SettingChatHistory, true)
	if err != nil || !on {
		return false, err
	}
	p, err := s.q.GetPersonalization(ctx, userID)
	if err != nil {
		return false, err
	}
	return p.ChatHistory, nil
}

// ChatHistoryGlobalOn is the administrator's switch alone.
func (s *Store) ChatHistoryGlobalOn(ctx context.Context) (bool, error) {
	return s.setting(ctx, SettingChatHistory, true)
}

// ChatHistoryAvailable says whether earlier conversations may be used in this
// one now, for the list in its prompt and for the chat_history tool: the
// switches are on for its owner and the conversation is not temporary. Decided
// per request from the live state, as ToolAvailable is, not from the snapshot.
func (s *Store) ChatHistoryAvailable(ctx context.Context, userID, conversationID uuid.UUID) (bool, error) {
	state, err := s.q.GetConversationMemoryState(ctx, sqlc.GetConversationMemoryStateParams{ID: conversationID, UserID: &userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil || state.Temporary {
		return false, err
	}
	return s.ChatHistoryOn(ctx, userID)
}

// ResolveRecent is the conversation a ref of the list (c3) stands for in this
// conversation's snapshot. Refs of any other conversation's list, and
// conversations that were removed from this one, mean nothing here.
func (s *Store) ResolveRecent(ctx context.Context, userID, conversationID uuid.UUID, ref string) (uuid.UUID, error) {
	state, err := s.q.GetConversationMemoryState(ctx, sqlc.GetConversationMemoryStateParams{ID: conversationID, UserID: &userID})
	if err != nil {
		return uuid.Nil, ErrUnknownRef
	}
	snap, err := decodeSnapshot(state.MemorySnapshot)
	if err != nil {
		return uuid.Nil, err
	}
	ref = strings.ToLower(strings.TrimSpace(ref))
	for _, c := range snap.Recent {
		if c.Ref == ref {
			return c.ID, nil
		}
	}
	return uuid.Nil, ErrUnknownRef
}

// TitleChecked says whether a conversation's title may be shown to the model:
// it is not the default, and it was written by the server or checked by content
// moderation and passed (the recorded hash is the title's own). The title is
// empty when it may not. A conversation that is not the person's is not found.
// Results of the chat_history tool show a title only through this.
func (s *Store) TitleChecked(ctx context.Context, userID, conversationID uuid.UUID) (title string, ok bool, err error) {
	row, err := s.q.GetConversationTitleCheck(ctx, sqlc.GetConversationTitleCheckParams{ID: conversationID, UserID: &userID})
	if err != nil {
		return "", false, err
	}
	if row.Title == DefaultTitle || strings.TrimSpace(row.Title) == "" {
		return "", false, nil
	}
	if row.TitleCheckedHash == nil || *row.TitleCheckedHash != Hash(row.Title) {
		return "", false, nil
	}
	return row.Title, true, nil
}

// recentCandidates reads the person's recent conversations for a new snapshot,
// numbered and within the budget. Their titles are not decided yet.
func (s *Store) recentCandidates(ctx context.Context, userID, current uuid.UUID) ([]*recentCandidate, error) {
	rows, err := s.q.ListRecentChatCandidates(ctx, sqlc.ListRecentChatCandidatesParams{
		UserID: &userID, CurrentID: current, RowLimit: recentFetch,
	})
	if err != nil {
		return nil, err
	}
	var (
		out  []*recentCandidate
		said []string
		last uuid.UUID
	)
	flush := func() {
		if n := len(out); n > 0 {
			out[n-1].chat.Messages = pickMessages(said)
		}
		said = nil
	}
	for _, row := range rows {
		if len(out) == 0 || row.ID != last {
			flush()
			last = row.ID
			c := &recentCandidate{chat: RecentChat{ID: row.ID, At: row.LastAt.Time.In(time.Local)}}
			if title := shortTitle(row.Title); title != "" && title != DefaultTitle {
				c.chat.Title, c.title, c.checked, c.blocked = title, row.Title, row.TitleCheckedHash, row.TitleBlockedHash
			}
			out = append(out, c)
		}
		if text := shorten(words.Typed(row.Content)); text != "" {
			said = append(said, text)
		}
	}
	flush()

	// A conversation with nothing the person typed has nothing to list.
	var chats []RecentChat
	kept := out[:0]
	for _, c := range out {
		if len(c.chat.Messages) > 0 && len(kept) < MaxRecentChats {
			kept = append(kept, c)
			chats = append(chats, c.chat)
		}
	}
	// Numbered and fitted with the titles as they are; one that is refused
	// afterwards only makes its line shorter.
	fitted := fitBudget(chats)
	kept = kept[:len(fitted)]
	for i := range kept {
		kept[i].chat = fitted[i]
	}
	return kept, nil
}

func shortTitle(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	if utf8.RuneCountInString(title) > recentTitleRunes {
		title = strings.TrimSpace(string([]rune(title)[:recentTitleRunes-1])) + "…"
	}
	return title
}

// recentList is the list as a snapshot keeps it: a title that was refused, or
// was never checked and could not be, taken off.
func recentList(candidates []*recentCandidate) []RecentChat {
	chats := make([]RecentChat, len(candidates))
	for i, c := range candidates {
		chats[i] = c.chat
		if c.check != nil && c.check.out {
			chats[i].Title = ""
		}
	}
	return chats
}

// recordTitleChecks writes down what a snapshot-time check found about titles,
// best effort like recordChecks.
func (s *Store) recordTitleChecks(ctx context.Context, userID uuid.UUID, candidates []*recentCandidate) {
	var (
		ids      []uuid.UUID
		hashes   []string
		verdicts []string
	)
	for _, c := range candidates {
		if c.check != nil && c.check.found != "" {
			ids, hashes, verdicts = append(ids, c.chat.ID), append(hashes, Hash(c.title)), append(verdicts, c.check.found)
		}
	}
	if len(ids) == 0 {
		return
	}
	if err := s.q.MarkTitlesModerated(ctx, sqlc.MarkTitlesModeratedParams{
		UserID: &userID, Ids: ids, Hashes: hashes, Verdicts: verdicts,
	}); err != nil {
		slog.WarnContext(ctx, "record the titles' moderation", "error", err, "user_id", userID)
	}
}
