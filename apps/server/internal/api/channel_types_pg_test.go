package api

import (
	"context"
	"testing"
)

// The served table and the database enum are two lists of the same names, and
// a type in the table but not the enum is offered in the picker and then fails
// to save with a 500. Nothing else ties them together, so this does.
func TestEveryServedChannelTypeIsInTheDatabaseEnum(t *testing.T) {
	pool := channelTestPool(t)
	var values []string
	if err := pool.QueryRow(context.Background(), "SELECT enum_range(NULL::channel_type)::text[]").Scan(&values); err != nil {
		t.Fatal(err)
	}
	inEnum := map[string]bool{}
	for _, v := range values {
		inEnum[v] = true
	}
	served := map[string]bool{}
	for _, def := range channelTypes {
		served[string(def.ID)] = true
		if !inEnum[string(def.ID)] {
			t.Errorf("channel type %q is served but the channel_type enum has no such value — add a migration", def.ID)
		}
	}
	for _, v := range values {
		if !served[v] {
			t.Errorf("the channel_type enum has %q, which no served type offers", v)
		}
	}
}
