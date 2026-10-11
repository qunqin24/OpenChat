package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/memory"
)

// Receipts: what a guest's browser is believed about.
//
// A guest's conversation is not on the server; each request carries all of it,
// and the browser could write anything there: a question that content
// moderation would refuse, an answer the model never gave, a summary, a
// memory. Only the new message was ever checked. So the server signs what it
// has seen, and takes nothing else on trust:
//
//   - a user's words the server moderated (the new question, and later the
//     instructions and memories it checked) get a receipt, which the browser
//     keeps next to them and sends back; the same words need no second check;
//   - every row the run wrote (assistant, tool), every summary the server's
//     model wrote, and every note the model saved get one too;
//   - a request is then read for what has no valid receipt. Words a person
//     could have written are moderated, once, together. Rows, summaries and the
//     rest that only the server could have written are refused
//     (invalid_history, invalid_summary): there is nothing to moderate in them,
//     they are not the model's.
//
// A receipt is an HMAC (a key the server keeps in app_settings and never
// shows) over a canonical encoding of exactly what the model would be sent,
// with a domain in front so that a receipt for one kind of thing is never
// good for another. One changed byte is a missing receipt.
//
// The checked-words receipts (text, file, snapshot) are only issued while
// moderation is on, since they say "this was checked"; turn it on later and
// everything is checked once. Rows and summaries are the server's own work and
// are signed always. Nothing is enforced while moderation is off.
const settingGuestReceiptKey = "guest_receipt_key"

// receiptSigner makes and checks receipts. It has the key and nothing else, so
// the streaming code can use it without a context.
type receiptSigner struct{ key []byte }

// guestSigner is the signer, with the key made on first use. Two processes
// starting together both make one; InsertSettingIfMissing keeps the first and
// reading back is how each learns which (as the end-user key does).
func (s *Server) guestSigner(ctx context.Context) (*receiptSigner, error) {
	s.receiptMu.Lock()
	defer s.receiptMu.Unlock()
	if s.receiptKey != nil {
		return &receiptSigner{key: s.receiptKey}, nil
	}
	key, err := stringSetting(ctx, s.q, settingGuestReceiptKey)
	if err != nil {
		return nil, err
	}
	if key == "" {
		fresh := make([]byte, 32)
		if _, err := rand.Read(fresh); err != nil {
			return nil, err
		}
		value, _ := json.Marshal(hex.EncodeToString(fresh))
		if err := s.q.InsertSettingIfMissing(ctx, sqlc.InsertSettingIfMissingParams{Key: settingGuestReceiptKey, Value: value}); err != nil {
			return nil, err
		}
		if key, err = stringSetting(ctx, s.q, settingGuestReceiptKey); err != nil {
			return nil, err
		}
	}
	decoded, err := hex.DecodeString(key)
	if err != nil || len(decoded) < 32 {
		return nil, errors.New("the stored guest receipt key is malformed")
	}
	s.receiptKey = decoded
	return &receiptSigner{key: decoded}, nil
}

// mac is the receipt for a domain and its fields. Every field is length-framed,
// so no two different field lists encode alike.
func (g *receiptSigner) mac(domain string, fields ...[]byte) string {
	h := hmac.New(sha256.New, g.key)
	h.Write([]byte(domain))
	h.Write([]byte{0})
	for _, f := range fields {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(f)))
		h.Write(n[:])
		h.Write(f)
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// receiptMatches compares in constant time. A missing receipt is never valid.
func receiptMatches(got *string, want string) bool {
	return got != nil && *got != "" && subtle.ConstantTimeCompare([]byte(*got), []byte(want)) == 1
}

// ---- words a person wrote, checked by moderation

// text is the receipt for words the server has moderated: a user row, the
// custom instructions, a memory. The same receipt serves wherever the same
// words are used.
func (g *receiptSigner) text(words string) string { return g.mac("t", []byte(words)) }

// file is the receipt for a picture's bytes the moderation service was sent.
func (g *receiptSigner) file(contentType string, data []byte) string {
	sum := sha256.Sum256(data)
	return g.mac("f", []byte(contentType), sum[:])
}

