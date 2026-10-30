package api

import (
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"

	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// Passkeys from the Android app. A browser's answer says which page it came
// from (https://chat.example.com); the app's says which signing key it was
// built with: `android:apk-key-hash:<base64url SHA-256 of the certificate>`.
// The administrator lists the app - its package name and the fingerprints of
// the keys it is signed with (release, and a debug build's) - and that list
// is both the opaque origins go-webauthn accepts (RPOpaqueOrigins) and the
// Digital Asset Links statement Android looks up on the RP ID's host
// (/.well-known/assetlinks.json) before it lets the app use that domain's
// passkeys. Nothing here is per user, and the statement is public by design.

// androidApp is an app allowed this site's passkeys.
type androidApp struct {
	PackageName  string   `json:"packageName"`
	Fingerprints []string `json:"fingerprints"`
}

// packageName is what Android accepts as an application id: two or more
// dot-separated segments, each starting with a letter.
var packageName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`)

// fingerprintBytes reads a SHA-256 certificate fingerprint written as hex,
// with or without colons or spaces (as keytool prints it, or not).
func fingerprintBytes(fingerprint string) ([]byte, bool) {
	clean := strings.NewReplacer(":", "", " ", "", "-", "").Replace(strings.TrimSpace(fingerprint))
	raw, err := hex.DecodeString(clean)
	if err != nil || len(raw) != 32 {
		return nil, false
	}
	return raw, true
}

// canonicalFingerprint is a fingerprint as assetlinks.json writes it:
// uppercase hex pairs joined by colons.
func canonicalFingerprint(raw []byte) string {
	pairs := make([]string, len(raw))
	for i, b := range raw {
		pairs[i] = strings.ToUpper(hex.EncodeToString([]byte{b}))
	}
	return strings.Join(pairs, ":")
}

// androidApps checks and normalises the apps an administrator lists.
func androidApps(in []AndroidApp) ([]androidApp, error) {
	out := []androidApp{}
	for _, app := range in {
		name := strings.TrimSpace(app.PackageName)
		if !packageName.MatchString(name) {
			return nil, httpx.NewFieldError("androidApps", "invalid_package_name",
				"a package name is dot-separated, such as org.example.openchat")
		}
		if len(app.Fingerprints) == 0 {
			return nil, httpx.NewFieldError("androidApps", "invalid_fingerprint",
				"list the SHA-256 fingerprint of the certificate the app is signed with")
		}
		fingerprints := []string{}
		for _, fingerprint := range app.Fingerprints {
			raw, ok := fingerprintBytes(fingerprint)
			if !ok {
				return nil, httpx.NewFieldError("androidApps", "invalid_fingerprint",
					"a fingerprint is the 32-byte SHA-256 of the signing certificate, in hex")
			}
			fingerprints = append(fingerprints, canonicalFingerprint(raw))
		}
		out = append(out, androidApp{PackageName: name, Fingerprints: fingerprints})
	}
	return out, nil
}

// androidOrigins are the origins the listed apps' answers carry.
func (c passkeyConfig) androidOrigins() []string {
	origins := []string{}
	for _, app := range c.AndroidApps {
		for _, fingerprint := range app.Fingerprints {
			if raw, ok := fingerprintBytes(fingerprint); ok {
				origins = append(origins, "android:apk-key-hash:"+base64.RawURLEncoding.EncodeToString(raw))
			}
		}
	}
	return origins
}

// assetLink is one statement of a Digital Asset Links file.
type assetLink struct {
	Relation []string        `json:"relation"`
	Target   assetLinkTarget `json:"target"`
}

type assetLinkTarget struct {
	Namespace    string   `json:"namespace"`
	PackageName  string   `json:"package_name"`
	Fingerprints []string `json:"sha256_cert_fingerprints"`
}

// AssetLinks is /.well-known/assetlinks.json: the apps this site lets use
// its passkeys (get_login_creds), so Android lets them. Public, outside the
// API and its contract, at the path Android asks for; an empty list when
// passkeys are off or no app is listed.
func (s *Server) AssetLinks(w http.ResponseWriter, r *http.Request) {
	config, err := loadPasskeys(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	links := []assetLink{}
	if config.Enabled {
		for _, app := range config.AndroidApps {
			links = append(links, assetLink{
				Relation: []string{"delegate_permission/common.get_login_creds"},
				Target: assetLinkTarget{
					Namespace: "android_app", PackageName: app.PackageName, Fingerprints: app.Fingerprints,
				},
			})
		}
	}
	// Android caches it; a change reaches phones within minutes, not days.
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.JSON(w, http.StatusOK, links)
}
