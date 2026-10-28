// Package memory is what OpenChat remembers about a person between
// conversations: two fields of custom instructions, and a short list of notes
// that the person writes — or the model, with its memory tool.
//
// The prompt cache is why a conversation does not read the list afresh at every
// request. A conversation takes a Snapshot of both at its first run and builds
// every system prompt from that, so the prompt is the same bytes on every turn
// (see Snapshot). The list itself changes at once and reaches the next
// conversation.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

const (
	// MaxContent is the length of one memory, in characters. A note is one
	// sentence; anything longer is an essay the model would carry in every
	// request.
	MaxContent = 200
	// MaxMemories caps the list. When it is full nothing is evicted: the model
	// is told to update or delete, and the page says it is full.
	MaxMemories = 100
	// MaxInstructions is the length of each custom-instructions field.
	MaxInstructions = 1500
	// EventRetention is how long the log of what the model changed is kept.
	EventRetention = 30 * 24 * time.Hour

	// SettingEnabled is the app_settings key of the administrator's switch.
	SettingEnabled = "memory"
)

// Errors a caller tells apart. The ones that are the person's or the model's to
// act on say so in their text.
var (
	ErrEmpty     = errors.New("a memory cannot be empty")
	ErrTooLong   = fmt.Errorf("a memory is at most %d characters", MaxContent)
	ErrFull      = fmt.Errorf("the memory list is full (%d)", MaxMemories)
	ErrDuplicate = errors.New("that is already remembered")
	ErrNotFound  = errors.New("no such memory")
	// ErrChanged is an undo of something that was edited since.
	ErrChanged    = errors.New("the memory was changed since")
	ErrUnknownRef = errors.New("no such memory in this conversation's list")
	ErrUndone     = errors.New("already undone")
)

// Source is who wrote a memory or made a change.
type Source = sqlc.MemorySource

const (
	SourceUser  = sqlc.MemorySourceUser
	SourceModel = sqlc.MemorySourceModel
	SourceAuto  = sqlc.MemorySourceAuto
)

// Store reads and writes a person's memory.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: sqlc.New(pool)} }

// Clean is a memory as it is kept: whitespace collapsed to single spaces — a
// note is one line — and checked for length. The error says what is wrong.
func Clean(raw string) (string, error) {
	content := strings.Join(strings.Fields(raw), " ")
	switch {
	case content == "":
		return "", ErrEmpty
	case utf8.RuneCountInString(content) > MaxContent:
		return "", ErrTooLong
	}
	return content, nil
}

// Enabled says whether memory is on for a person: the administrator's switch
// (on unless turned off) and the person's own.
func (s *Store) Enabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	on, err := s.globalOn(ctx)
	if err != nil || !on {
		return false, err
	}
	p, err := s.q.GetPersonalization(ctx, userID)
	if err != nil {
		return false, err
	}
	return p.MemoryEnabled, nil
}

// GlobalOn is the administrator's switch alone.
func (s *Store) GlobalOn(ctx context.Context) (bool, error) { return s.globalOn(ctx) }