// snapshot is the receipt for a conversation's memory snapshot: the
// instructions and notes its system prompt is rendered from. Canonical: the
// ids are not part of it (they differ between the browser's and the request's),
// and the snapshot is encoded from its type, not from whatever object the
// browser sent.
func (g *receiptSigner) snapshot(snap memory.Snapshot) string {
	return g.mac("n", snapshotCanon(snap))
}

func snapshotCanon(snap memory.Snapshot) []byte {
	clean := memory.Snapshot{About: snap.About, Style: snap.Style}
	for _, m := range snap.Memories {
		clean.Memories = append(clean.Memories, memory.SnapshotMemory{Ref: m.Ref, Content: m.Content})
	}
	for _, a := range snap.Added {
		clean.Added = append(clean.Added, memory.SnapshotRef{Ref: a.Ref})
	}
	raw, _ := json.Marshal(clean)
	return raw
}

// ---- what the server wrote

// summary is the receipt for the text of a summary the server's model wrote.
func (g *receiptSigner) summary(content string) string { return g.mac("s", []byte(content)) }

// rowReceipt is the receipt for a row of the model's (assistant, tool): the
// blocks the model would be sent as they are rebuilt from the row (guestParts),
// and, after a dot, the row's plain text, which is all a title is made from.
// The first half is what a request is checked against.
func (g *receiptSigner) rowReceipt(role sqlc.MessageRole, parts []llm.Part, content string) string {
	return g.mac("a", []byte(role), agent.EncodeParts(parts)) + "." + g.mac("c", []byte(content))
}

// rowValid is whether a row's receipt covers these blocks.
func (g *receiptSigner) rowValid(role sqlc.MessageRole, parts []llm.Part, receipt *string) bool {
	if receipt == nil {
		return false
	}
	head, _, _ := strings.Cut(*receipt, ".")
	return receiptMatches(&head, g.mac("a", []byte(role), agent.EncodeParts(parts)))
}

// contentValid is whether a row's receipt covers this plain text.
func (g *receiptSigner) contentValid(content string, receipt *string) bool {
	if receipt == nil {
		return false
	}
	_, tail, ok := strings.Cut(*receipt, ".")
	return ok && receiptMatches(&tail, g.mac("c", []byte(content)))
}

// messageReceipt signs a row as the browser will send it back: the message goes
// through JSON into the shape the request has it in (the browser does exactly
// that) and the blocks are read from that, the way verification reads them. So
// whatever does not survive the round trip is not in the receipt either.
func (g *receiptSigner) messageReceipt(m Message) string {
	raw, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	var back GuestMessage
	if err := json.Unmarshal(raw, &back); err != nil {
		return ""
	}
	parts := guestParts(back)
	return g.rowReceipt(sqlc.MessageRole(m.Role), parts, m.Content)
}

// ---- reading a request

// checkedWords is words in a request that nothing vouches for.
type checkedWords struct {
	text   string
	images []moderationImage
}

// guestFresh records what a request had to have moderated, so that the last
// frame can hand back receipts for it.
type guestFresh struct {
	rows  map[uuid.UUID]bool
	files map[uuid.UUID]bool
}

// guestContent is everything of a request that reaches a model and is the
// browser's to claim.
type guestContent struct {
	rows        []guestRow
	about       string
	style       string
	receipts    GuestReceipts
	memories    []GuestMemory
	snapshot    []byte // canonical, nil for none
	snapTexts   []string
	summary     *ContextSummary
	summaryText string // a summary given as text (manual compression)
	summaryRcpt *string
}

// errInvalidHistory is a row of the model's the server did not write.
func errInvalidHistory(what string) error {
	return guestBad("invalid_history", what+" was not written by this server; the conversation cannot be continued")
}

