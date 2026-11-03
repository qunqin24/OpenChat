package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// OAuth for plugins that act as each person (docs/design/plugins-plan.md §5),
// as the MCP authorization spec has it: the service names its authorization
// server in its protected resource metadata (RFC 9728), whose own metadata
// (RFC 8414) gives the endpoints; the person signs in there with PKCE (S256),
// and the code comes back to this site for tokens. The client is one an
// administrator registered with the server, or one registered on the fly
// (RFC 7591) where the server allows it.

const oauthTimeout = 20 * time.Second

// ServerMeta is what an authorization server says of itself.
type ServerMeta struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// Discovery is a service's authorization: the resource it names itself (sent
// as `resource`, RFC 8707), the scopes it lists, and its authorization server.
type Discovery struct {
	Resource string
	Scopes   []string
	Server   ServerMeta
}

var resourceMetadataParam = regexp.MustCompile(`resource_metadata="([^"]+)"`)

// Discover finds a service's authorization server. The service's address is an
// administrator's; the addresses it hands back are another server's word, so
// they are followed only over https, or to the service's own host.
func Discover(ctx context.Context, mcpURL string) (Discovery, error) {
	ctx, cancel := context.WithTimeout(ctx, oauthTimeout)
	defer cancel()
	client := &http.Client{Timeout: oauthTimeout}
	base, err := url.Parse(mcpURL)
	if err != nil {
		return Discovery{}, err
	}
	trusted := func(raw string) (*url.URL, error) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Host != base.Host) {
			return nil, fmt.Errorf("refusing to follow %q from the service", raw)
		}
		return u, nil
	}

	// The resource metadata: where the 401 says, else the well-known places.
	var candidates []string
	if req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, strings.NewReader(`{}`)); err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
			if m := resourceMetadataParam.FindStringSubmatch(resp.Header.Get("WWW-Authenticate")); m != nil {
				candidates = append(candidates, m[1])
			}
		}
	}
	origin := base.Scheme + "://" + base.Host
	if path := strings.TrimSuffix(base.Path, "/"); path != "" {
		candidates = append(candidates, origin+"/.well-known/oauth-protected-resource"+path)
	}
	candidates = append(candidates, origin+"/.well-known/oauth-protected-resource")

	var resource struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	found := false
	for _, c := range candidates {
		u, err := trusted(c)
		if err != nil {
			continue
		}
		if getJSON(ctx, client, u.String(), &resource) == nil && len(resource.AuthorizationServers) > 0 {
			found = true
			break
		}
	}
	if !found {
		return Discovery{}, errors.New("the service names no authorization server (no OAuth protected resource metadata)")
	}

	issuer, err := trusted(resource.AuthorizationServers[0])
	if err != nil {
		return Discovery{}, err
	}
	var meta ServerMeta
	if err := serverMeta(ctx, client, issuer, &meta); err != nil {
		return Discovery{}, err
	}
	if meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return Discovery{}, errors.New("the authorization server's metadata gives no endpoints")
	}
	for _, endpoint := range []string{meta.AuthorizationEndpoint, meta.TokenEndpoint} {
		if _, err := trusted(endpoint); err != nil {
			return Discovery{}, err
		}
	}
	if len(meta.CodeChallengeMethods) > 0 && !contains(meta.CodeChallengeMethods, "S256") {
		return Discovery{}, errors.New("the authorization server does not support PKCE with S256")
	}
	res := resource.Resource
	if res == "" {
		res = mcpURL
	}
	return Discovery{Resource: res, Scopes: resource.ScopesSupported, Server: meta}, nil
}

// serverMeta reads an authorization server's metadata from where RFC 8414 and
// OpenID Connect put it, an issuer with a path included.
func serverMeta(ctx context.Context, client *http.Client, issuer *url.URL, into *ServerMeta) error {
	origin := issuer.Scheme + "://" + issuer.Host
	path := strings.TrimSuffix(issuer.Path, "/")
	tries := []string{origin + "/.well-known/oauth-authorization-server" + path, origin + "/.well-known/openid-configuration" + path}
	if path != "" {
		tries = append(tries, origin+path+"/.well-known/openid-configuration")
	}
	for _, u := range tries {
		if err := getJSON(ctx, client, u, into); err == nil && into.TokenEndpoint != "" {
			return nil
		}
	}
	return fmt.Errorf("no authorization server metadata for %s", issuer)
}

func getJSON(ctx context.Context, client *http.Client, u string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(into)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Register registers this site as a client on the fly (RFC 7591), for a server
// that allows it.
func Register(ctx context.Context, meta ServerMeta, redirectURI string) (clientID, secret string, err error) {
	if meta.RegistrationEndpoint == "" {
		return "", "", errors.New("the authorization server does not register clients on the fly")
	}
	ctx, cancel := context.WithTimeout(ctx, oauthTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{
		"client_name":                "OpenChat",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "client_secret_post",
		"application_type":           "web",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.RegistrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: oauthTimeout}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return "", "", fmt.Errorf("registration refused: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var out struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || out.ClientID == "" {
		return "", "", errors.New("the registration gave no client id")
	}
	return out.ClientID, out.ClientSecret, nil
}

// Client is this site as an OAuth client of one authorization server.
type Client struct {
	Discovery    Discovery
	ClientID     string
	ClientSecret string
	RedirectURI  string
	// Scopes asked for, space-separated; empty asks for none in particular.
	Scopes string
}

func (c Client) config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.RedirectURI,
		Scopes:       strings.Fields(c.Scopes),
		Endpoint: oauth2.Endpoint{
			AuthURL:  c.Discovery.Server.AuthorizationEndpoint,
			TokenURL: c.Discovery.Server.TokenEndpoint,
		},
	}
}

// AuthorizeURL is where the person signs in: PKCE's challenge for verifier,
// the state, and the resource the token is for.
func (c Client) AuthorizeURL(state, verifier string) string {
	return c.config().AuthCodeURL(state, oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("resource", c.Discovery.Resource))
}

// Exchange trades the code the person came back with for tokens.
func (c Client) Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, oauthTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: oauthTimeout})
	return c.config().Exchange(ctx, code, oauth2.VerifierOption(verifier),
		oauth2.SetAuthURLParam("resource", c.Discovery.Resource))
}

// Refresh trades a refresh token for new tokens.
func (c Client) Refresh(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, oauthTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: oauthTimeout})
	return c.config().TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken, Expiry: time.Unix(1, 0)}).Token()
}
