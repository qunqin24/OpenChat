package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// Cursors are opaque to clients: base64url("<RFC3339Nano timestamp>|<uuid>").
// Keyset pagination on (timestamp, id) keeps pages stable while rows are inserted.

func encodeCursor(ts pgtype.Timestamptz, id uuid.UUID) string {
	raw := fmt.Sprintf("%s|%s", ts.Time.UTC().Format(time.RFC3339Nano), id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor *string) (pgtype.Timestamptz, *uuid.UUID, error) {
	if cursor == nil || *cursor == "" {
		return pgtype.Timestamptz{}, nil, nil
	}

	invalid := httpx.NewError(http.StatusBadRequest, "invalid_cursor", "cursor is not valid")

	raw, err := base64.RawURLEncoding.DecodeString(*cursor)
	if err != nil {
		return pgtype.Timestamptz{}, nil, invalid
	}
	ts, idPart, ok := strings.Cut(string(raw), "|")
	if !ok {
		return pgtype.Timestamptz{}, nil, invalid
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return pgtype.Timestamptz{}, nil, invalid
	}
	id, err := uuid.Parse(idPart)
	if err != nil {
		return pgtype.Timestamptz{}, nil, invalid
	}
	return pgtype.Timestamptz{Time: t, Valid: true}, &id, nil
}

// clampLimit keeps page sizes inside the bounds declared in the OpenAPI spec.
func clampLimit(limit *int) int32 {
	const (
		def = 50
		max = 100
	)
	if limit == nil || *limit <= 0 {
		return def
	}
	if *limit > max {
		return max
	}
	return int32(*limit)
}
