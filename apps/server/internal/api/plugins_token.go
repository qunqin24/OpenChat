package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/plugins"
)

// Plugins whose service takes each person's own token (authType token,
// docs/design/plugins-plan.md §5): the service issues it for the person's
// account there - McDonald's China, from open.mcd.cn - and they paste it on
// the plugins page. It is tried on the service before it is kept, then sealed
// on their install where an OAuth access token would be, with nothing to
// refresh; calls send it as `Authorization: Bearer`. A service that refuses it
// later marks the install for a new one (pluginHost.refusedToken).

// maxPluginToken bounds a pasted token; real ones are a few hundred bytes.
const maxPluginToken = 4000

var (
	errPluginNotToken = httpx.NewError(http.StatusConflict, "plugin_not_token", "the plugin does not take a token of your own")
	errTokenRefused   = httpx.NewError(http.StatusUnprocessableEntity, "plugin_token_refused",
		"the service did not accept this token")
)

func (s *Server) SetPluginToken(w http.ResponseWriter, r *http.Request, pluginId openapi_types.UUID) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	var body SetPluginTokenJSONBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	token := strings.TrimSpace(body.Token)
	if !validPluginToken(token) {
		httpx.Fail(w, r, httpx.NewFieldError("token", "invalid_token", "paste the token as the service gave it: no spaces, at most 4000 characters"))
		return
	}
	visible, err := s.q.GetUserPlugin(ctx, sqlc.GetUserPluginParams{ID: pluginId, UserID: user.ID, Admin: isStaff(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, r, errPluginNotFound)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if visible.AuthType != "token" {
		httpx.Fail(w, r, errPluginNotToken)
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
	if p.McpUrl == nil || *p.McpUrl == "" {
		httpx.Fail(w, r, errPluginNoService)
		return
	}

	// Tried first, so a mistyped token is said at once rather than at the
	// first call in a conversation. The listing is the administrator's first
	// one when they are the first to connect, as with OAuth.
	tools, err := plugins.ListTools(ctx, plugins.Endpoint{
		URL: *p.McpUrl, HeaderName: "Authorization", HeaderValue: "Bearer " + token, Version: s.cfg.Version,
	})
	// A token that may not even list the tools (403) is as good as refused here.
	if errors.Is(err, plugins.ErrRefused) || errors.Is(err, plugins.ErrForbidden) {
		slog.InfoContext(ctx, "plugin token refused", "plugin", p.Slug)
		httpx.Fail(w, r, errTokenRefused)
		return
	}
	if err != nil {
		slog.WarnContext(ctx, "plugin token could not be tried", "plugin", p.Slug, "error", err)
		httpx.Fail(w, r, httpx.NewError(http.StatusBadGateway, "plugin_unreachable", "the service could not be reached"))
		return
	}
	sealed, err := s.secrets.Seal([]byte(token))
	if err != nil {
		httpx.Fail(w, r, errSecretKeyMissing)
		return
	}
	if err := s.q.SavePluginTokens(ctx, sqlc.SavePluginTokensParams{
		UserID: user.ID, PluginID: p.ID, AccessToken: sealed, TokenExpiresAt: pgtype.Timestamptz{},
		// Bound to the service it was just tried on: an address changed
		// meanwhile leaves it unused rather than sent there.
		TokenFor: tokenFor(p),
	}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	slog.InfoContext(ctx, "plugin token saved", "plugin", p.Slug)
	if !p.ToolsFetchedAt.Valid && user.Role == sqlc.UserRoleAdmin {
		if err := s.storePluginTools(ctx, p.ID, tools); err != nil {
			slog.WarnContext(ctx, "plugin tools could not be kept", "plugin", p.Slug, "error", err)
		}
	}
	row, err := s.q.GetUserPlugin(ctx, sqlc.GetUserPluginParams{ID: pluginId, UserID: user.ID, Admin: isStaff(ctx)})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toPlugin(sqlc.ListUserPluginsRow(row)))
}

// validPluginToken is whether a token can go in a header as it is: visible
// ASCII, no spaces, which every bearer token is (RFC 6750's b64token).
func validPluginToken(token string) bool {
	if token == "" || len(token) > maxPluginToken {
		return false
	}
	for i := 0; i < len(token); i++ {
		if token[i] < 0x21 || token[i] > 0x7e {
			return false
		}
	}
	return true
}
