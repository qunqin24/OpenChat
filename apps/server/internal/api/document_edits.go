package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// The person's own edits to a writing block reach the model two ways, so that
// a "go on" after a few changes of theirs builds on their text:
//
//   - DocumentNote tells each run, with its input, what the person changed in
//     the conversation's documents since the model last wrote them, as a diff.
//   - A rewrite of a whole document (op=update with content) over a version of
//     the person's the run has not been shown is refused, with their changes,
//     since it would be written from the model's old text and undo them.
//
// What a run has been shown is kept in its agent.RunState: the version of each
// document it wrote, read, or was told about. A run resumed after a restart
// has forgotten it, and is refused one rewrite too many at worst.

const (
	// maxDocumentDiff bounds the changes shown of one document; past it, the
	// model is told to read the document instead.
	maxDocumentDiff = 6000
	// maxNotedDocuments bounds the documents one note covers: the latest.
	maxNotedDocuments = 5
)

type documentSeenKey struct{ id uuid.UUID }

// documentSeen is the version of a document this run has been shown, 0 none.
func documentSeen(ctx context.Context, id uuid.UUID) int32 {
	scope, _ := agent.ScopeFrom(ctx)
	if v, ok := scope.State.Load(documentSeenKey{id}); ok {
		return v.(int32)
	}
	return 0
}

// sawDocument records that this run has been shown version of a document.
func sawDocument(ctx context.Context, id uuid.UUID, version int32) {
	if version > documentSeen(ctx, id) {
		scope, _ := agent.ScopeFrom(ctx)
		scope.State.Store(documentSeenKey{id}, version)
	}
}

// documentBase is what the model last knew of a document before latest: the
// version this run was shown, or else the last one a model wrote.
func (s *Server) documentBase(ctx context.Context, doc sqlc.Document) (sqlc.DocumentVersion, error) {
	if seen := documentSeen(ctx, doc.ID); seen > 0 {
		return s.q.GetDocumentVersion(ctx, sqlc.GetDocumentVersionParams{DocumentID: doc.ID, Version: seen})
	}
	return s.q.LatestModelDocumentVersion(ctx, doc.ID)
}

// DocumentNote is the turn note (agent.TurnNote) about the documents of the
// run's conversation the person edited since the model's last version: each
// one's changes, or why they are not shown. The person's text is checked by
// content moderation first, as when the model reads it (documentText).
func (s *Server) DocumentNote(ctx context.Context) (string, error) {
	scope, ok := agent.ScopeFrom(ctx)
	if !ok {
		return "", nil
	}
	user, err := s.q.GetUser(ctx, scope.UserID)
	if err != nil {
		return "", err
	}
	docs, err := s.q.ListConversationDocuments(ctx, sqlc.ListConversationDocumentsParams{ConversationID: scope.ConversationID, UserID: user.ID})
	if err != nil {
		return "", err
	}
	var edited []sqlc.ListConversationDocumentsRow
	for _, d := range docs {
		if d.Source == "user" {
			edited = append(edited, d)
		} else {
			sawDocument(ctx, d.ID, d.Version) // its own text: nothing to tell
		}
	}
	if len(edited) == 0 {
		return "", nil
	}
	if len(edited) > maxNotedDocuments {
		edited = edited[len(edited)-maxNotedDocuments:]
	}

	var b strings.Builder
	b.WriteString("<document-edits>\nThe user edited these writing blocks themselves since your last version of them. " +
		"Each now holds their text: build on it, and keep their changes unless they ask otherwise.\n")
	for _, d := range edited {
		doc, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{ID: d.ID, UserID: user.ID})
		if err != nil {
			return "", err
		}
		// The title is the person's words as much as the text: named only once
		// moderation has passed them, by its id otherwise.
		latest, err := s.documentText(ctx, user, doc)
		var api *httpx.APIError
		switch {
		case errors.As(err, &api) && api.Code == "content_blocked":
			fmt.Fprintf(&b, "\nDocument %s, now version %d: content moderation refused the user's text, so it is not shown. Do not use it.\n", d.ID, d.Version)
			continue
		case errors.As(err, &api) && api.Code == "moderation_unavailable":
			fmt.Fprintf(&b, "\nDocument %s, now version %d: the user's text cannot be checked right now, so it is not shown. Read it (op=read) before changing it.\n", d.ID, d.Version)
			continue
		case err != nil:
			return "", err
		}
		fmt.Fprintf(&b, "\n%q (id %s), now version %d", latest.Title, d.ID, latest.Version)
		base, err := s.documentBase(ctx, doc)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		diff, fits := documentDiff(base, latest)
		if !fits {
			fmt.Fprintf(&b, ", from your version %d: the changes are too long to show here. Read it (op=read) before changing it.\n", base.Version)
			continue
		}
		fmt.Fprintf(&b, ", from your version %d:\n%s", base.Version, diff)
		sawDocument(ctx, d.ID, latest.Version)
	}
	b.WriteString("</document-edits>")
	return b.String(), nil
}

