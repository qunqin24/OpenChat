package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// Fingerprints are read as keytool prints them or not, and written as
// assetlinks.json and the app's origin want them.
func TestAndroidApps(t *testing.T) {
	colons := strings.TrimSuffix(strings.Repeat("ab:", 32), ":")
	apps, err := androidApps([]AndroidApp{{PackageName: " org.example.openchat ", Fingerprints: []string{colons, strings.Repeat("CD", 32)}}})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSuffix(strings.Repeat("AB:", 32), ":")
	if apps[0].PackageName != "org.example.openchat" || apps[0].Fingerprints[0] != want {
		t.Errorf("normalised: %+v", apps[0])
	}
	origins := passkeyConfig{AndroidApps: apps}.androidOrigins()
	// 32 bytes of 0xAB, base64url without padding.
	if len(origins) != 2 || origins[0] != "android:apk-key-hash:q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s" {
		t.Errorf("origins: %v", origins)
	}

	for name, bad := range map[string]AndroidApp{
		"one segment":       {PackageName: "openchat", Fingerprints: []string{want}},
		"starts with digit": {PackageName: "org.1example", Fingerprints: []string{want}},
		"short fingerprint": {PackageName: "org.example.app", Fingerprints: []string{"AB:CD"}},
		"not hex":           {PackageName: "org.example.app", Fingerprints: []string{strings.Repeat("ZZ", 32)}},
		"no fingerprint":    {PackageName: "org.example.app"},
	} {
		if _, err := androidApps([]AndroidApp{bad}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAssetLinksShape(t *testing.T) {
	link := assetLink{
		Relation: []string{"delegate_permission/common.get_login_creds"},
		Target:   assetLinkTarget{Namespace: "android_app", PackageName: "org.example.openchat", Fingerprints: []string{"AB"}},
	}
	raw, _ := json.Marshal([]assetLink{link})
	want := `[{"relation":["delegate_permission/common.get_login_creds"],"target":{"namespace":"android_app","package_name":"org.example.openchat","sha256_cert_fingerprints":["AB"]}}]`
	if string(raw) != want {
		t.Errorf("assetlinks:\n%s\nwant\n%s", raw, want)
	}
}
