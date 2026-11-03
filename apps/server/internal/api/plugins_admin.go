package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/plugins"
)

// The administrators' plugins page (后台 → 插件): adding and changing plugins,
// listing their service's tools, and each tool's permission. The header an
// administrator sets for a service is sealed (internal/secrets) and never sent
// back; the address may be on the internal network (2026-11-03).

var (
	errSlugTaken        = httpx.NewError(http.StatusConflict, "slug_taken", "another plugin has that short name")
	errSecretKeyMissing = httpx.NewError(http.StatusConflict, "secret_key_missing",
		"OPENCHAT_SECRET_KEY is not set, so the header value cannot be stored")
	errPluginBuiltin = httpx.NewError(http.StatusConflict, "plugin_builtin",
		"a built-in plugin cannot be deleted; turn it off instead")
	errPluginNoService = httpx.NewError(http.StatusConflict, "plugin_no_service", "the plugin has no MCP address")
)

var (
	pluginSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// An icon is a Simple Icons slug and its SVG path data: commands and
	// numbers only, which is all a path drawn into the page may hold.
	iconSlug = regexp.MustCompile(`^[a-z0-9]{0,64}$`)
	iconPath = regexp.MustCompile(`^[MmLlHhVvCcSsQqTtAaZz0-9.,+\-eE\s]*$`)
)

// maxIconPath bounds an icon's path; Simple Icons' longest are a few thousand.
const maxIconPath = 20000

func invalidPlugin(field, message string) error {
	return httpx.NewFieldError(field, "invalid_plugin", message)
}

// pluginRow is a plugin's stored fields as the input sets them.
type pluginRow struct {
	slug, name, description, about, category, iconText, iconColor string
	iconSlug, iconPath                                            string
	featured, enabled, directTools                                bool
	audience, authType, headerName                                string
	mcpURL                                                        *string
	sort                                                          int32
	// secret is the sealed header value to store, when setSecret.
	secret    []byte
	setSecret bool
	// The OAuth client: its id and scopes, and its sealed secret when setClientSecret.
	oauthClientID, oauthScopes string
	clientSecret               []byte
	setClientSecret            bool
}

// pluginInput checks a form and seals its header value.
func (s *Server) pluginInput(body AdminPluginInput) (pluginRow, error) {
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	row := pluginRow{
		slug: strings.TrimSpace(body.Slug), name: strings.TrimSpace(body.Name), description: str(body.Description),
		about: str(body.About), category: str(body.Category), iconText: str(body.IconText), iconColor: str(body.IconColor),
		iconSlug: str(body.IconSlug), iconPath: str(body.IconPath),
		enabled: true, audience: "all", authType: "none", headerName: "Authorization",
	}
	if body.Featured != nil {
		row.featured = *body.Featured
	}
	if body.Enabled != nil {
		row.enabled = *body.Enabled
	}
	if body.DirectTools != nil {
		row.directTools = *body.DirectTools
	}
	if body.Audience != nil {
		row.audience = string(*body.Audience)
	}
	if body.AuthType != nil {
		row.authType = string(*body.AuthType)
	}
	if name := str(body.HeaderName); name != "" {
		row.headerName = name
	}
	if body.Sort != nil {
		row.sort = int32(*body.Sort)
	}
	switch {
	case !pluginSlug.MatchString(row.slug) || len(row.slug) > 32:
		return row, invalidPlugin("slug", "the short name takes lowercase letters, digits and single hyphens, up to 32")
	case row.name == "" || utf8.RuneCountInString(row.name) > 60:
		return row, invalidPlugin("name", "the name must be 1 to 60 characters")
	case row.audience != "all" && row.audience != "admins":
		return row, invalidPlugin("audience", "audience must be all or admins")
	case row.authType != "none" && row.authType != "header" && row.authType != "oauth":
		return row, invalidPlugin("authType", "authType must be none, header or oauth")
	case !iconSlug.MatchString(row.iconSlug):
		return row, invalidPlugin("iconSlug", "not a Simple Icons slug")
	case len(row.iconPath) > maxIconPath || !iconPath.MatchString(row.iconPath):
		return row, invalidPlugin("iconPath", "not an SVG path")
	case !validHeaderName(row.headerName):
		return row, invalidPlugin("headerName", "not a header name")
	}
	if address := str(body.McpUrl); address != "" {
		u, err := url.Parse(address)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return row, invalidPlugin("mcpUrl", "the MCP address must be an http or https URL")
		}
		row.mcpURL = &address
	}
	row.oauthClientID, row.oauthScopes = str(body.OauthClientId), str(body.OauthScopes)
	if body.OauthClientSecret != nil {
		row.setClientSecret = true
		if value := strings.TrimSpace(*body.OauthClientSecret); value != "" {
			sealed, err := s.secrets.Seal([]byte(value))
			if err != nil {
				return row, errSecretKeyMissing
			}
			row.clientSecret = sealed
		}
	}
	if body.HeaderValue != nil {
		row.setSecret = true
		if value := strings.TrimSpace(*body.HeaderValue); value != "" {
			sealed, err := s.secrets.Seal([]byte(value))
			if err != nil {
				return row, errSecretKeyMissing
			}
			row.secret = sealed
		}
	}
	return row, nil
}

