package api

import (
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin/openchat/apps/server/internal/chat"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

func toConversation(row sqlc.Conversation) Conversation {
	return Conversation{
		Id:        row.ID,
		Title:     row.Title,
		Model:     row.Model,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

func toMessage(row sqlc.Message, attachments []Attachment) Message {
	m := Message{
		Id:             row.ID,
		ConversationId: row.ConversationID,
		Role:           Role(row.Role),
		Content:        row.Content,
		Status:         MessageStatus(row.Status),
		CreatedAt:      row.CreatedAt.Time,
	}
	if len(attachments) > 0 {
		m.Attachments = &attachments
	}
	return m
}

func toChatMessages(rows []sqlc.Message) []chat.Message {
	out := make([]chat.Message, 0, len(rows))
	for _, row := range rows {
		out = append(out, chat.Message{
			Role:    chat.Role(row.Role),
			Content: row.Content,
		})
	}
	return out
}

func toSessionUser(row sqlc.User) SessionUser {
	return SessionUser{
		Id:        row.ID,
		Email:     openapi_types.Email(row.Email),
		Name:      row.Name,
		Role:      UserRole(row.Role),
		AvatarUrl: avatarURL(row.Email),
	}
}

func toUser(row sqlc.User) User {
	user := User{
		Id:        row.ID,
		Email:     openapi_types.Email(row.Email),
		Name:      row.Name,
		Role:      UserRole(row.Role),
		Disabled:  row.DisabledAt.Valid,
		CreatedAt: row.CreatedAt.Time,
		AvatarUrl: avatarURL(row.Email),
	}
	if row.LastSeenAt.Valid {
		seen := row.LastSeenAt.Time
		user.LastSeenAt = &seen
	}
	return user
}