// verifyGuest reads a request for what has no receipt. It returns the words
// to moderate and what they were. A row of the model's, or a summary, without
// one is an error. Called with a signer, so it needs no database.
func (g *receiptSigner) verify(c guestContent) (pending []checkedWords, fresh guestFresh, err error) {
	fresh = guestFresh{rows: map[uuid.UUID]bool{}, files: map[uuid.UUID]bool{}}
	// known are the words already vouched for or already on the list.
	known := map[string]bool{}
	add := func(text string) {
		if strings.TrimSpace(text) != "" && !known[text] {
			known[text] = true
			pending = append(pending, checkedWords{text: text})
		}
	}

	for _, row := range c.rows {
		switch row.role {
		case sqlc.MessageRoleUser:
			var item checkedWords
			if strings.TrimSpace(row.content) != "" && !receiptMatches(&row.receipt, g.text(row.content)) {
				fresh.rows[row.id] = true
				item.text = row.content
			}
			for _, file := range row.files {
				// Only bytes the request sends reach the model; a file named and not sent is a line of text.
				if file.data == nil || llm.ModalityOf(file.contentType) != llm.ModalityImage {
					continue
				}
				if !receiptMatches(&file.receipt, g.file(file.contentType, file.data)) {
					fresh.files[file.id] = true
					item.images = append(item.images, moderationImage{contentType: file.contentType, data: file.data})
				}
			}
			if item.text != "" || len(item.images) > 0 {
				pending = append(pending, item)
			}
		default:
			// Nothing in a row with no blocks reaches a model (the empty failed
			// reply the browser writes for a lost connection is one).
			if len(row.parts) == 0 {
				continue
			}
			if !g.rowValid(row.role, row.parts, row.receiptPtr()) {
				return nil, fresh, errInvalidHistory("a " + string(row.role) + " message")
			}
		}
	}

	// The custom instructions.
	if c.about != "" && receiptMatches(c.receipts.AboutYou, g.text(c.about)) {
		known[c.about] = true
	} else {
		add(c.about)
	}
	if c.style != "" && receiptMatches(c.receipts.ResponseStyle, g.text(c.style)) {
		known[c.style] = true
	} else {
		add(c.style)
	}
	// The notes. A note the model saved has its receipt like one a person
	// checked; one that has none is words like any other.
	for _, m := range c.memories {
		if receiptMatches(m.Receipt, g.text(m.Content)) {
			known[m.Content] = true
		} else {
			add(m.Content)
		}
	}
	// The snapshot the system prompt is rendered from: the same words again,
	// frozen at the conversation's first run. It stands on its own receipt, or
	// its words are checked (those not already vouched for above).
	if c.snapshot != nil && !receiptMatches(c.receipts.MemorySnapshot, g.mac("n", c.snapshot)) {
		for _, t := range c.snapTexts {
			add(t)
		}
	}

	if c.summary != nil && !receiptMatches(c.summary.Receipt, g.summary(c.summary.Content)) {
		return nil, fresh, guestBad("invalid_summary", "the summary was not written by this server")
	}
	if c.summaryText != "" && !receiptMatches(c.summaryRcpt, g.summary(c.summaryText)) {
		return nil, fresh, guestBad("invalid_summary", "the summary was not written by this server")
	}
	return pending, fresh, nil
}

// receiptPtr is the row's receipt as a pointer, for the checks that take one.
func (r guestRow) receiptPtr() *string {
	if r.receipt == "" {
		return nil
	}
	return &r.receipt
}

// ---- moderating what has no receipt

const (
	// guestCheckRunes bounds the words of one moderation call, so that none of
	// it falls in the part a longer text is not read from (moderationTextRunes).
	guestCheckRunes = moderationTextRunes
	// guestCheckMaxRunes and guestCheckMaxImages bound how much a request may
	// ask to have checked: each guestCheckRunes is a call.
	guestCheckMaxRunes  = 10 * guestCheckRunes
	guestCheckMaxImages = 60
)

var errGuestCheckTooLarge = httpx.NewError(http.StatusRequestEntityTooLarge, "history_too_large",
	"too much of this conversation has not been checked yet; start a new one")

