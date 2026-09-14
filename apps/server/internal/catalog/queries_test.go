package catalog

import (
	"os"
	"strings"
	"testing"
)

// queriesPath is the source sqlc generates PgStore's statements from. Reading
// the SQL rather than the generated Go keeps this test pointed at the file a
// change would actually be made in.
const queriesPath = "../db/queries/catalog.sql"

// syncQueries are the statements PgStore.Replace runs. Keep this in step with
// it; a query added there and not here is simply unchecked.
var syncQueries = []string{
	"DeleteCatalogModels",
	"DeleteCatalogProviders",
	"InsertCatalogProviders",
	"InsertCatalogModels",
}

// TestSyncQueriesLeaveModelSettingsAlone checks the rule against the SQL rather
// than against a fake, which is where it can actually be broken: a DELETE added
// to the replace path would sail past any test that only watches the Store
// interface.
func TestSyncQueriesLeaveModelSettingsAlone(t *testing.T) {
	queries := namedQueries(t)

	for _, name := range syncQueries {
		body, ok := queries[name]
		if !ok {
			t.Errorf("%s is missing from %s", name, queriesPath)
			continue
		}
		if strings.Contains(body, "channel_models") {
			t.Errorf("%s touches channel_models; a catalogue refresh must not reach a channel's model list", name)
		}
	}
}

// The settings table has exactly one writer, so a second one is a decision
// somebody has to make deliberately rather than by pasting a statement.
func TestOnlyOneQueryWritesModelSettings(t *testing.T) {
	for name, body := range namedQueries(t) {
		writes := strings.Contains(body, "INSERT INTO channel_models") ||
			strings.Contains(body, "UPDATE channel_models") ||
			strings.Contains(body, "DELETE\nFROM channel_models") ||
			strings.Contains(body, "DELETE FROM channel_models")
		if writes && name != "SetModelEnabled" {
			t.Errorf("%s writes channel_models; SetModelEnabled is meant to be the only one", name)
		}
	}
}

// namedQueries splits the file on sqlc's `-- name:` markers.
func namedQueries(t *testing.T) map[string]string {
	t.Helper()

	raw, err := os.ReadFile(queriesPath)
	if err != nil {
		t.Fatalf("read %s: %v", queriesPath, err)
	}

	queries := make(map[string]string)
	name := ""
	var body strings.Builder

	for line := range strings.Lines(string(raw)) {
		if rest, found := strings.CutPrefix(line, "-- name: "); found {
			if name != "" {
				queries[name] = body.String()
			}
			name, _, _ = strings.Cut(strings.TrimSpace(rest), " ")
			body.Reset()
			continue
		}
		body.WriteString(line)
	}
	if name != "" {
		queries[name] = body.String()
	}

	if len(queries) == 0 {
		t.Fatalf("%s has no named queries", queriesPath)
	}
	return queries
}
