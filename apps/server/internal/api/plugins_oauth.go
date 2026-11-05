package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"golang.org/x/oauth2"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/plugins"
)

// Connecting a person's own account to a plugin (docs/design/plugins-plan.md
// §5): connect answers with where to sign in; the authorization server sends
// the browser back to the callback, which trades the code for tokens, seals
// them on the person's install, and returns to the plugins page. A state is
// one-time, the person's own, and ten minutes long; PKCE binds the code to
// the sign-in that asked for it.

const pluginCallbackPath = "/api/v1/plugins/oauth/callback"

var (
	errPluginOAuthUnconfigured = httpx.NewError(http.StatusConflict, "plugin_oauth_unconfigured",
		"the plugin has no OAuth client, and its authorization server registers none on the fly")
	errPluginNotOAuth     = httpx.NewError(http.StatusConflict, "plugin_not_oauth", "the plugin does not use people's own accounts")
	errPluginNotConnected = httpx.NewError(http.StatusConflict, "plugin_not_connected",
		"your account is not connected to this plugin; connect it on the plugins page first")
)

// pluginRedirectURI is the callback as the browser reaches this site.
func pluginRedirectURI(r *http.Request) string {
	return requestOrigin(r) + pluginCallbackPath
}

// pluginClient is this site as the plugin's OAuth client: the administrator's
// client, else one registered on the fly (kept for next time).
func (s *Server) pluginClient(ctx context.Context, p sqlc.Plugin, redirectURI string) (plugins.Client, error) {
	if p.McpUrl == nil || *p.McpUrl == "" {
		return plugins.Client{}, errPluginNoService
	}
	found, err := plugins.Discover(ctx, *p.McpUrl)
	if err != nil {
		slog.WarnContext(ctx, "plugin authorization could not be found", "plugin", p.Slug, "error", err)
		return plugins.Client{}, httpx.NewError(http.StatusBadGateway, "plugin_unreachable", fmt.Sprintf("the service's sign-in could not be found: %v", err))
	}
	client := plugins.Client{Discovery: found, ClientID: p.OauthClientID, RedirectURI: redirectURI, Scopes: p.OauthScopes}
	if client.ClientID == "" {
		if found.Server.RegistrationEndpoint == "" {
			return plugins.Client{}, errPluginOAuthUnconfigured
		}
		id, secret, err := plugins.Register(ctx, found.Server, redirectURI)
		if err != nil {
			slog.WarnContext(ctx, "plugin client registration failed", "plugin", p.Slug, "error", err)
			return plugins.Client{}, errPluginOAuthUnconfigured
		}
		var sealed []byte
		if secret != "" {
			if sealed, err = s.secrets.Seal([]byte(secret)); err != nil {
				return plugins.Client{}, errSecretKeyMissing
			}
		}
		if err := s.q.SetPluginRegisteredClient(ctx, sqlc.SetPluginRegisteredClientParams{ID: p.ID, OauthClientID: id, OauthClientSecret: sealed}); err != nil {
			return plugins.Client{}, err
		}
		client.ClientID, client.ClientSecret = id, secret
		return client, nil
	}
	if len(p.OauthClientSecret) > 0 {
		secret, err := s.secrets.Open(p.OauthClientSecret)
		if err != nil {
			return plugins.Client{}, errSecretKeyMissing
		}
		client.ClientSecret = string(secret)
	}
	return client, nil
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func stateHash(state string) []byte {
	sum := sha256.Sum256([]byte(state))
	return sum[:]
}

func (s *Server) ConnectPlugin(w http.ResponseWriter, r *http.Request, pluginId openapi_types.UUID) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	visible, err := s.q.GetUserPlugin(ctx, sqlc.GetUserPluginParams{ID: pluginId, UserID: user.ID, Admin: isStaff(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, r, errPluginNotFound)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if visible.AuthType != "oauth" {
		httpx.Fail(w, r, errPluginNotOAuth)
		return
	}
	if !s.secrets.Ready() {
		httpx.Fail(w, r, errSecretKeyMissing)
		return
	}
	p, err := s.q.GetPlugin(ctx, pluginId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	redirectURI := pluginRedirectURI(r)
	client, err := s.pluginClient(ctx, p, redirectURI)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	state, verifier := randomToken(), oauth2.GenerateVerifier()
	sealed, err := s.secrets.Seal([]byte(verifier))
	if err != nil {
		httpx.Fail(w, r, errSecretKeyMissing)
		return
	}
	if err := s.q.CreatePluginOAuthState(ctx, sqlc.CreatePluginOAuthStateParams{
		StateHash: stateHash(state), UserID: user.ID, PluginID: p.ID, Verifier: sealed, RedirectUri: redirectURI,
	}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"authorizeUrl": client.AuthorizeURL(state, verifier)})
}

// PluginOAuthCallback ends a sign-in. Whatever happens it goes back to the
// plugins page, saying how it went; the reasons are codes, never the server's
// words, which arrive in the query and are not to be echoed into the page.
func (s *Server) PluginOAuthCallback(w http.ResponseWriter, r *http.Request, params PluginOAuthCallbackParams) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	back := func(query url.Values) {
		http.Redirect(w, r, "/plugins?"+query.Encode(), http.StatusFound)
	}
	fail := func(reason string) { back(url.Values{"connect_error": {reason}}) }

	if params.State == nil || *params.State == "" {
		fail("state")
		return
	}
	pending, err := s.q.TakePluginOAuthState(ctx, sqlc.TakePluginOAuthStateParams{StateHash: stateHash(*params.State), UserID: user.ID})
	if err != nil {
		fail("state")
		return
	}
	p, err := s.q.GetPlugin(ctx, pending.PluginID)
	if err != nil {
		fail("state")
		return
	}
	if params.Error != nil && *params.Error != "" {
		reason := "refused"
		if *params.Error != "access_denied" {
			reason = "server"
		}
		slog.InfoContext(ctx, "plugin sign-in did not complete", "plugin", p.Slug, "error", *params.Error)
		back(url.Values{"connect_error": {reason}, "plugin": {p.Slug}})
		return
	}
	if params.Code == nil || *params.Code == "" {
		fail("state")
		return
	}
	verifier, err := s.secrets.Open(pending.Verifier)
	if err != nil {
		fail("key")
		return
	}
	client, err := s.pluginClient(ctx, p, pending.RedirectUri)
	if err != nil {
		back(url.Values{"connect_error": {"server"}, "plugin": {p.Slug}})
		return
	}
	// RFC 9207: a server that names itself in the answer must be the one asked.
	if params.Iss != nil && *params.Iss != "" && client.Discovery.Server.Issuer != "" && *params.Iss != client.Discovery.Server.Issuer {
		back(url.Values{"connect_error": {"server"}, "plugin": {p.Slug}})
		return
	}
	token, err := client.Exchange(ctx, *params.Code, string(verifier))
	if err != nil {
		slog.WarnContext(ctx, "plugin token exchange failed", "plugin", p.Slug, "error", err)
		back(url.Values{"connect_error": {"exchange"}, "plugin": {p.Slug}})
		return
	}
	if err := s.saveTokens(ctx, user.ID, p.ID, token); err != nil {
		slog.ErrorContext(ctx, "plugin tokens could not be saved", "plugin", p.Slug, "error", err)
		back(url.Values{"connect_error": {"key"}, "plugin": {p.Slug}})
		return
	}
	slog.InfoContext(ctx, "plugin account connected", "plugin", p.Slug)
	// An administrator's first connection is what its tools are listed with.
	if !p.ToolsFetchedAt.Valid && user.Role == sqlc.UserRoleAdmin && p.McpUrl != nil && *p.McpUrl != "" {
		_ = s.fetchPluginTools(ctx, p, user.ID, pending.RedirectUri)
	}
	back(url.Values{"connected": {p.Slug}})
}

// saveTokens seals a person's tokens on their install, making it.
func (s *Server) saveTokens(ctx context.Context, userID, pluginID openapi_types.UUID, token *oauth2.Token) error {
	access, err := s.secrets.Seal([]byte(token.AccessToken))
	if err != nil {
		return err
	}
	var refresh []byte
	if token.RefreshToken != "" {
		if refresh, err = s.secrets.Seal([]byte(token.RefreshToken)); err != nil {
			return err
		}
	}
	var expires pgtype.Timestamptz
	if !token.Expiry.IsZero() {
		expires = pgtype.Timestamptz{Time: token.Expiry, Valid: true}
	}
	return s.q.SavePluginTokens(ctx, sqlc.SavePluginTokensParams{
		UserID: userID, PluginID: pluginID, AccessToken: access, RefreshToken: refresh, TokenExpiresAt: expires,
	})
}

// pluginToken is a person's access token for a plugin, refreshed when it is
// about to run out. A refresh the server refuses marks the install for
// reconnecting.
func (s *Server) pluginToken(ctx context.Context, userID openapi_types.UUID, p sqlc.Plugin, redirectURI string) (string, error) {
	install, err := s.q.GetPluginInstall(ctx, sqlc.GetPluginInstallParams{UserID: userID, PluginID: p.ID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && len(install.AccessToken) == 0) {
		return "", errPluginNotConnected
	}
	if err != nil {
		return "", err
	}
	if install.NeedsReconnect {
		return "", errPluginNotConnected
	}
	fresh := !install.TokenExpiresAt.Valid || time.Until(install.TokenExpiresAt.Time) > time.Minute
	if fresh || len(install.RefreshToken) == 0 {
		access, err := s.secrets.Open(install.AccessToken)
		if err != nil {
			return "", errSecretKeyMissing
		}
		return string(access), nil
	}
	refresh, err := s.secrets.Open(install.RefreshToken)
	if err != nil {
		return "", errSecretKeyMissing
	}
	client, err := s.pluginClient(ctx, p, redirectURI)
	if err != nil {
		return "", err
	}
	token, err := client.Refresh(ctx, string(refresh))
	if err != nil {
		slog.WarnContext(ctx, "plugin token refresh failed", "plugin", p.Slug, "error", err)
		_ = s.q.MarkPluginNeedsReconnect(ctx, sqlc.MarkPluginNeedsReconnectParams{UserID: userID, PluginID: p.ID})
		return "", errPluginNotConnected
	}
	if err := s.saveTokens(ctx, userID, p.ID, token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}