// guestCalls batches words into as few moderation calls as fit: each at most
// guestCheckRunes of text, six pictures and the bytes one call sends.
func guestCalls(items []checkedWords) ([]checkedWords, error) {
	var calls []checkedWords
	var cur checkedWords
	runes, size, images, total := 0, 0, 0, 0
	flush := func() {
		if cur.text != "" || len(cur.images) > 0 {
			calls = append(calls, cur)
		}
		cur, runes, size = checkedWords{}, 0, 0
	}
	for _, item := range items {
		rest := strings.TrimSpace(item.text)
		for rest != "" {
			sep := 0
			if cur.text != "" {
				sep = 2
			}
			room := guestCheckRunes - runes - sep
			if room <= 0 {
				flush()
				continue
			}
			piece, tail := cutRunes(rest, room)
			n := utf8.RuneCountInString(piece)
			if total += n; total > guestCheckMaxRunes {
				return nil, errGuestCheckTooLarge
			}
			if sep > 0 {
				cur.text += "\n\n"
			}
			cur.text += piece
			runes += sep + n
			rest = tail
			// A text that did not fit whole has filled the call.
			if rest != "" {
				flush()
			}
		}
		for _, image := range item.images {
			if images++; images > guestCheckMaxImages {
				return nil, errGuestCheckTooLarge
			}
			if len(cur.images) >= moderationMaxImages || size+len(image.data) > moderationMaxImageBytes {
				flush()
			}
			cur.images = append(cur.images, image)
			size += len(image.data)
		}
	}
	flush()
	return calls, nil
}

// cutRunes splits text after its first n runes.
func cutRunes(text string, n int) (head, tail string) {
	cut := 0
	for i := 0; i < n && cut < len(text); i++ {
		_, width := utf8.DecodeRuneInString(text[cut:])
		cut += width
	}
	return text[:cut], text[cut:]
}

// moderateWords checks what a request carried that nothing vouched for, in as
// few calls as it fits in (one, normally). A refusal is marked as being about
// the conversation rather than the new message: the page keeps a refused new
// message as a blocked row, and this was not it.
func (s *Server) moderateWords(ctx context.Context, ip string, items []checkedWords) error {
	calls, err := guestCalls(items)
	if err != nil || len(calls) == 0 {
		return err
	}
	who := moderationSubject{guestIP: ip, source: ModerationSourceGuest}
	for _, call := range calls {
		in := moderationInput{text: call.text}
		if len(call.images) > 0 {
			images := call.images
			in.images = func(context.Context) ([]moderationImage, error) { return images, nil }
		}
		if err := s.moderate(ctx, who, in); err != nil {
			var api *httpx.APIError
			if errors.As(err, &api) && api.Code == "content_blocked" {
				if api.Details == nil {
					api.Details = map[string]any{}
				}
				api.Details["history"] = true
			}
			return err
		}
	}
	return nil
}

// guestModerationOn is whether anything is checked. A setting that cannot be
// read is a service that cannot be asked, which a guest's words do not get past.
func (s *Server) guestModerationOn(ctx context.Context) (bool, error) {
	cfg, err := loadModeration(ctx, s.q)
	if err != nil {
		return true, s.moderationFailed(ctx, defaultModerationConfig(), moderationSubject{}, err)
	}
	return cfg.Mode != ModerationModeOff, nil
}

// snapshotOf reads what the browser sent as the conversation's snapshot, as a
// snapshot and nothing else.
func snapshotOf(raw map[string]any) (memory.Snapshot, error) {
	var snap memory.Snapshot
	encoded, err := json.Marshal(raw)
	if err != nil {
		return snap, fmt.Errorf("memorySnapshot: %w", err)
	}
	return snap, json.Unmarshal(encoded, &snap)
}

// snapshotWords are the words in a snapshot.
func snapshotWords(snap memory.Snapshot) []string {
	words := []string{snap.About, snap.Style}
	for _, m := range snap.Memories {
		words = append(words, m.Content)
	}
	return words
}
