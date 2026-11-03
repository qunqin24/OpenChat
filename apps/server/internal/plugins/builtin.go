package plugins

import (
	"context"
	"log/slog"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// Builtin is a plugin that ships with OpenChat: a known service, set up
// in advance so an administrator only has to finish what is theirs (an
// account with the service, its OAuth app) and switch it on.
type Builtin struct {
	// Key names the definition for good; the row keeps it (plugins.builtin).
	Key         string
	Slug        string
	Name        string
	Description string
	About       string
	Category    string
	IconText    string
	IconColor   string
	// IconSlug and IconPath are its Simple Icons icon (simpleicons.org, CC0),
	// the path copied from simple-icons@16.36.0.
	IconSlug string
	IconPath string
	MCPURL   string
	AuthType string
	// OAuthScopes are what is asked for when a person connects.
	OAuthScopes string
	Sort        int32
}

// Builtins are the plugins every server starts with.
var Builtins = []Builtin{
	{
		Key:         "github",
		Slug:        "github",
		Name:        "GitHub",
		Description: "查仓库、读代码，管理 issue 和 pull request",
		About: "GitHub 官方的 MCP 服务：搜索和阅读仓库里的代码，查看、创建和评论 issue 与 pull request，" +
			"查看 Actions 运行和发布。每个人用自己的 GitHub 账号授权，只能做这个账号本来就能做的事。",
		Category:  "开发",
		IconText:  "GH",
		IconColor: "#181717",
		IconSlug:  "github",
		IconPath:  "M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12",
		// GitHub's hosted server (github/github-mcp-server). It takes OAuth from
		// github.com/login/oauth, which registers no clients on the fly: the
		// administrator registers an OAuth app for this site.
		MCPURL:   "https://api.githubcopilot.com/mcp/",
		AuthType: "oauth",
		// Its repositories (public and private), and the organizations and
		// profile around them; the person sees and grants these on GitHub.
		OAuthScopes: "repo read:org read:user",
		Sort:        10,
	},
}

// EnsureBuiltins makes the built-in plugins a database does not have yet,
// switched off. One made before is left as the administrator has it, and one
// whose short name an administrator's plugin already took is skipped.
func EnsureBuiltins(ctx context.Context, q *sqlc.Queries) error {
	for _, b := range Builtins {
		key, url := b.Key, b.MCPURL
		n, err := q.InsertBuiltinPlugin(ctx, sqlc.InsertBuiltinPluginParams{
			Builtin: &key, Slug: b.Slug, Name: b.Name, Description: b.Description, About: b.About,
			Category: b.Category, IconText: b.IconText, IconColor: b.IconColor, McpUrl: &url, AuthType: b.AuthType,
			Sort: b.Sort, IconSlug: b.IconSlug, IconPath: b.IconPath, OauthScopes: b.OAuthScopes,
		})
		if err != nil {
			return err
		}
		if n == 1 {
			slog.InfoContext(ctx, "built-in plugin added, switched off until an administrator sets it up", "plugin", b.Slug)
		}
		if b.OAuthScopes != "" {
			if err := q.FillBuiltinPluginScopes(ctx, sqlc.FillBuiltinPluginScopesParams{Builtin: &key, OauthScopes: b.OAuthScopes}); err != nil {
				return err
			}
		}
		if b.IconPath != "" {
			if err := q.FillBuiltinPluginIcon(ctx, sqlc.FillBuiltinPluginIconParams{
				Builtin: &key, IconSlug: b.IconSlug, IconPath: b.IconPath, IconColor: b.IconColor,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
