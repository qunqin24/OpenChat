-- Passkeys and the WebAuthn ceremonies that make and use them (api/passkeys.go).
-- Every statement about a person's passkeys names the person.

-- name: ListPasskeys :many
SELECT id, name, credential, created_at, last_used_at
FROM passkeys
WHERE user_id = $1
ORDER BY created_at;

-- name: UserPasskeyCredentials :many
-- What a registration leaves out (the same authenticator twice).
SELECT credential
FROM passkeys
WHERE user_id = $1;

-- name: InsertPasskey :one
INSERT INTO passkeys (user_id, credential_id, credential, name)
VALUES ($1, $2, $3, $4)
RETURNING id, name, credential, created_at, last_used_at;

-- name: RenamePasskey :one
UPDATE passkeys
SET name = $3
WHERE id = $1
  AND user_id = $2
RETURNING id, name, credential, created_at, last_used_at;

-- name: DeletePasskey :execrows
DELETE FROM passkeys
WHERE id = $1
  AND user_id = $2;

-- name: GetPasskeyByCredential :one
-- The passkey a sign-in presented, with its account.
SELECT p.id, p.user_id, p.credential
FROM passkeys p
WHERE p.credential_id = $1;

-- name: TouchPasskey :exec
-- A sign-in with it: the credential's new sign count and flags, and when.
UPDATE passkeys
SET credential   = $2,
    last_used_at = now()
WHERE id = $1;

-- name: CountPasskeys :one
SELECT count(*)::bigint
FROM passkeys;

-- name: CreateWebAuthnChallenge :one
INSERT INTO webauthn_challenges (kind, user_id, session)
VALUES ($1, $2, $3)
RETURNING id;

-- name: TakeWebAuthnChallenge :one
-- A ceremony's challenge, once: taken whether or not the answer is good, so
-- an answer cannot be tried twice. Only one younger than five minutes.
DELETE FROM webauthn_challenges
WHERE id = $1
  AND kind = $2
  AND created_at > now() - interval '5 minutes'
RETURNING user_id, session;

-- name: PurgeWebAuthnChallenges :execrows
DELETE FROM webauthn_challenges
WHERE created_at < $1;
