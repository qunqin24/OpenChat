package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"
	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// A passkey from registration to sign-in, with a software authenticator
// standing in for the browser's: each answer is checked against its
// challenge, once, from the right origin, by the account it was made for.
func TestPasskeys(t *testing.T) {
	pool := dbtest.Pool(t, "api_passkeys")
	ctx := context.Background()
	q := sqlc.New(pool)
	server := &Server{pool: pool, q: q, cfg: &config.Config{}}
	_, _ = pool.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, settingPasskeys)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = $1`, settingPasskeys)
	})

	tag := uuid.NewString()
	newUser := func(name string) sqlc.User {
		var u sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
			name+tag+"@test.invalid", name+tag).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", u.ID) })
		return u
	}
	alice, mallory := newUser("alice"), newUser("mallory")

	const origin = "https://chat.example.test"
	rp := virtualwebauthn.RelyingParty{ID: "chat.example.test", Name: "OpenChat", Origin: origin}
	device := virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{UserHandle: alice.ID[:], BackupEligible: true})
	key := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	call := func(user *sqlc.User, body any, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
		req.Header.Set("Origin", origin)
		// Each from an address of its own, so failures here do not add up
		// to the sign-in limit.
		req.RemoteAddr = "192.0.2." + uuid.NewString()[:2] + ":1234"
		if user != nil {
			req = req.WithContext(auth.WithUser(req.Context(), *user))
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}
	options := func(rec *httptest.ResponseRecorder) (PasskeyOptions, string) {
		t.Helper()
		var out PasskeyOptions
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("options: %d %s", rec.Code, rec.Body)
		}
		publicKey, _ := json.Marshal(out.PublicKey)
		return out, string(publicKey)
	}
	answer := func(challenge uuid.UUID, response string) PasskeyAnswer {
		var credential map[string]any
		if err := json.Unmarshal([]byte(response), &credential); err != nil {
			t.Fatal(err)
		}
		name := "My laptop"
		return PasskeyAnswer{ChallengeId: challenge, Credential: credential, Name: &name}
	}

	// Registration: the challenge, then the authenticator's answer.
	begun, publicKey := options(call(&alice, nil, server.BeginPasskeyRegistration))
	attestation, err := virtualwebauthn.ParseAttestationOptions(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	if attestation.RelyingPartyID != rp.ID || attestation.UserID != string(alice.ID[:]) {
		t.Fatalf("registration options: %+v", attestation)
	}
	response := virtualwebauthn.CreateAttestationResponse(rp, device, key, *attestation)
	rec := call(&alice, answer(begun.ChallengeId, response), server.AddPasskey)
	var added Passkey
	if err := json.Unmarshal(rec.Body.Bytes(), &added); err != nil || rec.Code != http.StatusCreated || added.Name != "My laptop" || !added.Synced {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	device.AddCredential(key)
	// The same answer again: its challenge is spent.
	if rec := call(&alice, answer(begun.ChallengeId, response), server.AddPasskey); rec.Code != http.StatusBadRequest {
		t.Errorf("replayed registration: %d %s", rec.Code, rec.Body)
	}
	// Another account cannot answer alice's challenge.
	begun, publicKey = options(call(&alice, nil, server.BeginPasskeyRegistration))
	attestation, _ = virtualwebauthn.ParseAttestationOptions(publicKey)
	other := virtualwebauthn.CreateAttestationResponse(rp, device, virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2), *attestation)
	if rec := call(&mallory, answer(begun.ChallengeId, other), server.AddPasskey); rec.Code != http.StatusBadRequest {
		t.Errorf("another account's challenge: %d %s", rec.Code, rec.Body)
	}

	signIn := func(from string) *httptest.ResponseRecorder {
		begun, publicKey := options(call(nil, nil, server.BeginPasskeySignIn))
		assertion, err := virtualwebauthn.ParseAssertionOptions(publicKey)
		if err != nil {
			t.Fatal(err)
		}
		site := rp
		site.Origin = from
		return call(nil, answer(begun.ChallengeId, virtualwebauthn.CreateAssertionResponse(site, device, key, *assertion)), browserPasskeySignIn(server))
	}

	// Sign-in: a session for alice, and the passkey's last use noted.
	rec = signIn(origin)
	var signed SessionUser
	if err := json.Unmarshal(rec.Body.Bytes(), &signed); err != nil || rec.Code != http.StatusOK || signed.Id != alice.ID {
		t.Fatalf("sign in: %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("no session cookie")
	}
	rec = call(&alice, nil, server.ListPasskeys)
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"lastUsedAt"`)) {
		t.Errorf("list after use: %s", rec.Body)
	}

	// An answer made for another origin does not sign in.
	if rec := signIn("https://evil.example.test"); rec.Code != http.StatusUnauthorized {
		t.Errorf("other origin: %d %s", rec.Code, rec.Body)
	}

	// The Android app: its answer names its signing key, not a page. Refused
	// until the app is listed, accepted once it is.
	fingerprint := strings.Repeat("AB", 32)
	raw, _ := hex.DecodeString(fingerprint)
	appOrigin := "android:apk-key-hash:" + base64.RawURLEncoding.EncodeToString(raw)
	if rec := signIn(appOrigin); rec.Code != http.StatusUnauthorized {
		t.Errorf("an unlisted app: %d %s", rec.Code, rec.Body)
	}
	apps := []AndroidApp{{PackageName: "org.example.openchat", Fingerprints: []string{fingerprint}}}
	if rec := call(&alice, UpdatePasskeySettingsRequest{AndroidApps: &apps}, server.UpdatePasskeySettings); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), appOrigin) {
		t.Fatalf("list the app: %d %s", rec.Code, rec.Body)
	}
	if rec := signIn(appOrigin); rec.Code != http.StatusOK {
		t.Errorf("a listed app: %d %s", rec.Code, rec.Body)
	}
	links := httptest.NewRecorder()
	server.AssetLinks(links, httptest.NewRequest(http.MethodGet, "/.well-known/assetlinks.json", nil))
	if !strings.Contains(links.Body.String(), `"package_name":"org.example.openchat"`) || !strings.Contains(links.Body.String(), "get_login_creds") {
		t.Errorf("assetlinks: %s", links.Body)
	}

	// A disabled account is told so, and gets no session.
	if _, err := pool.Exec(ctx, `UPDATE users SET disabled_at = now() WHERE id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	if rec := signIn(origin); rec.Code != http.StatusForbidden || rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("disabled account: %d %s", rec.Code, rec.Body)
	}
	_, _ = pool.Exec(ctx, `UPDATE users SET disabled_at = NULL WHERE id = $1`, alice.ID)

	// Someone else's passkey is not theirs to rename or remove.
	rename := func(user sqlc.User) int {
		raw, _ := json.Marshal(RenamePasskeyRequest{Name: "x"})
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(raw))
		rec := httptest.NewRecorder()
		server.RenamePasskey(rec, req.WithContext(auth.WithUser(req.Context(), user)), added.Id)
		return rec.Code
	}
	if code := rename(mallory); code != http.StatusNotFound {
		t.Errorf("rename another's: %d", code)
	}
	if code := rename(alice); code != http.StatusOK {
		t.Errorf("rename own: %d", code)
	}

	// Turned off: no sign-in, and the options say so.
	off := false
	if rec := call(&alice, UpdatePasskeySettingsRequest{Enabled: &off}, server.UpdatePasskeySettings); rec.Code != http.StatusOK {
		t.Fatalf("turn off: %d %s", rec.Code, rec.Body)
	}
	if rec := call(nil, nil, server.BeginPasskeySignIn); rec.Code != http.StatusNotFound {
		t.Errorf("sign-in while off: %d", rec.Code)
	}

	// Removed: it signs in no more.
	del := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec = httptest.NewRecorder()
	server.DeletePasskey(rec, del.WithContext(auth.WithUser(del.Context(), alice)), added.Id)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
}

// With the RP ID pinned to a parent domain and no origins listed, a sibling
// subdomain - which the browser lets use that RP ID - cannot relay a real
// answer: only the server's own origins count.
func TestPasskeySiblingSubdomainIsRefused(t *testing.T) {
	pool := dbtest.Pool(t, "api_passkeys_sibling")
	ctx := context.Background()
	q := sqlc.New(pool)
	const app, evil = "https://chat.example.test", "https://evil.example.test"
	server := &Server{pool: pool, q: q, cfg: &config.Config{CORSOrigins: []string{app}}}
	settings, _ := json.Marshal(passkeyConfig{Enabled: true, RPID: "example.test", UserVerification: "preferred"})
	if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingPasskeys, Value: settings}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = $1`, settingPasskeys)
	})

	tag := uuid.NewString()
	var alice sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		"sib"+tag+"@test.invalid", "sib"+tag).Scan(&alice.ID, &alice.Email, &alice.Name, &alice.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", alice.ID) })

	call := func(origin string, user *sqlc.User, body any, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
		req.Header.Set("Origin", origin)
		req.RemoteAddr = "198.51.100." + uuid.NewString()[:2] + ":1234"
		if user != nil {
			req = req.WithContext(auth.WithUser(req.Context(), *user))
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}
	publicKeyOf := func(rec *httptest.ResponseRecorder) (PasskeyOptions, string) {
		t.Helper()
		var out PasskeyOptions
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("options: %d %s", rec.Code, rec.Body)
		}
		raw, _ := json.Marshal(out.PublicKey)
		return out, string(raw)
	}
	answerOf := func(challenge uuid.UUID, response string) PasskeyAnswer {
		var credential map[string]any
		_ = json.Unmarshal([]byte(response), &credential)
		return PasskeyAnswer{ChallengeId: challenge, Credential: credential}
	}

	device := virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{UserHandle: alice.ID[:]})
	key := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	begun, publicKey := publicKeyOf(call(app, &alice, nil, server.BeginPasskeyRegistration))
	attestation, err := virtualwebauthn.ParseAttestationOptions(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	rp := virtualwebauthn.RelyingParty{ID: "example.test", Name: "OpenChat", Origin: app}
	if rec := call(app, &alice, answerOf(begun.ChallengeId, virtualwebauthn.CreateAttestationResponse(rp, device, key, *attestation)), server.AddPasskey); rec.Code != http.StatusCreated {
		t.Fatalf("add from the app: %d %s", rec.Code, rec.Body)
	}
	device.AddCredential(key)

	signIn := func(origin string) int {
		begun, publicKey := publicKeyOf(call(origin, nil, nil, server.BeginPasskeySignIn))
		assertion, err := virtualwebauthn.ParseAssertionOptions(publicKey)
		if err != nil {
			t.Fatal(err)
		}
		site := rp
		site.Origin = origin
		return call(origin, nil, answerOf(begun.ChallengeId, virtualwebauthn.CreateAssertionResponse(site, device, key, *assertion)), browserPasskeySignIn(server)).Code
	}
	if code := signIn(evil); code != http.StatusUnauthorized {
		t.Errorf("relayed from a sibling subdomain: %d, want 401", code)
	}
	if code := signIn(app); code != http.StatusOK {
		t.Errorf("from the app itself: %d, want 200", code)
	}
}

// browserPasskeySignIn is a browser's passkey sign-in: no app header.
func browserPasskeySignIn(server *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		server.PasskeySignIn(w, r, PasskeySignInParams{})
	}
}
