package memory

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestClean(t *testing.T) {
	got, err := Clean("  Lives in\n  Shanghai \t and works\there ")
	if err != nil || got != "Lives in Shanghai and works here" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := Clean(" \n "); !errors.Is(err, ErrEmpty) {
		t.Fatalf("blank: %v", err)
	}
	if _, err := Clean(strings.Repeat("好", MaxContent)); err != nil {
		t.Fatalf("a note of exactly the limit, in characters not bytes: %v", err)
	}
	if _, err := Clean(strings.Repeat("好", MaxContent+1)); !errors.Is(err, ErrTooLong) {
		t.Fatalf("too long: %v", err)
	}
}

func TestPrompt(t *testing.T) {
	snap := Snapshot{
		About: "A Go developer",
		Style: "Terse",
		Memories: []SnapshotMemory{
			{Ref: "m1", ID: uuid.New(), Content: "Lives in Shanghai"},
		},
		// Memories saved later are never in the prompt.
		Added: []SnapshotRef{{Ref: "m2", ID: uuid.New()}},
	}
	with := snap.Prompt(true, true)
	for _, want := range []string{"A Go developer", "Terse", "- [m1] Lives in Shanghai"} {
		if !strings.Contains(with, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, with)
		}
	}
	if strings.Contains(with, "m2") {
		t.Fatal("a memory saved during the conversation is in the prompt")
	}
	if plain := snap.Prompt(true, false); strings.Contains(plain, "[m1]") || !strings.Contains(plain, "- Lives in Shanghai") {
		t.Fatalf("ids shown without the tool:\n%s", plain)
	}
	if without := snap.Prompt(false, true); strings.Contains(without, "Shanghai") || !strings.Contains(without, "A Go developer") {
		t.Fatalf("memories not left out:\n%s", without)
	}
	if (Snapshot{}).Prompt(true, true) != "" {
		t.Fatal("an empty snapshot says something")
	}
	if snap.Prompt(true, true) != snap.Prompt(true, true) {
		t.Fatal("not deterministic")
	}
}
