package plugins

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/plugins/arxiv"
	"github.com/qunqin24/openchat/apps/server/internal/plugins/hackernews"
	"github.com/qunqin24/openchat/apps/server/internal/plugins/localkit"
	"github.com/qunqin24/openchat/apps/server/internal/plugins/openalex"
	"github.com/qunqin24/openchat/apps/server/internal/plugins/weather"
	"github.com/qunqin24/openchat/apps/server/internal/plugins/wikipedia"
)

// LocalScheme is the address of a service that runs inside OpenChat
// (openchat:arxiv): a built-in plugin with no service of its own to reach,
// connected to in memory. Nothing leaves the process to reach it.
const LocalScheme = "openchat:"

// local are the services by name, each made anew for a session.
var local = map[string]localkit.NewServer{
	"arxiv":      arxiv.NewServer,
	"openalex":   openalex.NewServer,
	"weather":    weather.NewServer,
	"wikipedia":  wikipedia.NewServer,
	"hackernews": hackernews.NewServer,
}

// IsLocal reports whether address names a service that runs here.
func IsLocal(address string) bool {
	name, ok := strings.CutPrefix(address, LocalScheme)
	return ok && local[name] != nil
}

// connectLocal opens a session with a service that runs here, handing it the
// plugin's header value (an API key the administrator set) as its secret.
func connectLocal(ctx context.Context, client *mcp.Client, end Endpoint, version string) (*mcp.ClientSession, error) {
	name, _ := strings.CutPrefix(end.URL, LocalScheme)
	newServer := local[name]
	if newServer == nil {
		return nil, fmt.Errorf("no built-in service %q", name)
	}
	clientEnd, serverEnd := mcp.NewInMemoryTransports()
	// The server's session ends when the client's closes its end.
	if _, err := newServer(localkit.Options{Version: version, Secret: end.HeaderValue}).Connect(ctx, serverEnd, nil); err != nil {
		return nil, fmt.Errorf("starting the built-in service: %w", err)
	}
	session, err := client.Connect(ctx, clientEnd, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}
	return session, nil
}
