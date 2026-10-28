package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// Passkeys: WebAuthn credentials an account signs in with instead of its
// password. A sign-in names no account - the passkey the person picks says
// whose it is (a discoverable credential) - and then goes the way a password
// sign-in goes from "the password is right": the same failure counting
// (passkeyLimit, counted before the answer is checked and given back when it
// verifies), the same refusal of a pending or disabled account, the same
// session (issueSession).
//
// A ceremony's challenge is kept in webauthn_challenges for five minutes and
// taken by the first answer, good or bad, so an answer cannot be replayed.
// The relying party - the domain passkeys are bound to, and the origins an
// answer may come from - is the administrator's to pin; left empty it is the
// page's own, read from the request (the browser itself refuses a passkey to
// a page of another domain).

const (
	// settingPasskeys holds the passkey settings, as JSON.
	settingPasskeys = "passkeys"
	// passkeyNameMax is the longest name a passkey can be given.
	passkeyNameMax = 60
	// challengeKeep is how long an unanswered challenge is kept.
	challengeKeep = 5 * time.Minute
)

// passkeyLimit counts failed passkey sign-ins, as loginLimit counts failed
// passwords: per address only, since an answer names no account up front.
var passkeyLimit = attemptLimit{
	kind: "passkey", perIP: 10, window: 15 * time.Minute,
	code: "login_rate_limited", message: "too many failed sign-in attempts; try again later",
}

// passkeyConfig is the stored form of the settings.
type passkeyConfig struct {
	Enabled          bool     `json:"enabled"`
	RPID             string   `json:"rpId"`
	Origins          []string `json:"origins"`
	UserVerification string   `json:"userVerification"`
}

// loadPasskeys is the settings: on, the page's own domain, verification
// preferred, until an administrator says otherwise.
func loadPasskeys(ctx context.Context, q *sqlc.Queries) (passkeyConfig, error) {
	config := passkeyConfig{Enabled: true, UserVerification: "preferred"}
	raw, err := q.GetSetting(ctx, settingPasskeys)
	if errors.Is(err, pgx.ErrNoRows) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	_ = json.Unmarshal(raw, &config)
	if config.UserVerification != "required" {
		config.UserVerification = "preferred"
	}
	return config, nil
}

// requestOrigin is the origin of the page making the request: the browser's
// Origin header, or this server's own address when there is none.
func requestOrigin(r *http.Request) string {
	if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
		return origin
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// relyingParty is the relying party in force for a request, and the origins
// an answer may come from.
//
// Listed origins are the administrator's and are taken as they are. With
// nothing pinned, the page's own origin is used and the RP ID follows it:
// a page can only ever be given passkeys of its own domain, so another page
// that says it is the origin gets nothing of anyone else's. With the RP ID
// pinned and no origins listed, the origin is never the request's word alone:
// a sibling subdomain (evil.example.com beside chat.example.com, both allowed
// by the browser to use the RP ID example.com) could otherwise relay a real
// answer. Then only trusted origins count - the server's own configured
// origins (CORS_ORIGINS) within the RP ID's domain, and the RP ID's own host.
func (c passkeyConfig) relyingParty(r *http.Request, trusted []string) (string, []string) {
	if len(c.Origins) > 0 {
		rpID := c.RPID
		if rpID == "" {
			rpID = hostOf(c.Origins[0])
		}
		return rpID, c.Origins
	}
	origin := requestOrigin(r)
	if c.RPID == "" {
		return hostOf(origin), []string{origin}
	}
	var origins []string
	add := func(o string) {
		for _, have := range origins {
			if have == o {
				return
			}
		}
		origins = append(origins, o)
	}
	for _, o := range trusted {
		if host := hostOf(o); host == c.RPID || strings.HasSuffix(host, "."+c.RPID) {
			add(strings.TrimRight(o, "/"))
		}
	}
	if hostOf(origin) == c.RPID {
		add(origin)
	}
	return c.RPID, origins
}

func hostOf(origin string) string {
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// errPasskeyOrigins is a pinned RP ID with no origin that may use it: the
// administrator has to list them.
var errPasskeyOrigins = httpx.NewError(http.StatusServiceUnavailable, "passkey_origins_needed",
	"list the origins passkeys may be used from in the passkey settings")

// webAuthn is go-webauthn set up for the request.
func (c passkeyConfig) webAuthn(r *http.Request, trusted []string) (*webauthn.WebAuthn, error) {
	rpID, origins := c.relyingParty(r, trusted)
	if rpID == "" || len(origins) == 0 {
		return nil, errPasskeyOrigins
	}
	return webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: "OpenChat",
		RPOrigins:     origins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: c.verification(),
		},
	})
}

func (c passkeyConfig) verification() protocol.UserVerificationRequirement {
	if c.UserVerification == "required" {
		return protocol.VerificationRequired
	}
	return protocol.VerificationPreferred
}

