package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// The image tool (docs/design/image-generation-plan.md): a chat model makes
// pictures with the image model the administrator chose. Every picture is the
// person's file - an attachment on the tool result's row, in storage, on their
// quota, in their library - and goes with that row.

const toolImageGenerate = "image_generate"

const (
	// maxImagePrompt bounds what one call may describe; content moderation
	// checks it whole.
	maxImagePrompt = 4000
	// maxEditSources is how many pictures one call may edit or combine.
	maxEditSources = 4
	// pictureRoom is what each picture asked for is assumed to take when the
	// person's storage is checked before anything is paid for.
	pictureRoom = 4 << 20
	// maxSourceBytes bounds a picture read to be edited.
	maxSourceBytes = 20 << 20
)

// imageOfferedKey records, for a run, whether it was offered the image tool:
// decided at its first request and kept until it ends, as the browser is.
type imageOfferedKey struct{}

// imagesMadeKey counts the pictures a run has asked for (*atomic.Int64).
type imagesMadeKey struct{}

// countersMu makes taking a run's counter one step: its calls run at once.
var countersMu sync.Mutex

// runPictures is the run's count of pictures asked for.
func runPictures(state *agent.RunState) *atomic.Int64 {
	countersMu.Lock()
	defer countersMu.Unlock()
	if counter, ok := state.Load(imagesMadeKey{}); ok {
		return counter.(*atomic.Int64)
	}
	counter := new(atomic.Int64)
	state.Store(imagesMadeKey{}, counter)
	return counter
}

// ImageTools is the image tool, for a run that may make pictures: not a
// sub-agent's (its words reach nobody), not a deep research's.
func (s *Server) ImageTools(ctx context.Context) []agent.Tool {
	scope, ok := agent.ScopeFrom(ctx)
	if !ok || scope.Subagent() || scope.Research {
		return nil
	}
	if decided, ok := scope.State.Load(imageOfferedKey{}); ok {
		if decided.(bool) {
			return []agent.Tool{imageTool{s: s}}
		}
		return nil
	}
	user, err := s.q.GetUser(ctx, scope.UserID)
	offered := err == nil && s.imageGenerationFor(ctx, user)
	scope.State.Store(imageOfferedKey{}, offered)
	if !offered {
		return nil
	}
	return []agent.Tool{imageTool{s: s}}
}

type imageTool struct{ s *Server }

type imageToolArgs struct {
	Prompt string   `json:"prompt"`
	Aspect string   `json:"aspect"`
	Count  int      `json:"count"`
	Images []string `json:"images"`
}

// ImageToolDetails is what the page shows of a call: the pictures, by id,
// and what was asked. Never read by a model.
type ImageToolDetails struct {
	Prompt string             `json:"prompt"`
	Aspect string             `json:"aspect"`
	Model  string             `json:"model,omitempty"`
	Images []ImageToolPicture `json:"images"`
}

type ImageToolPicture struct {
	ID        uuid.UUID `json:"id"`
	Handle    string    `json:"handle"`
	MediaType string    `json:"mediaType"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
}

// imageProgress is what the page is told while pictures are being made.
type imageProgress struct {
	Phase  string `json:"phase"`
	Count  int    `json:"count"`
	Aspect string `json:"aspect"`
	Prompt string `json:"prompt"`
}

func (imageTool) Definition() llm.Tool {
	return llm.Tool{
		Name: toolImageGenerate,
		Description: "Make pictures, or change pictures already in this conversation, with an image model. " +
			"Call it only when the user asks for a picture (to draw, paint, design, illustrate, render or edit one). " +
			"Write prompt as a complete, self-contained description of the picture wanted - subject, setting, style, " +
			"composition, colours, text to show - never only the change (\"make it blue\"): the image model sees " +
			"nothing of the conversation. To change or build on pictures, put their ids (img_… from this " +
			"conversation: the user's pictures are labelled [Image img_…], yours are in your earlier results) in " +
			"images, and describe the whole result in prompt. The pictures are shown to the user as they are " +
			"made; afterwards, say briefly what you made and offer to adjust it. Do not repeat them as Markdown " +
			"images or links, and do not describe each one at length.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "prompt": {"type": "string", "description": "The whole picture, described in full."},
    "aspect": {"type": "string", "enum": ["auto", "square", "landscape", "portrait"], "description": "The shape. Default auto."},
    "count": {"type": "integer", "minimum": 1, "description": "How many pictures. Default 1; ask for more only when the user wants options."},
    "images": {"type": "array", "items": {"type": "string"}, "description": "Ids of pictures in this conversation to edit or combine."}
  },
  "required": ["prompt"],
  "additionalProperties": false
}`),
	}
}