func validHeaderName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for _, r := range name {
		if !(r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

func toAdminPlugin(p sqlc.Plugin, installs, tools, toolsOn int64) AdminPlugin {
	out := AdminPlugin{
		Id: p.ID, Slug: p.Slug, Name: p.Name, Description: p.Description, About: p.About, Category: p.Category,
		IconText: p.IconText, IconColor: p.IconColor, Featured: p.Featured, Enabled: p.Enabled,
		Audience: AdminPluginAudience(p.Audience), AuthType: AdminPluginAuthType(p.AuthType), HeaderName: p.HeaderName,
		IconSlug: p.IconSlug, IconPath: p.IconPath,
		HeaderSet: len(p.HeaderSecret) > 0, Sort: int(p.Sort), Installs: int(installs), Tools: int(tools), ToolsOn: int(toolsOn),
		Builtin:       p.Builtin != nil,
		OauthClientId: p.OauthClientID, OauthSecretSet: len(p.OauthClientSecret) > 0, OauthScopes: p.OauthScopes,
		OauthRegistered: p.OauthRegistered, DirectTools: p.DirectTools,
	}
	if p.McpUrl != nil {
		out.McpUrl = *p.McpUrl
	}
	if p.ToolsFetchedAt.Valid {
		at := p.ToolsFetchedAt.Time
		out.ToolsFetchedAt = &at
	}
	return out
}

// adminPlugin is one plugin as the list has it.
func (s *Server) adminPlugin(ctx context.Context, id openapi_types.UUID) (AdminPlugin, error) {
	rows, err := s.q.ListAdminPlugins(ctx)
	if err != nil {
		return AdminPlugin{}, err
	}
	for _, r := range rows {
		if r.ID == id {
			return toAdminPlugin(adminRowPlugin(r), r.Installs, r.Tools, r.ToolsOn), nil
		}
	}
	return AdminPlugin{}, errPluginNotFound
}

func adminRowPlugin(r sqlc.ListAdminPluginsRow) sqlc.Plugin {
	return sqlc.Plugin{
		ID: r.ID, Slug: r.Slug, Name: r.Name, Description: r.Description, About: r.About, Category: r.Category,
		IconText: r.IconText, IconColor: r.IconColor, Featured: r.Featured, Enabled: r.Enabled, Audience: r.Audience,
		McpUrl: r.McpUrl, AuthType: r.AuthType, Sort: r.Sort, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		HeaderName: r.HeaderName, HeaderSecret: r.HeaderSecret, ToolsFetchedAt: r.ToolsFetchedAt, Builtin: r.Builtin,
		OauthClientID: r.OauthClientID, OauthClientSecret: r.OauthClientSecret, OauthScopes: r.OauthScopes,
		OauthRegistered: r.OauthRegistered, DirectTools: r.DirectTools,
		IconSlug: r.IconSlug, IconPath: r.IconPath,
	}
}

func (s *Server) ListAdminPlugins(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListAdminPlugins(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := AdminPluginList{Items: make([]AdminPlugin, 0, len(rows)), SecretKeySet: s.secrets.Ready()}
	for _, row := range rows {
		out.Items = append(out.Items, toAdminPlugin(adminRowPlugin(row), row.Installs, row.Tools, row.ToolsOn))
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) CreateAdminPlugin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body AdminPluginInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	in, err := s.pluginInput(body)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	p, err := s.q.CreatePlugin(ctx, sqlc.CreatePluginParams{
		Slug: in.slug, Name: in.name, Description: in.description, About: in.about, Category: in.category,
		IconText: in.iconText, IconColor: in.iconColor, Featured: in.featured, Enabled: in.enabled, Audience: in.audience,
		McpUrl: in.mcpURL, AuthType: in.authType, HeaderName: in.headerName, HeaderSecret: in.secret, Sort: in.sort,
		IconSlug: in.iconSlug, IconPath: in.iconPath, DirectTools: in.directTools,
	})
	if err != nil {
		httpx.Fail(w, r, slugErr(err))
		return
	}
	if err := s.setOAuthClient(ctx, p.ID, in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	slog.InfoContext(ctx, "plugin added", "plugin", p.Slug, "actor", auth.MustUser(ctx).Email)
	out, err := s.adminPlugin(ctx, p.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (s *Server) UpdateAdminPlugin(w http.ResponseWriter, r *http.Request, pluginId openapi_types.UUID) {
	ctx := r.Context()
	var body AdminPluginInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	in, err := s.pluginInput(body)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	p, err := s.q.UpdatePlugin(ctx, sqlc.UpdatePluginParams{
		ID: pluginId, Slug: in.slug, Name: in.name, Description: in.description, About: in.about, Category: in.category,
		IconText: in.iconText, IconColor: in.iconColor, Featured: in.featured, Enabled: in.enabled, Audience: in.audience,
		McpUrl: in.mcpURL, AuthType: in.authType, HeaderName: in.headerName, SetSecret: in.setSecret, HeaderSecret: in.secret,
		Sort: in.sort, IconSlug: in.iconSlug, IconPath: in.iconPath, DirectTools: in.directTools,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, r, errPluginNotFound)
		return
	}
	if err != nil {
		httpx.Fail(w, r, slugErr(err))
		return
	}
	// Never the header's value, not even at debug level.
	slog.InfoContext(ctx, "plugin changed", "plugin", p.Slug, "header_changed", in.setSecret, "actor", auth.MustUser(ctx).Email)
	if err := s.setOAuthClient(ctx, p.ID, in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out, err := s.adminPlugin(ctx, p.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// setOAuthClient stores the OAuth client a form gives. An id the form leaves
// empty keeps one registered on the fly; changing it drops that.
func (s *Server) setOAuthClient(ctx context.Context, id openapi_types.UUID, in pluginRow) error {
	if in.authType != "oauth" {
		return nil
	}
	current, err := s.q.GetPlugin(ctx, id)
	if err != nil {
		return err
	}
	if current.OauthRegistered && in.oauthClientID == "" && !in.setClientSecret {
		if current.OauthScopes == in.oauthScopes {
			return nil
		}
		in.oauthClientID, in.clientSecret, in.setClientSecret = current.OauthClientID, current.OauthClientSecret, true
	}
	return s.q.SetPluginOAuthClient(ctx, sqlc.SetPluginOAuthClientParams{
		ID: id, OauthClientID: in.oauthClientID, OauthScopes: in.oauthScopes, SetSecret: in.setClientSecret, Secret: in.clientSecret,
	})
}

func slugErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return errSlugTaken
	}
	return err
}

func (s *Server) DeleteAdminPlugin(w http.ResponseWriter, r *http.Request, pluginId openapi_types.UUID) {
	ctx := r.Context()
	n, err := s.q.DeletePlugin(ctx, pluginId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if n == 0 {
		// Not there, or one that ships with OpenChat (DeletePlugin leaves those).
		if p, err := s.q.GetPlugin(ctx, pluginId); err == nil && p.Builtin != nil {
			httpx.Fail(w, r, errPluginBuiltin)
			return
		}
		httpx.Fail(w, r, errPluginNotFound)
		return
	}
	slog.InfoContext(ctx, "plugin deleted", "plugin_id", pluginId, "actor", auth.MustUser(ctx).Email)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pluginTools(ctx context.Context, p sqlc.Plugin) (PluginToolList, error) {
	rows, err := s.q.ListPluginTools(ctx, p.ID)
	if err != nil {
		return PluginToolList{}, err
	}
	out := PluginToolList{Items: make([]PluginTool, 0, len(rows))}
	for _, t := range rows {
		out.Items = append(out.Items, PluginTool{
			Name: t.Name, Title: t.Title, Description: t.Description, ReadOnly: t.ReadOnly, Destructive: t.Destructive,
			Permission: PluginToolPermission(t.Permission),
		})
	}
	if p.ToolsFetchedAt.Valid {
		at := p.ToolsFetchedAt.Time
		out.FetchedAt = &at
	}
	return out, nil
}

func (s *Server) ListAdminPluginTools(w http.ResponseWriter, r *http.Request, pluginId openapi_types.UUID) {
	p, err := s.q.GetPlugin(r.Context(), pluginId)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, r, errPluginNotFound)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out, err := s.pluginTools(r.Context(), p)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) RefreshAdminPluginTools(w http.ResponseWriter, r *http.Request, pluginId openapi_types.UUID) {
	ctx := r.Context()
	p, err := s.q.GetPlugin(ctx, pluginId)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, r, errPluginNotFound)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if p.McpUrl == nil || *p.McpUrl == "" {
		httpx.Fail(w, r, errPluginNoService)
		return
	}
	end := plugins.Endpoint{URL: *p.McpUrl, HeaderName: p.HeaderName, Version: s.cfg.Version}
	// One that acts as each person is listed as the administrator: with their
	// own connection, made on the plugins page like anyone's.
	if p.AuthType == "oauth" {
		token, err := s.pluginToken(ctx, auth.MustUser(ctx).ID, p, pluginRedirectURI(r))
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		end.HeaderName, end.HeaderValue = "Authorization", "Bearer "+token
	}
	if p.AuthType == "header" && len(p.HeaderSecret) > 0 {
		value, err := s.secrets.Open(p.HeaderSecret)
		if err != nil {
			httpx.Fail(w, r, errSecretKeyMissing)
			return
		}
		end.HeaderValue = string(value)
	}
	tools, err := plugins.ListTools(ctx, end)
	if err != nil {
		slog.WarnContext(ctx, "plugin tools could not be listed", "plugin", p.Slug, "error", err)
		httpx.Fail(w, r, httpx.NewError(http.StatusBadGateway, "plugin_unreachable", fmt.Sprintf("the service could not be listed: %v", err)))
		return
	}
	names := make([]string, 0, len(tools))
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		for _, t := range tools {
			names = append(names, t.Name)
			if err := q.UpsertPluginTool(ctx, sqlc.UpsertPluginToolParams{
				PluginID: p.ID, Name: t.Name, Title: t.Title, Description: t.Description, InputSchema: t.InputSchema,
				ReadOnly: t.ReadOnly, Destructive: t.Destructive,
			}); err != nil {
				return err
			}
		}
		if err := q.DeletePluginToolsExcept(ctx, sqlc.DeletePluginToolsExceptParams{PluginID: p.ID, Names: names}); err != nil {
			return err
		}
		return q.MarkPluginToolsFetched(ctx, p.ID)
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	p.ToolsFetchedAt.Time, p.ToolsFetchedAt.Valid = time.Now(), true
	out, err := s.pluginTools(ctx, p)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) UpdateAdminPluginTool(w http.ResponseWriter, r *http.Request, pluginId openapi_types.UUID, toolName string) {
	var body UpdateAdminPluginToolJSONBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !body.Permission.Valid() {
		httpx.Fail(w, r, httpx.NewFieldError("permission", "invalid_permission", "permission must be allow, confirm or blocked"))
		return
	}
	n, err := s.q.SetPluginToolPermission(r.Context(), sqlc.SetPluginToolPermissionParams{
		PluginID: pluginId, Name: toolName, Permission: string(body.Permission),
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if n == 0 {
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "tool_not_found", "no such tool"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
