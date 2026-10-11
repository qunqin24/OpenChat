package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// End-user identifiers.
//
// Everything a person sends to a model goes out under the operator's own
// account with the provider, and an account is banned for what its users send.
// Providers ask for an identifier of the end user to tell one user's abuse from
// another's (OpenAI's safety_identifier, Anthropic's metadata.user_id), so that
// they can act on the one and leave the account alone.
//
// The identifier is a keyed hash: HMAC-SHA256 of the account's id, or for a
// guest of `guest:` and the address their requests come from (clientIP), with a
// secret of the server's own, hex, cut to endUserLength characters. It is stable
// (the same person is the same identifier for as long as the secret is), says
// nothing about who they are, and cannot be turned back into an id or an
// address by anyone without the secret. Never the email or the name.

// settingEndUserKey holds the secret: 32 random bytes, hex, made on first use
// and kept in app_settings. No API reads or writes it. Replacing it would make
// every user a stranger to the providers' history, so nothing does.
const settingEndUserKey = "end_user_key"

// endUserLength is how many hex characters of the MAC are sent: 128 bits, well
// inside the 64 characters OpenAI allows.
const endUserLength = 32

// EndUsers makes the identifiers.
type EndUsers struct {
	q *sqlc.Queries

	mu  sync.Mutex
	key []byte
}

func NewEndUsers(pool *pgxpool.Pool) *EndUsers {
	return &EndUsers{q: sqlc.New(pool)}
}

// secret is the key, read once and kept. A read that fails is retried at the
// next request, not remembered.
//
// Two processes starting together both find it missing and both make one;
// InsertSettingIfMissing lets only the first write stand, and reading back is
// how each learns which that was (as storageSigningKey does).
func (e *EndUsers) secret(ctx context.Context) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.key != nil {
		return e.key, nil
	}
	key, err := stringSetting(ctx, e.q, settingEndUserKey)
	if err != nil {
		return nil, err
	}
	if key == "" {
		fresh := make([]byte, 32)
		if _, err := rand.Read(fresh); err != nil {
			return nil, err
		}
		value, _ := json.Marshal(hex.EncodeToString(fresh))
		if err := e.q.InsertSettingIfMissing(ctx, sqlc.InsertSettingIfMissingParams{Key: settingEndUserKey, Value: value}); err != nil {
			return nil, err
		}
		if key, err = stringSetting(ctx, e.q, settingEndUserKey); err != nil {
			return nil, err
		}
	}
	decoded, err := hex.DecodeString(key)
	if err != nil || len(decoded) < 32 {
		return nil, errors.New("the stored end-user key is malformed")
	}
	e.key = decoded
	return decoded, nil
}

// of hashes a subject. An identifier that cannot be made is "", which sends
// none: a request is not worth refusing for the want of a hint to the provider.
func (e *EndUsers) of(ctx context.Context, subject string) string {
	if e == nil {
		return ""
	}
	key, err := e.secret(ctx)
	if err != nil {
		slog.WarnContext(ctx, "end-user identifier unavailable; the request goes without one", "error", err)
		return ""
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(subject))
	return hex.EncodeToString(mac.Sum(nil))[:endUserLength]
}

// Account is an account's identifier.
func (e *EndUsers) Account(ctx context.Context, userID uuid.UUID) string {
	if userID == uuid.Nil {
		return ""
	}
	return e.of(ctx, userID.String())
}

// Guest is a guest's identifier: all guests behind one address are one, which
// is also the unit they are limited and banned by.
func (e *EndUsers) Guest(ctx context.Context, ip string) string {
	if ip == "" {
		return ""
	}
	return e.of(ctx, "guest:"+ip)
}

// ForRun is the identifier of the person a run is for: the address it came
// from when it is a guest's (createRun records it), else the account.
func (e *EndUsers) ForRun(ctx context.Context, run sqlc.Run) string {
	if run.ClientIp != nil && *run.ClientIp != "" {
		return e.Guest(ctx, *run.ClientIp)
	}
	return e.Account(ctx, run.UserID)
}
