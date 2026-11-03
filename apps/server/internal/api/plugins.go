package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// Plugins (插件, docs/design/plugins-plan.md): skills plus a remote MCP
// service. Administrators add them; each person installs the ones they want
// from the plugins page. This is the catalogue and the installs; the MCP
// connection, OAuth and the tools in conversations build on it.
//
// A plugin the caller may not see - switched off, or for administrators only -
// is not there for them: 404, as anyone else's data is.

var (
	errPluginNotFound     = httpx.NewError(http.StatusNotFound, "plugin_not_found", "no such plugin")
	errPluginNeedsAccount = httpx.NewError(http.StatusConflict, "plugin_needs_account",
		"this plugin acts as you: connect your account to install it")
)

func toPlugin(row sqlc.ListUserPluginsRow) Plugin {
	return Plugin{
		Id: row.ID, Slug: row.Slug, Name: row.Name, Description: row.Description, About: row.About,
		Category: row.Category, Featured: row.Featured, IconText: row.IconText, IconColor: row.IconColor,
		IconPath: row.IconPath,
		AuthType: PluginAuthType(row.AuthType), ReadTools: int(row.ReadTools), WriteTools: int(row.WriteTools),
		Skills: int(row.Skills), Installed: row.Installed, NeedsReconnect: row.NeedsReconnect,
	}
}

func (s *Server) ListPlugins(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	rows, err := s.q.ListUserPlugins(ctx, sqlc.ListUserPluginsParams{UserID: user.ID, Admin: isStaff(ctx)})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := PluginList{Items: make([]Plugin, 0, len(rows))}
	for _, row := range rows {
		out.Items = append(out.Items, toPlugin(row))
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) InstallPlugin(w http.ResponseWriter, r *http.Request, pluginId openapi_types.UUID) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	params := sqlc.GetUserPluginParams{ID: pluginId, UserID: user.ID, Admin: isStaff(ctx)}
	plugin, err := s.q.GetUserPlugin(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, r, errPluginNotFound)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Installing one that acts as the person is connecting their account
	// (ConnectPlugin), which makes the install; it is not installed half-way.
	if plugin.AuthType == "oauth" && !plugin.Installed {
		httpx.Fail(w, r, errPluginNeedsAccount)
		return
	}
	if err := s.q.InstallPlugin(ctx, sqlc.InstallPluginParams{UserID: user.ID, PluginID: pluginId}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	slog.InfoContext(ctx, "plugin installed", "plugin", plugin.Slug)
	plugin.Installed = true
	httpx.JSON(w, http.StatusOK, toPlugin(sqlc.ListUserPluginsRow(plugin)))
}

func (s *Server) UninstallPlugin(w http.ResponseWriter, r *http.Request, pluginId openapi_types.UUID) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	if _, err := s.q.UninstallPlugin(ctx, sqlc.UninstallPluginParams{UserID: user.ID, PluginID: pluginId}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
