package api

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/providers/modellist"
)

// channelModelQueriesPath is the source sqlc generates the refresh's statements
// from. These tests read the SQL rather than the generated Go, because the SQL
// is the file a change would be made in — and because the guarantees below are
// properties of the statements themselves, not of the Go that calls them. A
// database would prove the same things, and CI does not have one.
const channelModelQueriesPath = "../db/queries/channel_models.sql"

// refreshQueries are the statements RefreshChannelModels runs. Keep this in
// step with storeChannelModels; a statement added there and not here is simply
// unchecked.
var refreshQueries = []string{
	"ListChannelModels",
	"UpsertFetchedChannelModels",
	"DeleteStaleChannelModels",
}

// A refresh replaces somebody else's list. Whether this installation offers a
// model is our decision, so no statement on the refresh path may write the
// column that records it: a model that survives a refresh keeps its switch.
func TestRefreshNeverWritesEnabled(t *testing.T) {
	queries := namedChannelModelQueries(t)

	for _, name := range refreshQueries {
		body, ok := queries[name]
		if !ok {
			t.Errorf("%s is missing from %s", name, channelModelQueriesPath)
			continue
		}
		if strings.Contains(body, "enabled") {
			t.Errorf("%s mentions `enabled`; a refresh must not switch a model on or off", name)
		}
	}
}

// The one writer of `enabled` is the endpoint that exists to write it. A second
// one is a decision somebody has to make deliberately rather than by pasting a
// statement.
func TestOnlyOneQueryTogglesAModel(t *testing.T) {
	allowed := map[string]bool{
		"SetChannelModelEnabled": true,
		// The insert sets it once, for a model an operator typed in by hand.
		"AddManualChannelModel": true,
		// Reads, not writes.
		"ListChannelModels":            true,
		"GetChannelModel":              true,
		"CountEnabledChannelModels":    true,
		"CountEnabledChannelModelsFor": true,
		"ListCatalogModelsForChannel":  true,
	}

	for name, body := range namedChannelModelQueries(t) {
		if strings.Contains(body, "enabled") && !allowed[name] {
			t.Errorf("%s mentions `enabled`; only SetChannelModelEnabled is meant to write it", name)
		}
	}
}

// A manual row is one the operator typed in because the endpoint does not list
// it. A refresh must not adopt it, rename it, or prune it.
func TestRefreshLeavesManualRowsAlone(t *testing.T) {
	queries := namedChannelModelQueries(t)

	upsert := queries["UpsertFetchedChannelModels"]
	if !strings.Contains(upsert, "DO UPDATE") {
		t.Fatal("UpsertFetchedChannelModels no longer upserts; re-read this test")
	}
	// Without the fence, a manual row whose id the endpoint happens to list
	// would have its display name overwritten by the endpoint's.
	if !strings.Contains(upsert, "WHERE channel_models.source = 'fetched'") {
		t.Error("UpsertFetchedChannelModels can update a manual row: the DO UPDATE is not fenced to fetched rows")
	}
	if !strings.Contains(upsert, "'fetched'") || strings.Contains(upsert, "'manual'") {
		t.Error("UpsertFetchedChannelModels writes something other than fetched rows")
	}

	prune := queries["DeleteStaleChannelModels"]
	if !strings.Contains(prune, "source = 'fetched'") {
		t.Error("DeleteStaleChannelModels is not scoped to fetched rows; it would prune manual ones too")
	}
}

// Every statement addresses one channel. A write that forgot its channel_id
// would reach across every configured endpoint at once.
func TestChannelModelQueriesAreScopedToOneChannel(t *testing.T) {
	for name, body := range namedChannelModelQueries(t) {
		switch name {
		// The grouped count is per-channel by construction, and the catalogue
		// lookup reads a table that has no channel in it.
		case "CountEnabledChannelModels", "ListCatalogModelsForChannel":
			continue
		}
		if !strings.Contains(body, "channel_id") {
			t.Errorf("%s does not mention channel_id", name)
		}
	}
}

