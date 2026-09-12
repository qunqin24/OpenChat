package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestCursorRoundTrip(t *testing.T) {
	id := uuid.New()
	ts := pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Nanosecond), Valid: true}

	encoded := encodeCursor(ts, id)
	gotTS, gotID, err := decodeCursor(&encoded)
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if gotID == nil || *gotID != id {
		t.Errorf("id = %v, want %v", gotID, id)
	}
	if !gotTS.Time.Equal(ts.Time) {
		t.Errorf("timestamp = %v, want %v", gotTS.Time, ts.Time)
	}
}

func TestDecodeCursorEmpty(t *testing.T) {
	ts, id, err := decodeCursor(nil)
	if err != nil {
		t.Fatalf("decodeCursor(nil): %v", err)
	}
	if ts.Valid || id != nil {
		t.Errorf("nil cursor should decode to zero values, got %v / %v", ts, id)
	}
}

func TestDecodeCursorGarbage(t *testing.T) {
	bad := "not-a-cursor!!"
	if _, _, err := decodeCursor(&bad); err == nil {
		t.Fatal("expected an error for a malformed cursor")
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		name string
		in   *int
		want int32
	}{
		{"nil falls back to default", nil, 50},
		{"zero falls back to default", ptr(0), 50},
		{"in range is kept", ptr(10), 10},
		{"over max is clamped", ptr(1000), 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampLimit(tc.in); got != tc.want {
				t.Errorf("clampLimit(%v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
