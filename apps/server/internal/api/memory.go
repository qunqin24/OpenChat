package api

import (
	"errors"
	"net/http"
	"unicode/utf8"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/memory"
)

// A person's memory: custom instructions, the list of notes, and the log of
// what the model changed. Every handler here acts on the signed-in user's own
// rows and nobody else's — the user id comes from the session, never from the
// request — and none of it is reachable from the admin group: an administrator
// reads a person's memories no more than their messages.

func (s *Server) notes() *memory.Store { return memory.NewStore(s.pool) }

func (s *Server) GetPersonalization(w http.ResponseWriter, r *http.Request) {
	out, err := s.personalization(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) personalization(r *http.Request) (Personalization, error) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	p, err := s.q.GetPersonalization(ctx, user.ID)
	if err != nil {
		return Personalization{}, err
	}
	allowed, err := s.notes().GlobalOn(ctx)
	if err != nil {
		return Personalization{}, err
	}
	return Personalization{
		AboutYou:         p.InstructionsAbout,
		ResponseStyle:    p.InstructionsStyle,
		MemoryEnabled:    p.MemoryEnabled,
		MemoryAllowed:    allowed,
		InstructionLimit: memory.MaxInstructions,
		MemoryLimit:      memory.MaxMemories,
		MemoryLength:     memory.MaxContent,
	}, nil
}

func (s *Server) UpdatePersonalization(w http.ResponseWriter, r *http.Request) {
	var body UpdatePersonalizationRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for field, value := range map[string]*string{"aboutYou": body.AboutYou, "responseStyle": body.ResponseStyle} {
		if value != nil && utf8.RuneCountInString(*value) > memory.MaxInstructions {
			httpx.Fail(w, r, httpx.NewFieldError(field, "instructions_too_long", "the instructions are too long"))
			return
		}
	}
	ctx := r.Context()
	user := auth.MustUser(ctx)
	current, err := s.q.GetPersonalization(ctx, user.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// A field that changed is moderated before anything is saved, like a message
	// is: the first refusal answers, and nothing is written. One sent back as it
	// is stays as it was (checked, or to be checked when a conversation starts).
	var aboutChecked, styleChecked *string
	for _, field := range []struct {
		value   *string
		current string
		checked **string
	}{
		{body.AboutYou, current.InstructionsAbout, &aboutChecked},
		{body.ResponseStyle, current.InstructionsStyle, &styleChecked},
	} {
		if field.value == nil || *field.value == field.current {
			continue
		}
		checked, err := s.moderateSaved(ctx, user, ModerationSourceInstructions, *field.value)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if checked {
			hash := memory.Hash(*field.value)
			*field.checked = &hash
		}
	}
	if _, err := s.q.UpdatePersonalization(ctx, sqlc.UpdatePersonalizationParams{
		ID:                user.ID,
		InstructionsAbout: body.AboutYou,
		InstructionsStyle: body.ResponseStyle,
		MemoryEnabled:     body.MemoryEnabled,
		AboutCheckedHash:  aboutChecked,
		StyleCheckedHash:  styleChecked,
	}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s.GetPersonalization(w, r)
}

func (s *Server) ListMemories(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	rows, err := s.notes().List(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := MemoryList{Items: make([]Memory, 0, len(rows))}
	for _, row := range rows {
		out.Items = append(out.Items, toMemory(row))
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) CreateMemory(w http.ResponseWriter, r *http.Request) {
	var body MemoryRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	user := auth.MustUser(ctx)
	content, err := memory.Clean(body.Content)
	if err != nil {
		httpx.Fail(w, r, memoryErr(err))
		return
	}
	checked, err := s.moderateSaved(ctx, user, ModerationSourceMemory, content)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	change, err := s.notes().Add(ctx, user.ID, content, memory.By{Source: memory.SourceUser, Checked: checked})
	if err != nil {
		httpx.Fail(w, r, memoryErr(err))
		return
	}
	httpx.JSON(w, http.StatusCreated, toMemory(change.Memory))
}

func (s *Server) UpdateMemory(w http.ResponseWriter, r *http.Request, memoryId openapi_types.UUID) {
	var body MemoryRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	user := auth.MustUser(ctx)
	content, err := memory.Clean(body.Content)
	if err != nil {
		httpx.Fail(w, r, memoryErr(err))
		return
	}
	checked, err := s.moderateSaved(ctx, user, ModerationSourceMemory, content)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	change, err := s.notes().Update(ctx, user.ID, memoryId, content, memory.By{Source: memory.SourceUser, Checked: checked})
	if err != nil {
		httpx.Fail(w, r, memoryErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, toMemory(change.Memory))
}

func (s *Server) DeleteMemory(w http.ResponseWriter, r *http.Request, memoryId openapi_types.UUID) {
	user := auth.MustUser(r.Context())
	if _, err := s.notes().Delete(r.Context(), user.ID, memoryId, memory.By{Source: memory.SourceUser}); err != nil {
		httpx.Fail(w, r, memoryErr(err))
		return
	}
	httpx.NoContent(w)
}

func (s *Server) ClearMemories(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := s.notes().Clear(r.Context(), user.ID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (s *Server) ListMemoryEvents(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	rows, err := s.notes().Events(r.Context(), user.ID, 50)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := MemoryEventList{Items: make([]MemoryEvent, 0, len(rows))}
	for _, row := range rows {
		out.Items = append(out.Items, toMemoryEvent(row))
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) UndoMemoryEvent(w http.ResponseWriter, r *http.Request, eventId openapi_types.UUID) {
	user := auth.MustUser(r.Context())
	event, err := s.notes().Undo(r.Context(), user.ID, eventId)
	if err != nil {
		httpx.Fail(w, r, memoryErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, toMemoryEvent(event))
}

// memoryErr turns what the store refuses into the contract's errors.
func memoryErr(err error) error {
	switch {
	case errors.Is(err, memory.ErrEmpty):
		return httpx.NewFieldError("content", "memory_empty", err.Error())
	case errors.Is(err, memory.ErrTooLong):
		return httpx.NewFieldError("content", "memory_too_long", err.Error())
	case errors.Is(err, memory.ErrFull):
		return httpx.NewError(http.StatusConflict, "memory_full", err.Error())
	case errors.Is(err, memory.ErrDuplicate):
		return httpx.NewError(http.StatusConflict, "memory_duplicate", err.Error())
	case errors.Is(err, memory.ErrChanged):
		return httpx.NewError(http.StatusConflict, "memory_changed", err.Error())
	case errors.Is(err, memory.ErrUndone):
		return httpx.NewError(http.StatusConflict, "already_undone", err.Error())
	case errors.Is(err, memory.ErrNotFound):
		return httpx.ErrNotFound
	}
	return err
}

func toMemory(row sqlc.UserMemory) Memory {
	return Memory{
		Id:             row.ID,
		Content:        row.Content,
		Source:         MemorySource(row.Source),
		ConversationId: row.ConversationID,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}

func toMemoryEvent(row sqlc.MemoryEvent) MemoryEvent {
	out := MemoryEvent{
		Id:             row.ID,
		MemoryId:       row.MemoryID,
		Op:             MemoryEventOp(row.Op),
		Before:         row.Before,
		After:          row.After,
		Source:         MemorySource(row.Source),
		ConversationId: row.ConversationID,
		CreatedAt:      row.CreatedAt.Time,
	}
	if row.UndoneAt.Valid {
		out.UndoneAt = &row.UndoneAt.Time
	}
	return out
}
