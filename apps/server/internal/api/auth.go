package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

const minPasswordLength = 8

// uniqueViolation is Postgres' code for a duplicate key.
const uniqueViolation = "23505"

func (s *Server) secureCookies() bool {
	// Behind a TLS-terminating proxy the request itself is plain HTTP, so the
	// environment decides rather than r.TLS.
	return s.cfg.IsProduction()
}

// issueSession creates a session row and sets the cookie for it.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, userID uuid.UUID) error {
	token, hash, err := auth.NewToken()
	if err != nil {
		return err
	}

	if _, err := s.q.CreateSession(r.Context(), sqlc.CreateSessionParams{
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(auth.SessionTTL), Valid: true},
		UserAgent: truncate(r.UserAgent(), 400),
	}); err != nil {
		return err
	}

	auth.SetCookie(w, token, s.secureCookies())
	return nil
}

func (s *Server) Register(w http.ResponseWriter, r *http.Request) {
	var body RegisterRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	email := strings.TrimSpace(string(body.Email))
	name := strings.TrimSpace(body.Name)

	switch {
	case email == "":
		httpx.Fail(w, r, httpx.NewFieldError("email", "email_required", "email is required"))
		return
	case name == "":
		httpx.Fail(w, r, httpx.NewFieldError("name", "name_required", "name is required"))
		return
	case len(body.Password) < minPasswordLength:
		httpx.Fail(w, r, httpx.NewFieldError("password", "password_short", "password is too short"))
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	user, err := s.q.RegisterUser(r.Context(), sqlc.RegisterUserParams{
		Email:        email,
		Name:         name,
		PasswordHash: hash,
	})
	if err != nil {
		if dup := duplicateError(err); dup != nil {
			httpx.Fail(w, r, dup)
			return
		}
		httpx.Fail(w, r, err)
		return
	}

	if err := s.issueSession(w, r, user.ID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toSessionUser(user))
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var body LoginRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// One error for every failure mode below. Distinguishing "no such account"
	// from "wrong password" would let anyone probe for registered addresses.
	invalid := httpx.NewError(http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")

	user, err := s.q.GetUserByEmail(r.Context(), strings.TrimSpace(string(body.Email)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Spend the same time as a real verification, so the response
			// duration does not reveal whether the address exists.
			auth.DummyVerify(body.Password)
			httpx.Fail(w, r, invalid)
			return
		}
		httpx.Fail(w, r, err)
		return
	}

	if err := auth.VerifyPassword(body.Password, user.PasswordHash); err != nil {
		if errors.Is(err, auth.ErrMismatch) || errors.Is(err, auth.ErrInvalidHash) {
			httpx.Fail(w, r, invalid)
			return
		}
		httpx.Fail(w, r, err)
		return
	}

	if user.DisabledAt.Valid {
		httpx.Fail(w, r, httpx.NewError(http.StatusForbidden, "account_disabled", "this account has been disabled"))
		return
	}

	if err := s.issueSession(w, r, user.ID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toSessionUser(user))
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	if token := auth.TokenFromRequest(r); token != "" {
		if err := s.q.DeleteSession(r.Context(), auth.HashToken(token)); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	auth.ClearCookie(w, s.secureCookies())
	httpx.NoContent(w)
}

func (s *Server) GetSession(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	// Cheap activity signal for the admin table. Only this endpoint writes it,
	// rather than every authenticated request, which would put a write on the
	// hot path for no benefit.
	if err := s.q.TouchUserSeen(r.Context(), user.ID); err != nil {
		// Not worth failing the request over.
		logWarn(r, "touch last_seen_at", err)
	}

	httpx.JSON(w, http.StatusOK, toSessionUser(user))
}

// UpdateMe changes the signed-in account's own name or email.
//
// Note what it cannot reach: role and disabled_at are not parameters of
// UpdateOwnProfile at all, so no combination of input here can raise the
// caller's own privileges.
func (s *Server) UpdateMe(w http.ResponseWriter, r *http.Request) {
	var body UpdateMeRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Name == nil && body.Email == nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "nothing_to_update", "no changes were given"))
		return
	}

	var name, email *string

	if body.Name != nil {
		trimmed := strings.TrimSpace(*body.Name)
		if trimmed == "" {
			httpx.Fail(w, r, httpx.NewFieldError("name", "name_required", "name is required"))
			return
		}
		name = &trimmed
	}

	if body.Email != nil {
		trimmed := strings.TrimSpace(string(*body.Email))
		if trimmed == "" {
			httpx.Fail(w, r, httpx.NewFieldError("email", "email_required", "email is required"))
			return
		}
		email = &trimmed
	}

	user := auth.MustUser(r.Context())
	updated, err := s.q.UpdateOwnProfile(r.Context(), sqlc.UpdateOwnProfileParams{
		ID:    user.ID,
		Name:  name,
		Email: email,
	})
	if err != nil {
		if dup := duplicateError(err); dup != nil {
			httpx.Fail(w, r, dup)
			return
		}
		httpx.Fail(w, r, dbErr(err))
		return
	}

	httpx.JSON(w, http.StatusOK, toSessionUser(updated))
}

// ChangePassword replaces the signed-in account's own password.
//
// The current password is required, which the admin endpoint does not ask for:
// an admin is acting on an account they do not own, while the whole point here
// is to prove the person at the keyboard is the account holder and not someone
// who found the machine unlocked.
func (s *Server) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var body ChangePasswordRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(body.NewPassword) < minPasswordLength {
		httpx.Fail(w, r, httpx.NewFieldError("newPassword", "password_short", "password is too short"))
		return
	}

	user := auth.MustUser(r.Context())

	if err := auth.VerifyPassword(body.CurrentPassword, user.PasswordHash); err != nil {
		if errors.Is(err, auth.ErrMismatch) || errors.Is(err, auth.ErrInvalidHash) {
			httpx.Fail(w, r, httpx.NewFieldError("currentPassword", "invalid_credentials",
				"that is not your current password"))
			return
		}
		httpx.Fail(w, r, err)
		return
	}

	hash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	if err := s.q.UpdateUserPassword(r.Context(), sqlc.UpdateUserPasswordParams{
		ID:           user.ID,
		PasswordHash: hash,
	}); err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}

	// Sessions issued under the old password go, except this one — signing
	// someone out of the tab they just used to change their password is
	// punishment, not security. If the context has no session id the middleware
	// did not run, which cannot happen behind Require.
	if sessionID, ok := auth.SessionIDFrom(r.Context()); ok {
		if err := s.q.DeleteOtherUserSessions(r.Context(), sqlc.DeleteOtherUserSessionsParams{
			UserID: user.ID,
			KeepID: sessionID,
		}); err != nil {
			logWarn(r, "revoke other sessions after password change", err)
		}
	}

	httpx.NoContent(w)
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