// Replayable is false: running a call again pays for its pictures again.
func (imageTool) Replayable() bool { return false }

func (t imageTool) Execute(ctx context.Context, raw json.RawMessage) (agent.ToolResult, error) {
	s := t.s
	scope, ok := agent.ScopeFrom(ctx)
	if !ok {
		return agent.ToolResult{}, errors.New("image_generate: no run in context")
	}
	if scope.Subagent() {
		return agent.ToolResult{}, agent.Errorf("image_generate is not available here")
	}
	var args imageToolArgs
	if err := agent.DecodeArgs(raw, &args); err != nil {
		return agent.ToolResult{}, err
	}
	prompt := strings.TrimSpace(args.Prompt)
	switch {
	case prompt == "":
		return agent.ToolResult{}, agent.Errorf("invalid arguments: prompt must describe the picture")
	case utf8.RuneCountInString(prompt) > maxImagePrompt:
		return agent.ToolResult{}, agent.Errorf("invalid arguments: prompt is at most %d characters", maxImagePrompt)
	case len(args.Images) > maxEditSources:
		return agent.ToolResult{}, agent.Errorf("invalid arguments: at most %d pictures to edit at once", maxEditSources)
	}
	config, err := loadImageConfig(ctx, s.q)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !config.Enabled || config.Model == "" {
		return refusedPicture("Image generation has been switched off by the administrator."), nil
	}
	count := max(args.Count, 1)
	if count > config.MaxPerCall {
		return agent.ToolResult{}, agent.Errorf("invalid arguments: at most %d pictures per call", config.MaxPerCall)
	}
	aspect := llm.ParseAspect(args.Aspect)

	// The run's own limit, counted before anything is paid for and given back
	// when nothing is made.
	inRun := runPictures(scope.State)
	if inRun.Add(int64(count)) > int64(config.MaxPerRun) {
		inRun.Add(-int64(count))
		return refusedPicture(fmt.Sprintf("This answer has already made as many pictures as one answer may (%d). Tell the user; they can ask again in a new message.", config.MaxPerRun)), nil
	}
	kept := false
	defer func() {
		if !kept {
			inRun.Add(-int64(count))
		}
	}()

	user, err := s.q.GetUser(ctx, scope.UserID)
	if err != nil {
		return agent.ToolResult{}, err
	}
	admin := user.Role == sqlc.UserRoleAdmin
	conversationID := scope.ConversationID

	sources, err := s.pictureSources(ctx, scope, args.Images)
	if err != nil {
		return agent.ToolResult{}, err
	}

	// Room for the pictures before they are paid for. Counted again, exactly,
	// when each is stored.
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return roomFor(ctx, s.q.WithTx(tx), user.ID, int64(count)*pictureRoom)
	}); err != nil {
		if errors.Is(err, errStorageFull) {
			return refusedPicture("The user's file storage is full, so no picture can be kept. Tell them to delete some files (Settings → Data) and ask again."), nil
		}
		return agent.ToolResult{}, err
	}

	// The prompt goes out under the operator's account: it is checked like the
	// person's own words, but a refusal is not counted against them - the
	// model wrote it (image_prompt).
	who := moderationSubject{userID: user.ID, admin: admin, conversationID: &conversationID, source: ModerationSourceImagePrompt}
	if err := s.moderate(ctx, who, moderationInput{text: prompt}); err != nil {
		if isContentBlocked(err) {
			return refusedPicture("This prompt did not pass content moderation, so no picture was made. Tell the user plainly; do not try to get around it with other wording."), nil
		}
		return refusedPicture("Content moderation is unavailable right now, so no picture could be made. Tell the user to try again later."), nil
	}

	// The day's allowance, taken under the person's lock, given back on failure.
	requestID, refusal, err := s.takeImageAllowance(ctx, user, scope.RunID, count, config)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if refusal != "" {
		return refusedPicture(refusal), nil
	}
	finished := false
	defer func() {
		if !finished {
			s.finishImageRequest(ctx, requestID, "failed", count)
		}
	}()

	_ = agent.ReportProgress(ctx, imageProgress{Phase: "generating", Count: count, Aspect: string(aspect), Prompt: prompt})
	runID := scope.RunID
	result, err := s.makePictures(ctx, pictureJob{
		user: user, modelID: config.Model, who: asker{staff: admin, admin: admin},
		req:   llm.ImageRequest{Prompt: prompt, Count: count, Aspect: aspect, Images: sources},
		runID: &runID, conversationID: &conversationID,
	})
	if err != nil {
		if ctx.Err() != nil {
			return agent.ToolResult{}, ctx.Err()
		}
		slog.WarnContext(ctx, "image_generate failed", "model", config.Model, "error", err)
		return refusedPicture("No picture was made: " + pictureFailure(err) + ". Tell the user."), nil
	}

	pictures, err := s.keepPictures(ctx, user.ID, runID, result.images)
	if err != nil && len(pictures) == 0 {
		if errors.Is(err, errStorageFull) {
			return refusedPicture("The pictures were made, but the user's file storage is full, so they could not be kept. Tell them to delete some files and ask again."), nil
		}
		return agent.ToolResult{}, err
	}
	finished = true
	kept = true
	s.finishImageRequest(ctx, requestID, "done", len(pictures))

	details := ImageToolDetails{Prompt: prompt, Aspect: string(aspect), Model: result.model, Images: pictures}
	ids := make([]uuid.UUID, 0, len(pictures))
	var b strings.Builder
	fmt.Fprintf(&b, "Made %d picture%s, shown to the user above your reply:", len(pictures), plural(len(pictures)))
	for _, p := range pictures {
		ids = append(ids, p.ID)
		fmt.Fprintf(&b, " %s (%d×%d)", p.Handle, p.Width, p.Height)
	}
	b.WriteString(". Do not repeat them as Markdown or links. To change one, call image_generate again with its id in images and a full prompt.")
	if len(pictures) < count {
		fmt.Fprintf(&b, " (%d of the %d asked for were made.)", len(pictures), count)
	}
	return agent.ToolResult{Content: b.String(), Details: details, Files: ids}, nil
}

