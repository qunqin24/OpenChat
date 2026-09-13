package api

import (
	"strings"
	"testing"
)

// The hash keys on the trimmed, lowercased address: the same person must get
// the same picture however they happened to type it in.
func TestAvatarURLNormalisesTheAddress(t *testing.T) {
	const want = "https://gravatar.com/avatar/" +
		"962449cb408fbc28a9a3d1ace7ebf2a65f951b21ecd4f7ca4d01c41b36b54cd7" +
		"?d=404&s=160"

	for _, in := range []string{
		"alice@openchat.dev",
		"Alice@OpenChat.dev",
		"ALICE@OPENCHAT.DEV",
		"  alice@openchat.dev  ",
	} {
		if got := avatarURL(in); got != want {
			t.Errorf("avatarURL(%q)\n got %s\nwant %s", in, got, want)
		}
	}
}

// Different addresses must not collide, and `d=404` has to survive — without it
// gravatar serves a generated identicon and the initials fallback never runs.
func TestAvatarURLCarriesTheFallbackParameter(t *testing.T) {
	if avatarURL("alice@openchat.dev") == avatarURL("bob@openchat.dev") {
		t.Fatal("two addresses produced the same avatar URL")
	}
	if got := avatarURL("alice@openchat.dev"); !strings.Contains(got, "d=404") {
		t.Errorf("avatarURL lost d=404: %s", got)
	}
}