// documentDiff is how after differs from before, paragraph by paragraph: each
// run of changes with the paragraph before it, removed lines marked "-",
// added "+", kept " ". False where it would be longer than maxDocumentDiff.
func documentDiff(before, after sqlc.DocumentVersion) (string, bool) {
	var b strings.Builder
	if before.Title != after.Title && before.Title != "" {
		fmt.Fprintf(&b, "Title: %q -> %q\n", before.Title, after.Title)
	}
	write := func(mark string, paragraph string) {
		for line := range strings.SplitSeq(paragraph, "\n") {
			b.WriteString(mark)
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	old, now := paragraphs(before.Content), paragraphs(after.Content)
	ops := diffParagraphs(old, now)
	hunks := 0
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		if hunks++; hunks > 1 {
			b.WriteString("…\n")
		}
		if i > 0 {
			write("  ", ops[i-1].text)
		}
		for ; i < len(ops) && ops[i].kind != ' '; i++ {
			write(string(ops[i].kind)+" ", ops[i].text)
		}
	}
	if b.Len() == 0 {
		b.WriteString("(only spacing changed)\n")
	}
	return b.String(), b.Len() <= maxDocumentDiff
}

func paragraphs(text string) []string {
	var out []string
	for p := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type paragraphOp struct {
	kind byte // ' ' kept, '-' removed, '+' added
	text string
}

// diffParagraphs lines up two lists of paragraphs: what both start and end
// with is kept, and the middle is matched by longest common subsequence -
// or, where that would be a large table, all removed and all added.
func diffParagraphs(old, now []string) []paragraphOp {
	head := 0
	for head < len(old) && head < len(now) && old[head] == now[head] {
		head++
	}
	tail := 0
	for tail < len(old)-head && tail < len(now)-head && old[len(old)-1-tail] == now[len(now)-1-tail] {
		tail++
	}
	var ops []paragraphOp
	for _, p := range old[:head] {
		ops = append(ops, paragraphOp{' ', p})
	}
	a, c := old[head:len(old)-tail], now[head:len(now)-tail]
	if len(a)*len(c) > 40_000 {
		for _, p := range a {
			ops = append(ops, paragraphOp{'-', p})
		}
		for _, p := range c {
			ops = append(ops, paragraphOp{'+', p})
		}
	} else {
		// lcs[i][j] is the longest common run of a[i:] and c[j:].
		lcs := make([][]int, len(a)+1)
		for i := range lcs {
			lcs[i] = make([]int, len(c)+1)
		}
		for i := len(a) - 1; i >= 0; i-- {
			for j := len(c) - 1; j >= 0; j-- {
				if a[i] == c[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(a) || j < len(c) {
			switch {
			case i < len(a) && j < len(c) && a[i] == c[j]:
				ops = append(ops, paragraphOp{' ', a[i]})
				i, j = i+1, j+1
			case i < len(a) && (j == len(c) || lcs[i+1][j] >= lcs[i][j+1]):
				ops = append(ops, paragraphOp{'-', a[i]}) // what went, before what came
				i++
			default:
				ops = append(ops, paragraphOp{'+', c[j]})
				j++
			}
		}
	}
	for _, p := range now[len(now)-tail:] {
		ops = append(ops, paragraphOp{' ', p})
	}
	return ops
}