// passkeyUser is an account as go-webauthn sees it: its id as the user
// handle, and its passkeys.
type passkeyUser struct {
	user        sqlc.User
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return u.user.ID[:] }
func (u passkeyUser) WebAuthnName() string                       { return u.user.Email }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.user.Name }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// trustedOrigins are the origins this server is configured to serve pages
// from (CORS_ORIGINS).
func (s *Server) trustedOrigins() []string {
	if s.cfg == nil {
		return nil
	}
	return s.cfg.CORSOrigins
}

// passkeysOn is the settings, or 404 when passkeys are off.
func (s *Server) passkeysOn(ctx context.Context) (passkeyConfig, error) {
	config, err := loadPasskeys(ctx, s.q)
	if err != nil {
		return config, err
	}
	if !config.Enabled {
		return config, httpx.NewError(http.StatusNotFound, "passkeys_off", "passkeys are turned off")
	}
	return config, nil
}

// saveChallenge keeps a ceremony's session for its answer.
func (s *Server) saveChallenge(ctx context.Context, kind string, userID *uuid.UUID, session *webauthn.SessionData) (uuid.UUID, error) {
	value, err := json.Marshal(session)
	if err != nil {
		return uuid.Nil, err
	}
	return s.q.CreateWebAuthnChallenge(ctx, sqlc.CreateWebAuthnChallengeParams{Kind: kind, UserID: userID, Session: value})
}

// takeChallenge is a ceremony's session, once; ok is false for one unknown,
// spent or expired.
func (s *Server) takeChallenge(ctx context.Context, kind string, id uuid.UUID) (sqlc.TakeWebAuthnChallengeRow, webauthn.SessionData, bool, error) {
	var session webauthn.SessionData
	row, err := s.q.TakeWebAuthnChallenge(ctx, sqlc.TakeWebAuthnChallengeParams{ID: id, Kind: kind})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, session, false, nil
	}
	if err != nil {
		return row, session, false, err
	}
	if err := json.Unmarshal(row.Session, &session); err != nil {
		return row, session, false, err
	}
	return row, session, true, nil
}

// optionsJSON is the browser's half of a ceremony, as the JSON object the
// contract carries.
func optionsJSON(options any) (map[string]any, error) {
	raw, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(raw, &out)
}

// ------------------------------------------------------------------ sign-in

// BeginPasskeySignIn hands out a challenge for any passkey of this site.
func (s *Server) BeginPasskeySignIn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	config, err := s.passkeysOn(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Only a challenge is made here, nothing is checked; the rows it leaves
	// are bounded by how often one address may ask.
	if !passkeyChallenges.allow(clientIP(r), time.Now()) {
		httpx.Fail(w, r, httpx.NewError(http.StatusTooManyRequests, "login_rate_limited", "too many sign-in attempts; try again later"))
		return
	}
	wa, err := config.webAuthn(r, s.trustedOrigins())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	assertion, session, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(config.verification()))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s.writeOptions(w, r, "login", nil, session, assertion.Response)
}

// passkeyChallenges bounds how often one address may begin a sign-in.
var passkeyChallenges = &imageLimiter{seen: map[string]int{}}

func (s *Server) writeOptions(w http.ResponseWriter, r *http.Request, kind string, userID *uuid.UUID, session *webauthn.SessionData, options any) {
	id, err := s.saveChallenge(r.Context(), kind, userID, session)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	publicKey, err := optionsJSON(options)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, PasskeyOptions{ChallengeId: id, PublicKey: publicKey})
}

