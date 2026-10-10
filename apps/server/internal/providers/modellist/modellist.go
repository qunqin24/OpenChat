// Package modellist asks a provider endpoint which models it serves.
//
// This is the first request the server makes *outbound on an operator's
// behalf*: to a URL an operator typed, carrying a credential an operator
// stored. Every rule in this package follows from that one sentence.
//
//   - The URL is input. It must be http or https, and a redirect may not leave
//     the host the operator named — a redirect is precisely how a well-formed
//     request ends up somewhere nobody authorised, with the key still attached.
//   - The key is secret. It is never logged, never put in an error, and never
//     left in a URL that an error carries: Gemini takes it in the query string,
//     so every URL that reaches an error goes through redact first.
//   - The answer is somebody else's data. It is read under a byte cap and a
//     deadline, and its body never becomes an API error — the operator gets a
//     status and a short reason, the detail goes to the server log.
//
// Nothing in here logs. A package that holds a credential and also writes to
// the log is one careless format string away from writing the credential to
// the log; callers do the logging, from errors built here to be safe to print.
package modellist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Model is one entry from an endpoint's list.
type Model struct {
	// ID is the model as the endpoint spells it, which is what gets sent back
	// to it when someone chats with it.
	ID string

	// DisplayName is what the endpoint calls the model, and is empty when the
	// endpoint does not say. It stays empty rather than falling back to the id:
	// "upstream has no name for this" is a fact worth storing, and a label to
	// show in its place is a rendering decision.
	DisplayName string
}

// Lister asks one configured endpoint for its models.
//
// One method, because that is the whole job. Three implementations, because
// three vendors each spell the same request differently; adding a fourth is
// adding a file here, not a branch at the call site.
type Lister interface {
	List(ctx context.Context) ([]Model, error)
}

// Kind is the wire protocol an endpoint speaks. It mirrors the WireApi schema
// in the contract, which is why the strings have to match it exactly; a
// channel's vendor type is mapped to one of these by the API layer, not stored
// on the channel row.
type Kind string

const (
	KindOpenAI    Kind = "openai"
	KindAnthropic Kind = "anthropic"
	KindGemini    Kind = "gemini"
)

// Config is one endpoint as the operator configured it.
type Config struct {
	// BaseURL overrides the vendor's default endpoint. Empty means the default
	// for the kind, which is knowledge the adapter has and the channel row
	// deliberately does not.
	BaseURL string

	// APIKey is the operator's credential, saved or supplied for a preview.
	// It never appears in an error.
	APIKey string
	// AllowNoKey is set only for a channel type that supports anonymous access.
	AllowNoKey bool

	// HTTP overrides the shared client, mainly so a test can point an adapter
	// at an httptest server and never reach the network. The redirect policy is
	// attached to a copy of it either way, so a test exercises the same rule
	// production does.
	HTTP *http.Client

	// Timeout bounds one whole listing, paging included. Zero uses
	// DefaultTimeout.
	Timeout time.Duration
}

const (
	// DefaultTimeout bounds the entire List call rather than one request, so an
	// endpoint that pages cannot extend its own budget a page at a time.
	DefaultTimeout = 30 * time.Second

	// maxBodyBytes caps what we read from an endpoint we do not control. A
	// model list is kilobytes; this is room for an implausible one, not for an
	// endpoint that answers forever.
	maxBodyBytes = 4 << 20

	// maxPages bounds a paging loop. Twenty pages of a thousand models is two
	// orders of magnitude more than any real endpoint serves, so hitting it
	// means the endpoint is broken or lying — and running past it is a loop
	// that never ends.
	maxPages = 20

	// pageSize is what we ask for per page, so the common case is one request.
	pageSize = 1000

	// maxRedirects matches net/http's own default. Same-host redirects are
	// allowed (an endpoint may well normalise a path), just not endless ones.
	maxRedirects = 10

	// maxErrorBodyBytes is how much of a failed response we keep for the log.
	// Enough to explain a 403, not enough for an HTML error page to matter.
	maxErrorBodyBytes = 512
)

