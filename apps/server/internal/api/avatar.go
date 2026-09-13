package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
)

// gravatarSize is the pixel size requested. The largest avatar rendered today
// is 36 CSS pixels, so this covers it on a 4× display and still costs little.
const gravatarSize = "160"

// avatarURL returns where to fetch an account's picture.
//
// Gravatar keys on the SHA-256 of the lowercased, trimmed address — the same
// hash for the same person everywhere, which is the point of it and also its
// cost: fetching one tells gravatar.com that this viewer is looking at this
// account. That is a live dependency on a third party, and it is at odds with
// how fonts are handled here (self-hosted, deliberately). It is a default, not
// a commitment: when avatars can be uploaded, this function returns the stored
// URL and nothing else moves.
//
// `d=404` asks for an error rather than gravatar's generated placeholder, so an
// address with no picture registered falls through to the initials the UI
// already draws, instead of an identicon that looks nothing like the rest.
func avatarURL(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))

	u := url.URL{
		Scheme:   "https",
		Host:     "gravatar.com",
		Path:     "/avatar/" + hex.EncodeToString(sum[:]),
		RawQuery: url.Values{"d": {"404"}, "s": {gravatarSize}}.Encode(),
	}
	return u.String()
}