// PasskeySignIn checks the browser's answer and signs its account in.
func (s *Server) PasskeySignIn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	config, err := s.passkeysOn(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var body PasskeyAnswer
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	invalid := httpx.NewError(http.StatusUnauthorized, "invalid_passkey", "that passkey could not sign you in")

	// Counted before the answer is checked, given back if it verifies.
	slot, err := s.takeAttempt(ctx, passkeyLimit, clientIP(r), "")
	if err != nil {
		var limited *rateLimited
		if errors.As(err, &limited) {
			failRateLimited(w, r, limited)
			return
		}
		httpx.Fail(w, r, err)
		return
	}
	_, session, ok, err := s.takeChallenge(ctx, "login", body.ChallengeId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !ok {
		httpx.Fail(w, r, invalid)
		return
	}
	raw, err := json.Marshal(body.Credential)
	if err != nil {
		httpx.Fail(w, r, invalid)
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(raw)
	if err != nil {
		httpx.Fail(w, r, invalid)
		return
	}
	wa, err := config.webAuthn(r, s.trustedOrigins())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// The passkey presented, and its account: the user handle the
	// authenticator gives back must be the account the passkey was made for.
	var passkeyID uuid.UUID
	var account sqlc.User
	found, credential, err := wa.ValidatePasskeyLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		row, err := s.q.GetPasskeyByCredential(ctx, rawID)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(row.UserID[:], userHandle) {
			return nil, errors.New("the passkey is another account's")
		}
		var stored webauthn.Credential
		if err := json.Unmarshal(row.Credential, &stored); err != nil {
			return nil, err
		}
		user, err := s.q.GetUser(ctx, row.UserID)
		if err != nil {
			return nil, err
		}
		passkeyID, account = row.ID, user
		return passkeyUser{user: user, credentials: []webauthn.Credential{stored}}, nil
	}, session, parsed)
	if err != nil || found == nil {
		slog.InfoContext(ctx, "passkey sign-in refused", "error", err)
		httpx.Fail(w, r, invalid)
		return
	}
	// A sign count that went backwards is a copied authenticator.
	if credential.Authenticator.CloneWarning {
		slog.WarnContext(ctx, "passkey sign-in refused: possible cloned authenticator", "passkey_id", passkeyID)
		httpx.Fail(w, r, invalid)
		return
	}
	s.releaseAttempt(r, slot)
	if value, err := json.Marshal(credential); err == nil {
		if err := s.q.TouchPasskey(ctx, sqlc.TouchPasskeyParams{ID: passkeyID, Credential: value}); err != nil {
			slog.WarnContext(ctx, "record passkey use", "error", err)
		}
	}

	if account.DisabledAt.Valid {
		httpx.Fail(w, r, s.refusedAccount(ctx, errAccountDisabled))
		return
	}
	if account.Pending {
		httpx.Fail(w, r, s.refusedAccount(ctx, errPendingApproval))
		return
	}
	if err := s.issueSession(w, r, account.ID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, s.sessionUser(ctx, account))
}

// ------------------------------------------------------------- your passkeys

// accountPasskeys is the signed-in account and its passkeys, as go-webauthn
// wants them.
func (s *Server) accountPasskeys(ctx context.Context) (passkeyUser, error) {
	user := auth.MustUser(ctx)
	rows, err := s.q.UserPasskeyCredentials(ctx, user.ID)
	if err != nil {
		return passkeyUser{}, err
	}
	account := passkeyUser{user: user}
	for _, raw := range rows {
		var credential webauthn.Credential
		if json.Unmarshal(raw, &credential) == nil {
			account.credentials = append(account.credentials, credential)
		}
	}
	return account, nil
}

