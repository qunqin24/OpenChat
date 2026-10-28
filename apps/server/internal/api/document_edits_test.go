package api

import (
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

func TestDocumentDiff(t *testing.T) {
	v := func(title, content string) sqlc.DocumentVersion {
		return sqlc.DocumentVersion{Title: title, Content: content}
	}
	before := v("Leave", "Dear Sam,\n\nI would like Friday off.\n\nThe handover is on Thursday.\n\nThanks,\nAlice")

	t.Run("a changed paragraph, with the one before it", func(t *testing.T) {
		got, fits := documentDiff(before, v("Leave", "Dear Sam,\n\nI would like Friday off, for a wedding.\n\nThe handover is on Thursday.\n\nThanks,\nAlice"))
		want := "  Dear Sam,\n- I would like Friday off.\n+ I would like Friday off, for a wedding.\n"
		if !fits || got != want {
			t.Errorf("got %q", got)
		}
	})

	t.Run("separate changes, a new title, and lines of one paragraph", func(t *testing.T) {
		got, _ := documentDiff(before, v("Leave request", "Hi Sam,\n\nI would like Friday off.\n\nThe handover is on Thursday.\n\nThank you,\nAlice"))
		for _, want := range []string{"Title: \"Leave\" -> \"Leave request\"\n", "- Dear Sam,\n+ Hi Sam,\n", "…\n", "  The handover is on Thursday.\n- Thanks,\n- Alice\n+ Thank you,\n+ Alice\n"} {
			if !strings.Contains(got, want) {
				t.Errorf("%q lacks %q", got, want)
			}
		}
	})

	t.Run("too long to show", func(t *testing.T) {
		if _, fits := documentDiff(before, v("Leave", strings.Repeat("word ", maxDocumentDiff))); fits {
			t.Error("a diff past the bound fits")
		}
	})
}