// What the endpoint said stays in the log. The operator gets a status and a
// reason: another vendor's error text is unlocalised, unvetted, and written by
// whoever is actually answering at that address.
func TestRefreshErrorKeepsUpstreamTextOutOfTheResponse(t *testing.T) {
	const leak = "your key sk-live-1234 is invalid for org acme"

	err := refreshError(&modellist.StatusError{StatusCode: http.StatusInternalServerError, Snippet: leak})

	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want an *httpx.APIError", err)
	}
	if strings.Contains(apiErr.Message, leak) || strings.Contains(apiErr.Message, "sk-live") {
		t.Fatalf("the API error repeats what the endpoint said: %q", apiErr.Message)
	}
	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", apiErr.Status)
	}
}

func TestRefreshErrorCodes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		code   string
		status int
	}{
		{"no key", modellist.ErrNoKey, "channel_key_missing", http.StatusBadRequest},
		{"bad base url", modellist.ErrBaseURL, "channel_base_url_invalid", http.StatusBadRequest},
		{"unknown kind", modellist.ErrUnknownKind, "channel_kind_unsupported", http.StatusBadRequest},
		{"redirect", modellist.ErrRedirect, "channel_redirect_refused", http.StatusBadGateway},
		{"unreadable", modellist.ErrResponse, "channel_response_invalid", http.StatusBadGateway},
		{"endless paging", modellist.ErrTooManyPages, "channel_response_invalid", http.StatusBadGateway},
		{"dial failure", errors.New("dial tcp: refused"), "channel_unreachable", http.StatusBadGateway},
		{
			"rejected key",
			&modellist.StatusError{StatusCode: http.StatusUnauthorized},
			"channel_key_rejected", http.StatusBadGateway,
		},
		{
			"upstream failure",
			&modellist.StatusError{StatusCode: http.StatusServiceUnavailable},
			"channel_upstream_error", http.StatusBadGateway,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var apiErr *httpx.APIError
			// Wrapped, because that is how these arrive: the adapters add
			// context and errors.Is has to see through it.
			if !errors.As(refreshError(wrapped(tc.err)), &apiErr) {
				t.Fatal("not an *httpx.APIError")
			}
			if apiErr.Code != tc.code {
				t.Errorf("code = %q, want %q", apiErr.Code, tc.code)
			}
			if apiErr.Status != tc.status {
				t.Errorf("status = %d, want %d", apiErr.Status, tc.status)
			}
		})
	}
}

// An endpoint that publishes no name for a model leaves the column empty, and
// the id stands in — at render time, so a later refresh that does supply a name
// replaces a blank rather than a guess we wrote in.
func TestToChannelModelFallsBackToTheModelId(t *testing.T) {
	model := toChannelModel(sqlc.ChannelModel{ModelID: "gpt-4o", Source: "fetched"}, nil)
	if model.DisplayName != "gpt-4o" {
		t.Errorf("displayName = %q, want the id", model.DisplayName)
	}
	if model.FetchedAt != nil {
		t.Errorf("fetchedAt = %v, want absent for a row that has never been fetched", model.FetchedAt)
	}

	named := toChannelModel(sqlc.ChannelModel{
		ModelID:     "claude-opus-4",
		DisplayName: "Claude Opus 4",
		Source:      "fetched",
		FetchedAt:   pgtype.Timestamptz{Valid: true},
	}, nil)
	if named.DisplayName != "Claude Opus 4" {
		t.Errorf("displayName = %q", named.DisplayName)
	}
	if named.FetchedAt == nil {
		t.Error("fetchedAt is absent for a row that was fetched")
	}
}

func wrapped(err error) error {
	return errors.Join(errors.New("modellist: listing models"), err)
}

// namedChannelModelQueries splits the file on sqlc's `-- name:` markers and
// returns the statements with their comments stripped: the comments explain
// what the SQL must not do, and a test looking for a column name would find it
// in the prose that promises the column is absent.
func namedChannelModelQueries(t *testing.T) map[string]string {
	t.Helper()

	raw, err := os.ReadFile(channelModelQueriesPath)
	if err != nil {
		t.Fatalf("read %s: %v", channelModelQueriesPath, err)
	}

	queries := make(map[string]string)
	for _, block := range strings.Split(string(raw), "-- name: ")[1:] {
		name, body, _ := strings.Cut(block, "\n")

		var statement []string
		for _, line := range strings.Split(body, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				statement = append(statement, line)
			}
		}
		queries[strings.Fields(name)[0]] = strings.Join(statement, "\n")
	}
	if len(queries) == 0 {
		t.Fatalf("no queries found in %s", channelModelQueriesPath)
	}
	return queries
}
