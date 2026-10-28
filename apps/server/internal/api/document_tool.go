package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// ToolDocument is the writing-block tool's name.
const ToolDocument = "document"

// DocumentTool is the `document` tool: the model writes a text the person
// will want to revise - an article, an email, a post, or a text of theirs to
// improve - as a writing block (documents.go), and changes it with targeted
// edits rather than writing it all again. It lives here, as the task tool
// does, because reading the person's text goes through the server's
// moderation. Everything it touches is the run's own conversation's.
type DocumentTool struct {
	s *Server
	// readOnly is a sub-agent's: it lists and reads the documents of the
	// conversation it works for, and writes none.
	readOnly bool
}

var _ agent.Tool = DocumentTool{}

// DocumentToolArgs are a call's arguments.
type DocumentToolArgs struct {
	Op      string             `json:"op"`
	ID      string             `json:"id"`
	Title   *string            `json:"title"`
	Content *string            `json:"content"`
	Edits   []DocumentToolEdit `json:"edits"`
}

// DocumentToolEdit replaces one passage.
type DocumentToolEdit struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

// DocumentToolDetails is what the interface is shown of a call: which
// document, at which version, so the reply can draw it.
type DocumentToolDetails struct {
	Op         string    `json:"op"`
	DocumentID uuid.UUID `json:"documentId,omitempty"`
	Version    int       `json:"version,omitempty"`
	Title      string    `json:"title,omitempty"`
}

func (t DocumentTool) Definition() llm.Tool {
	if t.readOnly {
		return llm.Tool{
			Name: ToolDocument,
			Description: "Read the writing blocks (documents) of the conversation you work for. op=list shows them " +
				"with their ids; op=read returns one's current text (id). Content is Markdown.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "op": {"type": "string", "enum": ["read", "list"]},
    "id": {"type": "string", "description": "The document's id. For read."}
  },
  "required": ["op"]
}`),
		}
	}
	return llm.Tool{
		Name: ToolDocument,
		Description: "Write in a writing block: a document shown to the user beside the chat, which they can edit " +
			"themselves and ask you to revise. Use it for a piece of writing the user will likely keep and refine - an " +
			"article, essay, email, letter, post, speech, story, product copy - and when the user gives you a text " +
			"of theirs to improve. Not for short answers, explanations, lists of ideas or code. " +
			"op=create writes a new document (title, content). op=update changes one (id): give edits, each " +
			"replacing an exact passage (find, copied exactly from the current text and long enough to occur only " +
			"once) with new text (replace) - prefer this for any change smaller than the whole text; give content to " +
			"rewrite it all; give title to rename it. op=read returns a document's current text (id). op=list shows " +
			"this conversation's documents. The user may have edited a document since you last saw it: read it " +
			"before editing if unsure. Content is Markdown.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "op": {"type": "string", "enum": ["create", "update", "read", "list"]},
    "id": {"type": "string", "description": "The document's id. For update and read."},
    "title": {"type": "string", "description": "The document's title, shown above the text as its heading. For create, or to rename it on update."},
    "content": {"type": "string", "description": "The whole text, in Markdown, without the title: it starts with the text itself, never a # heading (the title is shown above it). For create, or to rewrite it all on update."},
    "edits": {
      "type": "array",
      "description": "For update: passages to replace, applied in order.",
      "items": {
        "type": "object",
        "properties": {
          "find": {"type": "string", "description": "An exact passage of the current text, occurring once."},
          "replace": {"type": "string", "description": "What it becomes."}
        },
        "required": ["find", "replace"]
      }
    }
  },
  "required": ["op"]
}`),
	}
}

// Replayable is false: a call cut off may have written its version already.
// A sub-agent's only reads.
func (t DocumentTool) Replayable() bool { return t.readOnly }

// DocumentTools is the writing-block tool, for a run of a signed-in person.
func (s *Server) DocumentTools(ctx context.Context) []agent.Tool {
	scope, ok := agent.ScopeFrom(ctx)
	if !ok {
		return nil
	}
	return []agent.Tool{DocumentTool{s: s, readOnly: scope.Subagent()}}
}

// ServerTools is every tool that works through the server, for the runner. A
// sub-agent's run gets only the writing blocks, to read: no tasks, and no
// sub-agents of its own.
func (s *Server) ServerTools(ctx context.Context) []agent.Tool {
	if scope, ok := agent.ScopeFrom(ctx); ok && scope.Subagent() {
		return s.DocumentTools(ctx)
	}
	tools := append(s.TaskTools(ctx), s.DocumentTools(ctx)...)
	return append(tools, s.SubagentTools(ctx)...)
}