// refusedPicture is a result that says why no picture was made, for the model
// to tell the person.
func refusedPicture(why string) agent.ToolResult {
	return agent.ToolResult{Content: why, IsError: true, Details: ImageToolDetails{Images: []ImageToolPicture{}}}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// pictureSources reads the pictures a call names to edit: this conversation's
// (the person's, as moderation saw them, and the ones made here), each
// checked against the receipt taken when it was sent or stored.
func (s *Server) pictureSources(ctx context.Context, scope agent.Scope, handles []string) ([]llm.ImageInput, error) {
	if len(handles) == 0 {
		return nil, nil
	}
	rows, err := s.q.ListConversationImages(ctx, sqlc.ListConversationImagesParams{ConversationID: scope.ConversationID, UserID: &scope.UserID})
	if err != nil {
		return nil, err
	}
	out := make([]llm.ImageInput, 0, len(handles))
	for _, handle := range handles {
		handle = strings.TrimSpace(handle)
		var found *sqlc.ListConversationImagesRow
		for i := range rows {
			if agent.ImageHandle(rows[i].ID) == handle {
				found = &rows[i]
				break
			}
		}
		if found == nil {
			return nil, agent.Errorf("invalid arguments: %q is not a picture in this conversation", handle)
		}
		data, err := s.store.Read(ctx, found.StorageKey, maxSourceBytes)
		if err != nil {
			return nil, agent.Errorf("the picture %s cannot be read", handle)
		}
		if want := receiptOf(*found); want != "" {
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != want {
				return nil, agent.Errorf("the picture %s changed after it was checked, so it cannot be used", handle)
			}
		}
		mediaType, _, _, ok := llm.SniffPicture(data)
		if !ok {
			return nil, agent.Errorf("%s is not a PNG, JPEG or WebP picture", handle)
		}
		out = append(out, llm.ImageInput{Filename: found.Filename, MediaType: mediaType, Data: data})
	}
	return out, nil
}

// receiptOf is the digest a picture must still have: the server's own for a
// picture made here, moderation's for one the person sent (in the message's
// parts). Empty when nothing was recorded - moderation off, say.
func receiptOf(row sqlc.ListConversationImagesRow) string {
	if row.Sha256 != nil {
		return *row.Sha256
	}
	parts, err := agent.DecodeParts(row.MessageParts)
	if err != nil {
		return ""
	}
	for _, part := range parts {
		if file, ok := part.(llm.AttachmentPart); ok && file.Ref == row.ID.String() {
			return file.SHA256
		}
	}
	return ""
}

// takeImageAllowance takes count pictures of the person's day, or says why
// not. Administrators have no daily limit; their requests are still recorded.
func (s *Server) takeImageAllowance(ctx context.Context, user sqlc.User, runID uuid.UUID, count int, config imageConfig) (uuid.UUID, string, error) {
	limit := config.Daily
	if user.ImageDaily != nil {
		limit = int(*user.ImageDaily)
	}
	if user.Role == sqlc.UserRoleAdmin {
		limit = 0
	}
	var id uuid.UUID
	var refusal string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockImageQuota(ctx, user.ID); err != nil {
			return err
		}
		if limit > 0 {
			used, err := q.ImagesSince(ctx, sqlc.ImagesSinceParams{UserID: user.ID, Since: startOfDay(time.Now())})
			if err != nil {
				return err
			}
			if left := int64(limit) - used; left < int64(count) {
				if left <= 0 {
					refusal = fmt.Sprintf("The user has used today's allowance of %d pictures. Tell them it renews tomorrow.", limit)
				} else {
					refusal = fmt.Sprintf("The user has %d picture%s left today; ask for no more than that.", left, plural(int(left)))
				}
				return nil
			}
		}
		var err error
		id, err = q.InsertImageRequest(ctx, sqlc.InsertImageRequestParams{UserID: user.ID, RunID: &runID, Count: int32(count)})
		return err
	})
	return id, refusal, err
}

