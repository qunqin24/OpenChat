package api

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// Two channels may not share a name, whatever its case or the spaces around
// it, and the clash comes back as an error on the form's name field.
func TestChannelNamesAreUnique(t *testing.T) {
	q := sqlc.New(channelTestPool(t))
	ctx := context.Background()
	name := "Relay " + uuid.NewString()
	create := func(name string) error {
		_, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{Name: name, Type: sqlc.ChannelTypeOpenai, Enabled: true, Weight: 1})
		return err
	}
	if err := create(name); err != nil {
		t.Fatal(err)
	}
	err := create("  " + name + "  ")
	if err == nil {
		err = create(name)
	}
	if err == nil {
		t.Fatal("a second channel took the same name")
	}
	var apiErr *httpx.APIError
	if dup := duplicateError(err); !errors.As(dup, &apiErr) || apiErr.Code != "channel_name_taken" || apiErr.Field != "name" {
		t.Fatalf("the clash reads as %v", dup)
	}
	if err := create(name + " (copy)"); err != nil {
		t.Fatalf("a different name was refused: %v", err)
	}
}