func (t DocumentTool) Execute(ctx context.Context, raw json.RawMessage) (agent.ToolResult, error) {
	scope, ok := agent.ScopeFrom(ctx)
	if !ok {
		return agent.ToolResult{}, errors.New("document: no run in context")
	}
	var args DocumentToolArgs
	if err := agent.DecodeArgs(raw, &args); err != nil {
		return agent.ToolResult{}, err
	}
	s := t.s
	user, err := s.q.GetUser(ctx, scope.UserID)
	if err != nil {
		return agent.ToolResult{}, err
	}
	// A sub-agent reads the documents of the conversation it works for.
	if scope.Parent != nil {
		scope.ConversationID = *scope.Parent
	}

	op := strings.ToLower(strings.TrimSpace(args.Op))
	if t.readOnly && op != "read" && op != "list" {
		return agent.ToolResult{}, agent.Errorf("invalid arguments: op must be read or list")
	}
	switch op {
	case "list":
		docs, err := s.q.ListConversationDocuments(ctx, sqlc.ListConversationDocumentsParams{ConversationID: scope.ConversationID, UserID: user.ID})
		if err != nil {
			return agent.ToolResult{}, err
		}
		if len(docs) == 0 {
			return agent.ToolResult{Content: "This conversation has no documents.", Details: DocumentToolDetails{Op: op}}, nil
		}
		var b strings.Builder
		for _, d := range docs {
			// A title the person last wrote reaches the model only once moderation
			// has passed it, as their text does when it is read (documentText).
			title := fmt.Sprintf("%q", d.Title)
			if d.Source == "user" {
				title = "(title not shown: the user's text could not be checked; read it to see it)"
				if doc, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{ID: d.ID, UserID: user.ID}); err == nil {
					if latest, err := s.documentText(ctx, user, doc); err == nil {
						title = fmt.Sprintf("%q", latest.Title)
					}
				}
			}
			fmt.Fprintf(&b, "- [%s] %s, version %d, %d characters", d.ID, title, d.Version, d.Length)
			if d.Source == "user" {
				b.WriteString(", last edited by the user")
			}
			b.WriteString("\n")
		}
		return agent.ToolResult{Content: b.String(), Details: DocumentToolDetails{Op: op}}, nil

	case "create":
		if args.Title == nil || strings.TrimSpace(*args.Title) == "" || args.Content == nil {
			return agent.ToolResult{}, agent.Errorf("invalid arguments: create needs title and content")
		}
		title, content := clip(strings.TrimSpace(*args.Title), maxDocumentTitle), *args.Content
		if utf8.RuneCountInString(content) > maxDocumentContent {
			return agent.ToolResult{}, agent.Errorf("the content is longer than %d characters; write it shorter", maxDocumentContent)
		}
		var doc sqlc.Document
		var version sqlc.DocumentVersion
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			q := s.q.WithTx(tx)
			var err error
			if doc, err = q.CreateDocument(ctx, sqlc.CreateDocumentParams{ConversationID: scope.ConversationID, UserID: user.ID, RunID: scope.RunID}); err != nil {
				return err
			}
			hash := documentHash(title, content)
			version, err = q.InsertDocumentVersion(ctx, sqlc.InsertDocumentVersionParams{
				DocumentID: doc.ID, Title: title, Content: content, Source: "model", RunID: &scope.RunID, Checked: &hash,
			})
			return err
		})
		if err != nil {
			return agent.ToolResult{}, err
		}
		sawDocument(ctx, doc.ID, version.Version)
		return agent.ToolResult{
			Content: fmt.Sprintf("Created document %s, version 1: %q. The user sees it beside the chat; do not repeat its text in your reply.", doc.ID, title),
			Details: DocumentToolDetails{Op: op, DocumentID: doc.ID, Version: int(version.Version), Title: title},
		}, nil

	case "read", "update":
		id, err := uuid.Parse(strings.TrimSpace(args.ID))
		if err != nil {
			return agent.ToolResult{}, agent.Errorf("invalid arguments: id must be a document id (op=list shows them)")
		}
		doc, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{ID: id, UserID: user.ID})
		if err != nil || doc.ConversationID != scope.ConversationID {
			return agent.ToolResult{}, agent.Errorf("There is no document with that id in this conversation. Call op=list for the ids.")
		}
		latest, err := s.documentText(ctx, user, doc)
		if err != nil {
			return agent.ToolResult{}, documentToolError(err)
		}
		edited := ""
		if latest.Source == "user" {
			edited = " The user edited it since the last change you made."
		}
		if op == "read" {
			sawDocument(ctx, doc.ID, latest.Version)
			return agent.ToolResult{
				Content: fmt.Sprintf("Document %s, version %d.%s\nTitle: %s\n\n%s", doc.ID, latest.Version, edited, latest.Title, latest.Content),
				Details: DocumentToolDetails{Op: op, DocumentID: doc.ID, Version: int(latest.Version), Title: latest.Title},
			}, nil
		}
		return t.update(ctx, scope, doc, latest, args, edited)

	default:
		return agent.ToolResult{}, agent.Errorf("invalid arguments: op must be create, update, read or list")
	}
}

