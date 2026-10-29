package api

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

func toConversation(row sqlc.Conversation) Conversation {
	effort := toEffort(row.ReasoningEffort)
	return Conversation{
		Id:              row.ID,
		Title:           row.Title,
		Model:           row.Model,
		ReasoningEffort: &effort,
		WebSearch:       row.WebSearch,
		Temporary:       row.Temporary,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
		PinnedAt:        timePtr(row.PinnedAt),
	}
}

// timePtr is a nullable timestamp column as an optional field.
func timePtr(column pgtype.Timestamptz) *time.Time {
	if !column.Valid {
		return nil
	}
	at := column.Time
	return &at
}

// toEffort reads a reasoning_effort column, where NULL is automatic.
func toEffort(column *string) ReasoningEffort {
	if column == nil {
		return ReasoningEffortAuto
	}
	return ReasoningEffort(*column)
}

// effortColumn is the column value for a requested effort: NULL for automatic.
// ok is false for a value outside the enum.
func effortColumn(effort ReasoningEffort) (*string, bool) {
	switch effort {
	case ReasoningEffortAuto:
		return nil, true
	case ReasoningEffortNone, ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium,
		ReasoningEffortHigh, ReasoningEffortXhigh, ReasoningEffortMax:
		value := string(effort)
		return &value, true
	}
	return nil, false
}

var errInvalidEffort = httpx.NewFieldError("reasoningEffort", "invalid_reasoning_effort",
	"reasoningEffort must be auto or a reasoning level")

// sessionUser is an account as its owner sees it.
func (s *Server) sessionUser(ctx context.Context, row sqlc.User) SessionUser {
	return SessionUser{
		Id:        row.ID,
		Email:     openapi_types.Email(row.Email),
		Name:      row.Name,
		Role:      UserRole(row.Role),
		AvatarUrl: s.avatarURL(ctx, row.AvatarKey),
		TimeZone:  row.TimeZone,
	}
}

// user is an account as an administrator sees it.
func (s *Server) user(ctx context.Context, row sqlc.User) User {
	user := User{
		Id:        row.ID,
		Email:     openapi_types.Email(row.Email),
		Name:      row.Name,
		Role:      UserRole(row.Role),
		AvatarUrl: s.avatarURL(ctx, row.AvatarKey),
		Disabled:  row.DisabledAt.Valid,
		Pending:   row.Pending,
		CreatedAt: row.CreatedAt.Time,
	}
	if row.LastSeenAt.Valid {
		seen := row.LastSeenAt.Time
		user.LastSeenAt = &seen
	}
	user.StorageQuotaBytes = row.StorageQuotaBytes
	if row.DisabledAt.Valid && row.DisabledReason != nil {
		reason := DisabledReason(*row.DisabledReason)
		user.DisabledReason = &reason
		user.DisabledNote = row.DisabledNote
	}
	return user
}