// BeginPasskeyRegistration hands out a challenge for a new passkey, leaving
// out the authenticators the account already has one on.
func (s *Server) BeginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	config, err := s.passkeysOn(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	account, err := s.accountPasskeys(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	wa, err := config.webAuthn(r, s.trustedOrigins())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	creation, session, err := wa.BeginRegistration(account,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(webauthn.Credentials(account.credentials).CredentialDescriptors()))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	id := account.user.ID
	s.writeOptions(w, r, "register", &id, session, creation.Response)
}

// AddPasskey checks the browser's new credential and keeps it.
func (s *Server) AddPasskey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	config, err := s.passkeysOn(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var body PasskeyAnswer
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	invalid := httpx.NewError(http.StatusBadRequest, "invalid_passkey", "that passkey could not be added")
	account, err := s.accountPasskeys(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	row, session, ok, err := s.takeChallenge(ctx, "register", body.ChallengeId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// A challenge is the account's that asked for it.
	if !ok || row.UserID == nil || *row.UserID != account.user.ID {
		httpx.Fail(w, r, invalid)
		return
	}
	raw, err := json.Marshal(body.Credential)
	if err != nil {
		httpx.Fail(w, r, invalid)
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(raw)
	if err != nil {
		httpx.Fail(w, r, invalid)
		return
	}
	wa, err := config.webAuthn(r, s.trustedOrigins())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	credential, err := wa.CreateCredential(account, session, parsed)
	if err != nil {
		slog.InfoContext(ctx, "passkey registration refused", "error", err)
		httpx.Fail(w, r, invalid)
		return
	}
	value, err := json.Marshal(credential)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	name := "Passkey"
	if body.Name != nil {
		if trimmed := truncate(strings.TrimSpace(*body.Name), passkeyNameMax); trimmed != "" {
			name = trimmed
		}
	}
	created, err := s.q.InsertPasskey(ctx, sqlc.InsertPasskeyParams{
		UserID: account.user.ID, CredentialID: credential.ID, Credential: value, Name: name,
	})
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == uniqueViolation {
		httpx.Fail(w, r, httpx.NewError(http.StatusConflict, "passkey_exists", "that passkey is already registered"))
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toPasskey(created.ID, created.Name, created.Credential, created.CreatedAt, created.LastUsedAt))
}

func (s *Server) ListPasskeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.q.ListPasskeys(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	items := make([]Passkey, 0, len(rows))
	for _, row := range rows {
		items = append(items, toPasskey(row.ID, row.Name, row.Credential, row.CreatedAt, row.LastUsedAt))
	}
	httpx.JSON(w, http.StatusOK, struct {
		Items []Passkey `json:"items"`
	}{items})
}

func (s *Server) RenamePasskey(w http.ResponseWriter, r *http.Request, passkeyId uuid.UUID) {
	ctx := r.Context()
	var body RenamePasskeyRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	name := truncate(strings.TrimSpace(body.Name), passkeyNameMax)
	if name == "" {
		httpx.Fail(w, r, httpx.NewFieldError("name", "invalid_name", "a passkey needs a name"))
		return
	}
	row, err := s.q.RenamePasskey(ctx, sqlc.RenamePasskeyParams{ID: passkeyId, UserID: auth.MustUser(ctx).ID, Name: name})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, toPasskey(row.ID, row.Name, row.Credential, row.CreatedAt, row.LastUsedAt))
}

func (s *Server) DeletePasskey(w http.ResponseWriter, r *http.Request, passkeyId uuid.UUID) {
	ctx := r.Context()
	n, err := s.q.DeletePasskey(ctx, sqlc.DeletePasskeyParams{ID: passkeyId, UserID: auth.MustUser(ctx).ID})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if n == 0 {
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "not_found", "no such passkey"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toPasskey(id uuid.UUID, name string, raw []byte, createdAt, lastUsedAt pgtype.Timestamptz) Passkey {
	var credential webauthn.Credential
	_ = json.Unmarshal(raw, &credential)
	out := Passkey{Id: id, Name: name, CreatedAt: createdAt.Time, Synced: credential.Flags.BackupEligible}
	if lastUsedAt.Valid {
		out.LastUsedAt = &lastUsedAt.Time
	}
	return out
}

// ------------------------------------------------------------------ settings

func (s *Server) passkeySettings(r *http.Request, config passkeyConfig) (PasskeySettings, error) {
	count, err := s.q.CountPasskeys(r.Context())
	if err != nil {
		return PasskeySettings{}, err
	}
	rpID, origins := config.relyingParty(r, s.trustedOrigins())
	out := PasskeySettings{
		Enabled: config.Enabled, RpId: config.RPID, Origins: config.Origins,
		UserVerification: PasskeySettingsUserVerification(config.UserVerification),
		EffectiveRpId:    rpID, EffectiveOrigins: origins, Registered: int(count),
	}
	if out.Origins == nil {
		out.Origins = []string{}
	}
	if out.EffectiveOrigins == nil {
		out.EffectiveOrigins = []string{}
	}
	return out, nil
}

func (s *Server) GetPasskeySettings(w http.ResponseWriter, r *http.Request) {
	config, err := loadPasskeys(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out, err := s.passkeySettings(r, config)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) UpdatePasskeySettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body UpdatePasskeySettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	config, err := loadPasskeys(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Enabled != nil {
		config.Enabled = *body.Enabled
	}
	if body.RpId != nil {
		rpID := strings.ToLower(strings.TrimSpace(*body.RpId))
		if strings.ContainsAny(rpID, "/: ") {
			httpx.Fail(w, r, httpx.NewFieldError("rpId", "invalid_rp_id", "a relying party ID is a domain, such as chat.example.com"))
			return
		}
		config.RPID = rpID
	}
	if body.Origins != nil {
		origins := []string{}
		for _, origin := range *body.Origins {
			origin = strings.TrimRight(strings.TrimSpace(origin), "/")
			if origin == "" {
				continue
			}
			parsed, err := url.Parse(origin)
			if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.Path != "" {
				httpx.Fail(w, r, httpx.NewFieldError("origins", "invalid_origin", "an origin is a scheme and a host, such as https://chat.example.com"))
				return
			}
			origins = append(origins, origin)
		}
		config.Origins = origins
	}
	if body.UserVerification != nil {
		config.UserVerification = string(*body.UserVerification)
	}
	value, _ := json.Marshal(config)
	if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingPasskeys, Value: value}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	slog.InfoContext(ctx, "passkey settings changed", "enabled", config.Enabled, "rp_id", config.RPID,
		"origins", config.Origins, "user_verification", config.UserVerification, "actor", auth.MustUser(ctx).Email)
	out, err := s.passkeySettings(r, config)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// purgeChallenges removes the ceremonies nobody finished.
func (s *Server) purgeChallenges(ctx context.Context, now time.Time) {
	if _, err := s.q.PurgeWebAuthnChallenges(ctx, pgtype.Timestamptz{Time: now.Add(-challengeKeep), Valid: true}); err != nil {
		slog.WarnContext(ctx, "purge webauthn challenges", "error", err)
	}
}