// update writes the next version of a document from the model's edits.
func (t DocumentTool) update(ctx context.Context, scope agent.Scope, doc sqlc.Document, latest sqlc.DocumentVersion, args DocumentToolArgs, edited string) (agent.ToolResult, error) {
	s := t.s
	title, content := latest.Title, latest.Content
	if args.Title != nil && strings.TrimSpace(*args.Title) != "" {
		title = clip(strings.TrimSpace(*args.Title), maxDocumentTitle)
	}
	switch {
	case args.Content != nil:
		// Written from what the model last knew: over a version of the
		// person's it has not been shown, that would undo their changes.
		if latest.Source == "user" && latest.Version > documentSeen(ctx, doc.ID) {
			return agent.ToolResult{}, t.unseenEdits(ctx, doc, latest)
		}
		content = *args.Content
	case len(args.Edits) > 0:
		for i, e := range args.Edits {
			if e.Find == "" {
				return agent.ToolResult{}, agent.Errorf("edit %d: find is empty. Nothing was changed.", i+1)
			}
			switch n := strings.Count(content, e.Find); n {
			case 1:
				content = strings.Replace(content, e.Find, e.Replace, 1)
			case 0:
				return agent.ToolResult{}, agent.Errorf("edit %d: the passage was not found in the current text (version %d).%s "+
					"Nothing was changed. Read the document and copy the passage exactly.", i+1, latest.Version, edited)
			default:
				return agent.ToolResult{}, agent.Errorf("edit %d: the passage occurs %d times. Nothing was changed. "+
					"Include more of the surrounding text so it occurs once.", i+1, n)
			}
		}
	case args.Title == nil:
		return agent.ToolResult{}, agent.Errorf("invalid arguments: update needs edits, content or title")
	}
	if utf8.RuneCountInString(content) > maxDocumentContent {
		return agent.ToolResult{}, agent.Errorf("the result would be longer than %d characters. Nothing was changed.", maxDocumentContent)
	}

	var version sqlc.DocumentVersion
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockDocument(ctx, sqlc.LockDocumentParams{ID: doc.ID, UserID: doc.UserID}); err != nil {
			return err
		}
		// The person may have saved a version while this was being worked out.
		now, err := q.LatestDocumentVersion(ctx, doc.ID)
		if err != nil {
			return err
		}
		if now.Version != latest.Version {
			return errVersionConflict
		}
		hash := documentHash(title, content)
		version, err = q.InsertDocumentVersion(ctx, sqlc.InsertDocumentVersionParams{
			DocumentID: doc.ID, Title: title, Content: content, Source: "model", RunID: &scope.RunID, Checked: &hash,
		})
		return err
	})
	if errors.Is(err, errVersionConflict) {
		return agent.ToolResult{}, agent.Errorf("The user saved a new version while you were editing. Nothing was changed. Read the document again, then edit.")
	}
	if err != nil {
		return agent.ToolResult{}, err
	}
	sawDocument(ctx, doc.ID, version.Version)
	return agent.ToolResult{
		Content: fmt.Sprintf("Updated document %s to version %d.%s The user sees the change beside the chat; do not repeat the text in your reply.", doc.ID, version.Version, edited),
		Details: DocumentToolDetails{Op: "update", DocumentID: doc.ID, Version: int(version.Version), Title: title},
	}, nil
}

// unseenEdits refuses a rewrite over the person's edits the run has not been
// shown, and shows them: the next attempt can build on their text.
func (t DocumentTool) unseenEdits(ctx context.Context, doc sqlc.Document, latest sqlc.DocumentVersion) error {
	base, err := t.s.documentBase(ctx, doc)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	sawDocument(ctx, doc.ID, latest.Version)
	const refused = "The user edited this document since your version %d: rewriting it all from your text would undo " +
		"their changes. Nothing was changed. "
	if diff, fits := documentDiff(base, latest); fits {
		return agent.Errorf(refused+"Their changes, now version %d:\n%s\nWrite the new text from theirs, or change only "+
			"what you mean to with edits.", base.Version, latest.Version, diff)
	}
	return agent.Errorf(refused+"Their text, now version %d:\nTitle: %s\n\n%s\n\nWrite the new text from theirs, or change "+
		"only what you mean to with edits.", base.Version, latest.Version, latest.Title, latest.Content)
}

func documentToolError(err error) error {
	var api *httpx.APIError
	if errors.As(err, &api) {
		switch api.Code {
		case "content_blocked":
			return agent.Errorf("Content moderation refused the document's current text, which the user wrote. Do not use it; " +
				"tell the user it could not be read and ask them to change it.")
		case "moderation_unavailable":
			return agent.Errorf("The document's text cannot be checked right now, so it cannot be read. Try again later.")
		}
	}
	return err
}