func (s *Server) finishImageRequest(ctx context.Context, id uuid.UUID, status string, count int) {
	if id == uuid.Nil {
		return
	}
	if err := s.q.FinishImageRequest(context.WithoutCancel(ctx), sqlc.FinishImageRequestParams{ID: id, Status: status, Count: int32(count)}); err != nil {
		slog.WarnContext(ctx, "finish image request", "error", err)
	}
}

// startOfDay is local midnight, in the server's time zone (TZ), as the
// spending caps count days.
func startOfDay(now time.Time) pgtype.Timestamptz {
	y, m, d := now.Date()
	return pgtype.Timestamptz{Time: time.Date(y, m, d, 0, 0, 0, 0, now.Location()), Valid: true}
}

// keepPictures stores each picture as the person's file: a row counted
// against their quota (roomFor, in the row's transaction), then the bytes. A
// picture that cannot be kept is left out; err says why the first one was.
func (s *Server) keepPictures(ctx context.Context, userID, runID uuid.UUID, images []llm.GeneratedImage) ([]ImageToolPicture, error) {
	var out []ImageToolPicture
	var first error
	for i, picture := range images {
		mediaType, width, height, ok := llm.SniffPicture(picture.Data)
		if !ok {
			if first == nil {
				first = errors.New("api: a generated picture is not a picture")
			}
			continue
		}
		id := uuid.New()
		key := fmt.Sprintf("uploads/%s/%s%s", time.Now().UTC().Format("2006/01/02"), id, pictureExt(mediaType))
		sum := sha256.Sum256(picture.Data)
		digest := hex.EncodeToString(sum[:])
		w32, h32 := int32(width), int32(height)
		name := fmt.Sprintf("image-%s-%d%s", time.Now().Format("20060102-150405"), i+1, pictureExt(mediaType))
		var row sqlc.Attachment
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			q := s.q.WithTx(tx)
			if err := roomFor(ctx, q, userID, int64(len(picture.Data))); err != nil {
				return err
			}
			var err error
			row, err = q.CreateGeneratedImage(ctx, sqlc.CreateGeneratedImageParams{
				UserID: &userID, StorageKey: key, Filename: name, ContentType: mediaType,
				SizeBytes: int64(len(picture.Data)), RunID: &runID, Sha256: &digest, Width: &w32, Height: &h32,
			})
			return err
		})
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := s.store.PutObject(context.WithoutCancel(ctx), key, mediaType, picture.Data); err != nil {
			slog.WarnContext(ctx, "store generated picture", "error", err)
			_ = s.q.DeleteAttachment(context.WithoutCancel(ctx), row.ID)
			if first == nil {
				first = err
			}
			continue
		}
		out = append(out, ImageToolPicture{ID: row.ID, Handle: agent.ImageHandle(row.ID), MediaType: mediaType, Width: width, Height: height})
	}
	return out, first
}

func pictureExt(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ".png"
}

// imageGuide is the system prompt's word on pictures, for a run offered the
// image tool.
const imageGuide = "\n\n## Pictures\n" +
	"You can make and edit pictures with image_generate. Use it when the user asks for a picture, not on your own " +
	"initiative. Pictures the user sent are labelled [Image img_…] in their messages; to change or build on one, pass " +
	"its id in images and describe the whole picture wanted in prompt. When the user asks to change a picture you made, " +
	"edit that picture (its id is in your earlier result) rather than starting over, unless they want something new. " +
	"After the pictures are made, reply in a sentence or two; they are already shown to the user."

// ViewFile redirects to where one of the caller's files is shown from.
func (s *Server) ViewFile(w http.ResponseWriter, r *http.Request, fileId FileId) {
	user := auth.MustUser(r.Context())
	row, err := s.q.GetOwnAttachment(r.Context(), sqlc.GetOwnAttachmentParams{ID: fileId, UserID: &user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	url, err := s.store.PresignGet(r.Context(), row.StorageKey, attachmentTTL)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The address is signed for a while; the redirect may be kept as long.
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.Redirect(w, r, url, http.StatusFound)
}