var (
	// ErrNoKey is a refresh attempted with nothing to authenticate with. The
	// handler checks this before building a Lister so it can name the field;
	// the adapters check it too, because a request that cannot succeed is a
	// request not worth making.
	ErrNoKey = errors.New("modellist: no API key is stored for this channel")

	// ErrBaseURL is an endpoint address we will not call: not a URL, not http
	// or https, or missing a host. It is operator input, so it is checked
	// before it is dialled rather than after.
	ErrBaseURL = errors.New("modellist: invalid base URL")

	// ErrRedirect is a redirect that would have left the configured host. Go
	// replays our headers on the next hop, x-api-key included, so following one
	// hands the operator's credential to a host they never named.
	ErrRedirect = errors.New("modellist: refused a redirect to another host")

	// ErrResponse is an answer we could not read as a model list.
	ErrResponse = errors.New("modellist: endpoint returned an unreadable model list")

	// ErrTooManyPages is an endpoint that kept claiming there was more.
	//
	// It is an error rather than a truncated list on purpose: the caller prunes
	// models the list does not mention, so a half-read list would read as
	// authoritative and delete the other half.
	ErrTooManyPages = errors.New("modellist: endpoint paged past the limit")

	// ErrUnknownKind is a channel whose kind no adapter implements.
	ErrUnknownKind = errors.New("modellist: unknown channel kind")
)

// StatusError is an endpoint that answered, but not with a model list.
//
// Snippet is a short, key-redacted excerpt of the body, and it exists for the
// server log. A handler must not put it in an API response: it is another
// vendor's error text — unlocalised, unvetted, and shaped by whoever is
// actually answering at that address.
type StatusError struct {
	StatusCode int
	Snippet    string
}

func (e *StatusError) Error() string {
	if e.Snippet == "" {
		return fmt.Sprintf("modellist: endpoint returned %d", e.StatusCode)
	}
	return fmt.Sprintf("modellist: endpoint returned %d: %s", e.StatusCode, e.Snippet)
}

// Unauthorized reports whether the endpoint rejected the credential, which is
// the one upstream failure an operator can fix from the panel.
func (e *StatusError) Unauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
}

// For builds the Lister for a kind.
//
// The credential is checked here rather than at the first request, so a
// misconfigured channel fails without anything leaving the machine.
func For(kind Kind, cfg Config) (Lister, error) {
	if strings.TrimSpace(cfg.APIKey) == "" && !cfg.AllowNoKey {
		return nil, ErrNoKey
	}

	switch kind {
	case KindOpenAI:
		return newLister(cfg, openAIDefaultBase, openAI{})
	case KindAnthropic:
		return newLister(cfg, anthropicDefaultBase, anthropic{})
	case KindGemini:
		return newLister(cfg, geminiDefaultBase, gemini{})
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
}

// resolveBase validates the operator's base URL, falling back to the vendor's
// default when they left it empty.
//
// It deliberately does not judge *where* the host is. A self-hosted relay on
// localhost or a private address is the ordinary case for this product, so
// blocking those would break the deployments most likely to use a custom base
// URL. What we defend against is not the address the operator typed — that one
// they chose — but the address somebody else could redirect us to afterwards.
func resolveBase(raw, fallback string) (*url.URL, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = fallback
	}

	parsed, err := url.Parse(value)
	if err != nil {
		// The parse error quotes the URL, which for Gemini is where the key
		// would be; it is not repeated here.
		return nil, fmt.Errorf("%w: not a URL", ErrBaseURL)
	}

	switch {
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return nil, fmt.Errorf("%w: scheme %q is not http or https", ErrBaseURL, parsed.Scheme)
	case parsed.Host == "":
		return nil, fmt.Errorf("%w: no host", ErrBaseURL)
	}

	// A base URL is a prefix, not a request: a query or fragment on it would be
	// either dropped or — for Gemini, whose key rides in the query — merged
	// with ours. Credentials in the userinfo section go the same way; they
	// would travel on every hop and show up in any URL an error quotes.
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil
	return parsed, nil
}