func (s *Store) globalOn(ctx context.Context) (bool, error) {
	raw, err := s.q.GetSetting(ctx, SettingEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	var on bool
	if err := json.Unmarshal(raw, &on); err != nil {
		return true, err
	}
	return on, nil
}

// ToolAvailable says whether a run of this conversation is offered the memory
// tool: memory is on for its owner, and the conversation is not temporary.
func (s *Store) ToolAvailable(ctx context.Context, userID, conversationID uuid.UUID) (bool, error) {
	state, err := s.q.GetConversationMemoryState(ctx, sqlc.GetConversationMemoryStateParams{ID: conversationID, UserID: &userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil || state.Temporary {
		return false, err
	}
	return s.Enabled(ctx, userID)
}

// ------------------------------------------------------------------ snapshot

// Snapshot is what a conversation was started with.
type Snapshot struct {
	// About and Style are the person's two custom-instructions fields.
	About string `json:"about,omitempty"`
	Style string `json:"style,omitempty"`
	// Memories are the list as it was, each under a short id for the memory tool
	// to refer to. Empty for a temporary conversation and when memory was off.
	Memories []SnapshotMemory `json:"memories,omitempty"`
	// Added are the memories saved during the conversation. They are not in the
	// prompt — it must not change — but the model may update or delete what it
	// saved itself, so their ids resolve.
	Added []SnapshotRef `json:"added,omitempty"`
}

type SnapshotMemory struct {
	Ref     string    `json:"ref"`
	ID      uuid.UUID `json:"id"`
	Content string    `json:"content"`
}

type SnapshotRef struct {
	Ref string    `json:"ref"`
	ID  uuid.UUID `json:"id"`
}

// Ref is the short id the nth memory of a conversation is known by (from 1):
// the notes of the snapshot first, in order, then those saved since.
func Ref(n int) string { return fmt.Sprintf("m%d", n) }

// Hash is how a text is remembered as checked or refused by content
// moderation: the hex SHA-256 of its UTF-8 bytes. Editing a text changes it,
// which is all it takes for the text to count as unchecked again.
func Hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// PieceKind says which of a person's texts a Piece is.
type PieceKind string

const (
	PieceAbout  PieceKind = "about"
	PieceStyle  PieceKind = "style"
	PieceMemory PieceKind = "memory"
)

// Piece is one text a snapshot would carry that nobody has checked yet: one of
// the two instruction fields, or a memory (ID).
type Piece struct {
	Kind PieceKind
	ID   uuid.UUID
	Text string
}

// Verdict is what a Vet decided about the Piece at the same index. Checked is
// whether the text really was looked at: false when moderation is off, the
// person is exempt, or the check could not be made and was allowed to go on -
// the text goes in either way, but it is not recorded as checked, so the next
// snapshot asks again. Blocked texts are left out of the snapshot.
type Verdict struct {
	Blocked bool
	Checked bool
}

// Vet decides about the pieces of a snapshot that have not been checked. It is
// asked once for all of them, before the snapshot is written; an error means
// the snapshot is not taken (content moderation could not be asked and the
// settings say not to go on without it).
type Vet func(ctx context.Context, pieces []Piece) ([]Verdict, error)

// candidate is a text on its way into a snapshot.
type candidate struct {
	piece Piece
	// checked and blocked are the hashes recorded for the text.
	checked, blocked *string
	out              bool   // left out of the snapshot
	found            string // what a check made just now found: "ok", "blocked" or ""
}

// EnsureSnapshot is the conversation's snapshot, taken now if it has none.
// Taken once: concurrent first requests agree on one, and every later call
// returns it unchanged.
//
// Taking it is also where what the person wrote is moderated, if it was not
// when it was saved - before moderation was turned on, or while it was off: of
// the instructions and memories that go in, those whose hash is not the one
// recorded as checked are handed to vet, and what it blocks is left out. What
// was checked or refused is recorded by hash, so a text is asked about once. A
// nil vet takes the snapshot as it stands, leaving out only what was refused
// before. Memories the model wrote are vouched for and never need a check.
func (s *Store) EnsureSnapshot(ctx context.Context, userID, conversationID uuid.UUID, vet Vet) (Snapshot, error) {
	state, err := s.q.GetConversationMemoryState(ctx, sqlc.GetConversationMemoryStateParams{ID: conversationID, UserID: &userID})
	if err != nil {
		return Snapshot{}, err
	}
	if state.MemorySnapshot != nil {
		return decodeSnapshot(state.MemorySnapshot)
	}

	p, err := s.q.GetPersonalization(ctx, userID)
	if err != nil {
		return Snapshot{}, err
	}
	var memories []sqlc.UserMemory
	// Instructions go into every conversation, temporary or not; memories only
	// into one that is neither temporary nor opted out of.
	if !state.Temporary {
		on, err := s.Enabled(ctx, userID)
		if err != nil {
			return Snapshot{}, err
		}
		if on {
			if memories, err = s.q.ListMemories(ctx, userID); err != nil {
				return Snapshot{}, err
			}
		}
	}

	// Each text is let in, kept out because it was refused before, or asked about.
	var ask []*candidate
	consider := func(piece Piece, checked, blocked *string) *candidate {
		c := &candidate{piece: piece, checked: checked, blocked: blocked}
		hash := Hash(piece.Text)
		switch {
		case strings.TrimSpace(piece.Text) == "":
		case checked != nil && *checked == hash:
		case blocked != nil && *blocked == hash:
			c.out = true
		default:
			ask = append(ask, c)
		}
		return c
	}
	about := consider(Piece{Kind: PieceAbout, Text: p.InstructionsAbout}, p.InstructionsAboutCheckedHash, p.InstructionsAboutBlockedHash)
	style := consider(Piece{Kind: PieceStyle, Text: p.InstructionsStyle}, p.InstructionsStyleCheckedHash, p.InstructionsStyleBlockedHash)
	listed := make([]*candidate, len(memories))
	for i, row := range memories {
		listed[i] = consider(Piece{Kind: PieceMemory, ID: row.ID, Text: row.Content}, row.CheckedHash, row.BlockedHash)
	}

	if vet != nil && len(ask) > 0 {
		pieces := make([]Piece, len(ask))
		for i, c := range ask {
			pieces[i] = c.piece
		}
		verdicts, err := vet(ctx, pieces)
		if err != nil {
			return Snapshot{}, err
		}
		if len(verdicts) != len(ask) {
			return Snapshot{}, fmt.Errorf("moderation answered %d texts of %d", len(verdicts), len(ask))
		}
		for i, c := range ask {
			c.out = verdicts[i].Blocked
			switch {
			case !verdicts[i].Checked:
			case c.out:
				c.found = "blocked"
			default:
				c.found = "ok"
			}
		}
		s.recordChecks(ctx, userID, about, style, listed)
	}

	snap := Snapshot{}
	if !about.out {
		snap.About = about.piece.Text
	}
	if !style.out {
		snap.Style = style.piece.Text
	}
	for _, c := range listed {
		if !c.out {
			snap.Memories = append(snap.Memories, SnapshotMemory{Ref: Ref(len(snap.Memories) + 1), ID: c.piece.ID, Content: c.piece.Text})
		}
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return Snapshot{}, err
	}
	kept, err := s.q.SetConversationSnapshot(ctx, sqlc.SetConversationSnapshotParams{ID: conversationID, UserID: &userID, Snapshot: raw})
	if errors.Is(err, pgx.ErrNoRows) {
		// Another request took it first; theirs stands.
		state, err = s.q.GetConversationMemoryState(ctx, sqlc.GetConversationMemoryStateParams{ID: conversationID, UserID: &userID})
		if err != nil {
			return Snapshot{}, err
		}
		return decodeSnapshot(state.MemorySnapshot)
	}
	if err != nil {
		return Snapshot{}, err
	}
	return decodeSnapshot(kept)
}

// recordChecks writes down what a snapshot-time check found out, whoever's
// snapshot ends up standing: it is about the words, not about the conversation.
// Best effort - the text is asked about again next time if this fails - and
// recorded only for text that is still what was checked, which the statements
// see to.
func (s *Store) recordChecks(ctx context.Context, userID uuid.UUID, about, style *candidate, listed []*candidate) {
	hashOf := func(c *candidate, verdict string) *string {
		if c.found != verdict {
			return nil
		}
		hash := Hash(c.piece.Text)
		return &hash
	}
	if about.found != "" || style.found != "" {
		if err := s.q.MarkInstructionsModerated(ctx, sqlc.MarkInstructionsModeratedParams{
			ID:           userID,
			AboutChecked: hashOf(about, "ok"), AboutBlocked: hashOf(about, "blocked"),
			StyleChecked: hashOf(style, "ok"), StyleBlocked: hashOf(style, "blocked"),
		}); err != nil {
			slog.WarnContext(ctx, "record the instructions' moderation", "error", err, "user_id", userID)
		}
	}
	var (
		ids      []uuid.UUID
		hashes   []string
		verdicts []string
	)
	for _, c := range listed {
		if c.found != "" {
			ids, hashes, verdicts = append(ids, c.piece.ID), append(hashes, Hash(c.piece.Text)), append(verdicts, c.found)
		}
	}
	if len(ids) > 0 {
		if err := s.q.MarkMemoriesModerated(ctx, sqlc.MarkMemoriesModeratedParams{
			UserID: userID, Ids: ids, Hashes: hashes, Verdicts: verdicts,
		}); err != nil {
			slog.WarnContext(ctx, "record the memories' moderation", "error", err, "user_id", userID)
		}
	}
}

func decodeSnapshot(raw []byte) (Snapshot, error) {
	var snap Snapshot
	if len(raw) == 0 {
		return snap, nil
	}
	return snap, json.Unmarshal(raw, &snap)
}

// Resolve is the memory a short id stands for in this conversation: from its
// snapshot, or saved since. Ids of any other conversation are not known here.
func (s *Store) Resolve(ctx context.Context, userID, conversationID uuid.UUID, ref string) (uuid.UUID, error) {
	state, err := s.q.GetConversationMemoryState(ctx, sqlc.GetConversationMemoryStateParams{ID: conversationID, UserID: &userID})
	if err != nil {
		return uuid.Nil, ErrUnknownRef
	}
	snap, err := decodeSnapshot(state.MemorySnapshot)
	if err != nil {
		return uuid.Nil, err
	}
	ref = strings.ToLower(strings.TrimSpace(ref))
	for _, m := range snap.Memories {
		if m.Ref == ref {
			return m.ID, nil
		}
	}
	for _, a := range snap.Added {
		if a.Ref == ref {
			return a.ID, nil
		}
	}
	return uuid.Nil, ErrUnknownRef
}

// RefOf is the short id the conversation knows a memory by, or "" when it has
// none.
func (s *Store) RefOf(ctx context.Context, userID, conversationID, memoryID uuid.UUID) string {
	state, err := s.q.GetConversationMemoryState(ctx, sqlc.GetConversationMemoryStateParams{ID: conversationID, UserID: &userID})
	if err != nil {
		return ""
	}
	snap, _ := decodeSnapshot(state.MemorySnapshot)
	for _, m := range snap.Memories {
		if m.ID == memoryID {
			return m.Ref
		}
	}
	for _, a := range snap.Added {
		if a.ID == memoryID {
			return a.Ref
		}
	}
	return ""
}

// ------------------------------------------------------------------- changes

// By is who is changing the list, and from where.
type By struct {
	Source         Source
	ConversationID *uuid.UUID
	// Checked says the text was checked by content moderation as it was written,
	// and passed. What the model writes needs no check (it is output, from
	// conversations that were checked as they were sent); what a person writes
	// without it is checked when a conversation takes its snapshot.
	Checked bool
}

// vouched is the hash recorded with a write: the text's own when it needs no
// further check, nil otherwise.
func (b By) vouched(content string) *string {
	if b.Source == SourceUser && !b.Checked {
		return nil
	}
	hash := Hash(content)
	return &hash
}

// Change is the outcome of a write.
type Change struct {
	Memory sqlc.UserMemory
	// Before is the content a change replaced or removed.
	Before *string
	// Event is the log entry, for a change the model made.
	Event *sqlc.MemoryEvent
	// Ref is the short id the conversation knows the memory by, for an add.
	Ref string
}

func (b By) logged() bool { return b.Source != SourceUser }

func (s *Store) tx(ctx context.Context, userID uuid.UUID, fn func(q *sqlc.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockMemoryList(ctx, userID.String()); err != nil {
			return err
		}
		return fn(q)
	})
}

func duplicateOf(rows []sqlc.UserMemory, content string, except uuid.UUID) bool {
	for _, row := range rows {
		if row.ID != except && strings.EqualFold(row.Content, content) {
			return true
		}
	}
	return false
}

// Add writes a memory. ErrFull at the cap, ErrDuplicate for one that is
// already there.
func (s *Store) Add(ctx context.Context, userID uuid.UUID, raw string, by By) (Change, error) {
	content, err := Clean(raw)
	if err != nil {
		return Change{}, err
	}
	var out Change
	err = s.tx(ctx, userID, func(q *sqlc.Queries) error {
		rows, err := q.ListMemories(ctx, userID)
		if err != nil {
			return err
		}
		if len(rows) >= MaxMemories {
			return ErrFull
		}
		if duplicateOf(rows, content, uuid.Nil) {
			return ErrDuplicate
		}
		out.Memory, err = q.InsertMemory(ctx, sqlc.InsertMemoryParams{
			UserID: userID, Content: content, Source: by.Source, ConversationID: by.ConversationID,
			CheckedHash: by.vouched(content),
		})
		if err != nil {
			return err
		}
		if by.logged() {
			event, err := q.InsertMemoryEvent(ctx, sqlc.InsertMemoryEventParams{
				UserID: userID, MemoryID: out.Memory.ID, Op: sqlc.MemoryOpAdd, After: &content,
				Source: by.Source, ConversationID: by.ConversationID,
			})
			if err != nil {
				return err
			}
			out.Event = &event
		}
		if by.ConversationID != nil {
			out.Ref, err = noteAdded(ctx, q, userID, *by.ConversationID, out.Memory.ID)
		}
		return err
	})
	return out, err
}

// noteAdded gives a memory saved during a conversation a short id there.
func noteAdded(ctx context.Context, q *sqlc.Queries, userID, conversationID, memoryID uuid.UUID) (string, error) {
	state, err := q.GetConversationSnapshotForUpdate(ctx, sqlc.GetConversationSnapshotForUpdateParams{ID: conversationID, UserID: &userID})
	if err != nil {
		return "", err
	}
	snap, err := decodeSnapshot(state.MemorySnapshot)
	if err != nil {
		return "", err
	}
	ref := Ref(len(snap.Memories) + len(snap.Added) + 1)
	snap.Added = append(snap.Added, SnapshotRef{Ref: ref, ID: memoryID})
	raw, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	return ref, q.WriteConversationSnapshot(ctx, sqlc.WriteConversationSnapshotParams{ID: conversationID, UserID: &userID, Snapshot: raw})
}

// Update rewrites a memory.
func (s *Store) Update(ctx context.Context, userID, id uuid.UUID, raw string, by By) (Change, error) {
	content, err := Clean(raw)
	if err != nil {
		return Change{}, err
	}
	var out Change
	err = s.tx(ctx, userID, func(q *sqlc.Queries) error {
		rows, err := q.ListMemories(ctx, userID)
		if err != nil {
			return err
		}
		var current *sqlc.UserMemory
		for i := range rows {
			if rows[i].ID == id {
				current = &rows[i]
			}
		}
		if current == nil {
			return ErrNotFound
		}
		if duplicateOf(rows, content, id) {
			return ErrDuplicate
		}
		before := current.Content
		out.Before = &before
		if out.Memory, err = q.UpdateMemoryContent(ctx, sqlc.UpdateMemoryContentParams{
			ID: id, UserID: userID, Content: content, CheckedHash: by.vouched(content),
		}); err != nil {
			return err
		}
		return s.log(ctx, q, userID, by, &out, sqlc.MemoryOpUpdate, &before, &content)
	})
	return out, err
}

// Delete removes a memory.
func (s *Store) Delete(ctx context.Context, userID, id uuid.UUID, by By) (Change, error) {
	var out Change
	err := s.tx(ctx, userID, func(q *sqlc.Queries) error {
		current, err := q.GetMemory(ctx, sqlc.GetMemoryParams{ID: id, UserID: userID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := q.DeleteMemory(ctx, sqlc.DeleteMemoryParams{ID: id, UserID: userID}); err != nil {
			return err
		}
		out.Memory, out.Before = current, &current.Content
		return s.log(ctx, q, userID, by, &out, sqlc.MemoryOpDelete, &current.Content, nil)
	})
	return out, err
}

// log records a change the model made; a person's own change instead clears
// what the log holds about the memory, so that editing or deleting one really
// does get rid of the old words.
func (s *Store) log(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, by By, out *Change, op sqlc.MemoryOp, before, after *string) error {
	if !by.logged() {
		return q.DeleteMemoryEventsFor(ctx, sqlc.DeleteMemoryEventsForParams{UserID: userID, MemoryID: out.Memory.ID})
	}
	event, err := q.InsertMemoryEvent(ctx, sqlc.InsertMemoryEventParams{
		UserID: userID, MemoryID: out.Memory.ID, Op: op, Before: before, After: after,
		Source: by.Source, ConversationID: by.ConversationID,
	})
	out.Event = &event
	return err
}

// Clear deletes every memory and what the log holds about them.
func (s *Store) Clear(ctx context.Context, userID uuid.UUID) error {
	return s.tx(ctx, userID, func(q *sqlc.Queries) error {
		if err := q.DeleteAllMemories(ctx, userID); err != nil {
			return err
		}
		return q.DeleteAllMemoryEvents(ctx, userID)
	})
}

// List is a person's memories, oldest first.
func (s *Store) List(ctx context.Context, userID uuid.UUID) ([]sqlc.UserMemory, error) {
	return s.q.ListMemories(ctx, userID)
}

// Events is the log of what the model changed, newest first.
func (s *Store) Events(ctx context.Context, userID uuid.UUID, limit int32) ([]sqlc.MemoryEvent, error) {
	cutoff := time.Now().Add(-EventRetention)
	rows, err := s.q.ListMemoryEvents(ctx, sqlc.ListMemoryEventsParams{UserID: userID, RowLimit: limit})
	if err != nil {
		return nil, err
	}
	kept := rows[:0]
	for _, row := range rows {
		if row.CreatedAt.Time.After(cutoff) {
			kept = append(kept, row)
		}
	}
	return kept, nil
}

// PurgeEvents forgets log entries past their retention.
func (s *Store) PurgeEvents(ctx context.Context) (int64, error) {
	return s.q.DeleteOldMemoryEvents(ctx, pgtypeTime(time.Now().Add(-EventRetention)))
}

// Undo reverses a change the model made: an add is removed, an update put
// back, a delete restored. Only while the memory still reads as the change left
// it — an edit made since is the person's, and wins.
func (s *Store) Undo(ctx context.Context, userID, eventID uuid.UUID) (sqlc.MemoryEvent, error) {
	var out sqlc.MemoryEvent
	err := s.tx(ctx, userID, func(q *sqlc.Queries) error {
		event, err := q.GetMemoryEventForUpdate(ctx, sqlc.GetMemoryEventForUpdateParams{ID: eventID, UserID: userID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if event.UndoneAt.Valid {
			return ErrUndone
		}
		current, err := q.GetMemory(ctx, sqlc.GetMemoryParams{ID: event.MemoryID, UserID: userID})
		exists := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		switch event.Op {
		case sqlc.MemoryOpAdd:
			// Gone already is as good as undone.
			if exists {
				if event.After == nil || current.Content != *event.After {
					return ErrChanged
				}
				if _, err := q.DeleteMemory(ctx, sqlc.DeleteMemoryParams{ID: event.MemoryID, UserID: userID}); err != nil {
					return err
				}
			}
		case sqlc.MemoryOpUpdate:
			if !exists {
				return ErrNotFound
			}
			if event.After == nil || event.Before == nil || current.Content != *event.After {
				return ErrChanged
			}
			if _, err := q.UpdateMemoryContent(ctx, sqlc.UpdateMemoryContentParams{ID: event.MemoryID, UserID: userID, Content: *event.Before}); err != nil {
				return err
			}
		case sqlc.MemoryOpDelete:
			if exists {
				return ErrChanged // someone wrote it back already
			}
			if event.Before == nil {
				return ErrNotFound
			}
			rows, err := q.ListMemories(ctx, userID)
			if err != nil {
				return err
			}
			if len(rows) >= MaxMemories {
				return ErrFull
			}
			if duplicateOf(rows, *event.Before, uuid.Nil) {
				return ErrDuplicate
			}
			// Restored by a person's decision, so it is theirs now.
			id := event.MemoryID
			if _, err := q.InsertMemory(ctx, sqlc.InsertMemoryParams{
				ID: &id, UserID: userID, Content: *event.Before, Source: SourceUser, ConversationID: event.ConversationID,
			}); err != nil {
				return err
			}
		}
		out, err = q.MarkMemoryEventUndone(ctx, sqlc.MarkMemoryEventUndoneParams{ID: eventID, UserID: userID})
		return err
	})
	return out, err
}
