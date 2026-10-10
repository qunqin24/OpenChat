# Deployment guide

[简体中文](deployment.md) · **English** · [Back to the README](../README.en.md)

This guide takes you from nothing to OpenChat on your own server: Docker Compose runs Postgres, the app and Caddy, and Caddy issues the HTTPS certificate on its own.

**Contents**

- [How it runs](#how-it-runs)
- [Requirements](#requirements)
- [Configuration](#configuration)
- [First start](#first-start)
- [After it is up](#after-it-is-up)
- [Updating](#updating)
- [Backup and restore](#backup-and-restore)
- [Switching attachments to S3](#switching-attachments-to-s3)
- [Behind another reverse proxy or a CDN](#behind-another-reverse-proxy-or-a-cdn)
- [Running without Docker](#running-without-docker)
- [Releasing a new version](#releasing-a-new-version)
- [Troubleshooting](#troubleshooting)

## How it runs

`deploy/docker-compose.yml` defines three services:

| Service | Image | Role |
| --- | --- | --- |
| `postgres` | `postgres:18-alpine` | Holds all application data |
| `openchat` | `ghcr.io/qunqin24/openchat` | The app. One Go process serves the web app and the API; the front end is embedded in the binary, and database migrations run on startup |
| `caddy` | `caddy:2-alpine` | The public entry point: ports 80/443, automatic HTTPS, HTTP/2 and HTTP/3, reverse proxy to the app |

The app container is reachable only from Caddy on port 8080; it is not published to the internet.

## Requirements

- A Linux server with Docker and the Docker Compose plugin (`docker compose version` works). Images exist for amd64 and arm64.
- A domain whose A record (and AAAA, if you have IPv6) **already** points at the server. Caddy needs it on first start to obtain the certificate.
- Ports **80** and **443** (TCP) open in the server firewall and in your cloud provider's security group. Open UDP 443 too; HTTP/3 uses it.

## Configuration

```bash
git clone https://github.com/qunqin24/OpenChat.git openchat && cd openchat
cp deploy/.env.example deploy/.env
```

Edit `deploy/.env`. It contains a password, so never commit it; the repository already ignores it.

| Variable | Required | Default | Notes |
| --- | --- | --- | --- |
| `DOMAIN` | yes | `localhost` | The site's domain, without `https://`. Caddy uses it for the certificate and the app uses it to set `CORS_ORIGINS` |
| `POSTGRES_PASSWORD` | yes | none | The database password. Use random characters, for example `openssl rand -hex 24`. Compose refuses to start without it |
| `APP_ENV` | no | `production` | In `production` the session cookie is `Secure`, so the site must be reached over HTTPS. Change to `development` only for a trial without a domain |
| `OPENCHAT_IMAGE` | no | `ghcr.io/qunqin24/openchat:latest` | The app image. To pin a version, write `ghcr.io/qunqin24/openchat:1.0.0` |

> **About `POSTGRES_PASSWORD`**: the password is written into the data volume when the database is first initialised. Changing the value in `.env` later does not change it in the database, and the app can no longer connect. To change it, run `ALTER USER` in the database and then update `.env`.

### Pinning an image version

`:latest` moves with every stable release. To choose when to upgrade, pin a version in `deploy/.env`:

```bash
OPENCHAT_IMAGE=ghcr.io/qunqin24/openchat:1.0.0
```

An image tag is the version without the `v` (Git tag `v1.0.0` becomes image `1.0.0`). Pre-releases such as `1.1.0-rc.1` get their own tag and never move `latest`.

### Other application variables

With Compose these are already set and need no attention. They matter when you run the binary yourself (see [Running without Docker](#running-without-docker)):

| Variable | Default | Notes |
| --- | --- | --- |
| `DATABASE_URL` | a local development database | Postgres connection string. In Compose it points at the `postgres` service |
| `HTTP_ADDR` | `:8080` | Listen address |
| `CORS_ORIGINS` | `http://localhost:5173` | Allowed cross-origin sources, comma-separated. Not needed while the web app and API share an origin; Compose sets it to `https://<DOMAIN>` |
| `SHUTDOWN_TIMEOUT` | `15s` | How long to wait for in-flight requests after a stop signal |

Attachment storage has **no** environment variables. It is configured in the admin panel (see [Switching attachments to S3](#switching-attachments-to-s3)).

## First start

From the repository root:

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

The image is pulled from the GitHub Container Registry. To build it from the code you checked out instead, add `--build` to the command.

Check on it:

```bash
docker compose -f deploy/docker-compose.yml ps
docker compose -f deploy/docker-compose.yml logs -f openchat
```

When all three services are running, open `https://your-domain`. On the first visit Caddy must obtain the certificate first, which can take a few dozen seconds.

### Becoming the administrator

**The first account to register becomes the administrator.** It is exempt from the sign-up switches and needs no approval. So register yourself right after the site is up, before you share the address with anyone.

After that, an admin controls sign-up in Admin → System:

| Mode | "Allow registration" | "New users need approval" | Effect |
| --- | --- | --- | --- |
| Approval (default) | on | on | Anyone can sign up, but the account cannot sign in until an admin approves it in Admin → Users |
| Open | on | off | Accounts work immediately |
| Closed | off | (does not apply) | No self-service sign-up; only an admin can create accounts in Admin → Users |

Sign-in and sign-up attempts are rate-limited per IP, which slows password guessing and mass sign-ups.

## After it is up

### Add a model channel

Open Admin → Channels and add one:

1. Pick a channel type (Anthropic, OpenAI, Gemini, DeepSeek, OpenRouter and so on, or "OpenAI-compatible / relay" and enter your own address). For vendors with a China site and an international site, choose the one your key belongs to; the keys are not interchangeable.
2. Enter the API key.
3. Fetch the model list and tick the models to enable.

Then, in Admin → Models, adjust each model: whether it supports reasoning and tools, which thinking levels are offered, token limits, or turn off "Offer to members" so that only admins can use that model.

To have conversations titled automatically, choose a model for background tasks in Admin → System; a cheap, fast one is a good pick. If you choose none, each conversation's own model is used.

### Web search

Turn it on in Admin → Web search. There are two kinds of search service:

- **Free endpoints.** No key. Exa, Parallel, Firecrawl and Keenable take requests in turn, and a service that rate-limits is skipped. They are rate-limited and meant for light use, and search queries are sent to those services.
- **Tavily.** Needs a saved Tavily key.

Page reading does not depend on the search service and is always available.

### Turn on guest mode

Guest mode is off by default. Turn it on in Admin → System and the sign-in page gains "Use as guest". Guests need no account; their conversations, memories and instructions stay in their own browser and the server keeps none of it.

A guest's requests belong to no account, so they are limited: a maximum per IP per hour and a maximum for all guests per day; 0 means unlimited. **The limits are counted per visitor IP**, so read [Behind another reverse proxy or a CDN](#behind-another-reverse-proxy-or-a-cdn) first.

### Other system switches

Admin → System also holds: automatic context compression, memory, generative UI, questions to the user, avatar uploads and debug mode. Its "Data retention" section sets how long deleted conversations, unsent uploads and orphaned files are kept.

## Updating

[Back up](#backup-and-restore) first. Database migrations run automatically when the app starts, so there is no separate step.

**On `latest` (no pinned version):**

```bash
git pull
docker compose -f deploy/docker-compose.yml --env-file deploy/.env pull openchat
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

`git pull` brings in new deployment files; skip it if only the image changed.

**With a pinned version:** change `OPENCHAT_IMAGE` in `deploy/.env` to the new version, then run the last two commands above.

Check that the new version is up:

```bash
docker compose -f deploy/docker-compose.yml logs --tail 50 openchat
curl -sS --max-time 20 https://your-domain/healthz
```

To roll back, set the image version back **and restore the database backup you took before upgrading**. Migrations only move forward, and an older version may not understand a database a newer one has changed.

## Backup and restore

Two things need backing up, and they must be backed up together:

| Data | Where |
| --- | --- |
| The database | the `openchat_postgres-data` volume (Postgres) |
| Attachments (local storage driver) and avatars | the `openchat_openchat-data` volume, `/app/data` in the container |

Caddy's certificates live in the `openchat_caddy-data` volume. Losing them only means requesting new ones, so they are optional.

### Back up

```bash
# the database
docker compose -f deploy/docker-compose.yml exec -T postgres \
  pg_dump -U openchat openchat > openchat-$(date +%F).sql

# attachments and avatars (the volume as a tar.gz in the current directory)
docker run --rm -v openchat_openchat-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/openchat-data-$(date +%F).tar.gz -C /data .
```

If attachments have been moved to S3, new uploads are in the bucket and you back that up on the storage service's side; files that were in the volume before the switch still need backing up.

> The data directory must match the database. The app deletes files under `uploads/` that have no database row and are older than the retention time, so never point a data directory at a different database, and never restore only one of the two.

### Restore

On a fresh deployment, start only the database first:

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d postgres

# restore the database
docker compose -f deploy/docker-compose.yml exec -T postgres \
  psql -U openchat openchat < openchat-2026-01-01.sql

# restore the attachments volume (docker creates it if it does not exist)
docker run --rm -v openchat_openchat-data:/data -v "$PWD":/backup alpine \
  tar xzf /backup/openchat-data-2026-01-01.tar.gz -C /data
```

Then start everything: `docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d`.

> Restore into a database the app has never run against (as above, with only `postgres` started). Once the app has started it creates its tables, and importing into a database that already has them reports "already exists".

## Switching attachments to S3

By default attachments are stored on the server's local disk (the `openchat-data` volume). To use S3-compatible storage (AWS S3, Cloudflare R2, MinIO and others) you do not need to change any deployment file:

1. Create a bucket on the storage service and **configure CORS on it**. The browser `PUT`s files straight to the bucket, so the bucket must allow your site's origin, the `PUT` method and the `Content-Type` header, or the browser blocks the upload. An S3-style example:

   ```json
   [
     {
       "AllowedOrigins": ["https://chat.example.com"],
       "AllowedMethods": ["PUT", "GET"],
       "AllowedHeaders": ["Content-Type"],
       "MaxAgeSeconds": 3000
     }
   ]
   ```

2. Open Admin → File storage, choose "S3-compatible storage" as the driver, and fill in Endpoint (leave it empty for AWS S3), Bucket, Region, Access Key ID and Secret Access Key. MinIO and similar services need "path style" turned on.
3. Press "Test connection". It writes a small file, reads it back and deletes it, without saving the settings.
4. Save once it passes. **It takes effect immediately; no restart.**

The secret is write-only: after saving, only its last four characters are shown. If you leave the access key empty, the server uses the default AWS credentials in its own environment.

Switching does not orphan old attachments: a read looks on local disk first and then in S3, so old files stay in the `openchat-data` volume and remain readable, and only new uploads go to the bucket. Keep backing that volume up.

## Behind another reverse proxy or a CDN

The bundled Caddy already does all of this. If you want your own nginx or Traefik, or there is a CDN such as Cloudflare in front, mind the following.

### Handing the app to your own proxy

Remove the `caddy` service (or do not start it: `up -d postgres openchat`) and make the app listen on the host only, replacing the `openchat` service's `expose` with:

```yaml
    ports:
      - "127.0.0.1:8080:8080"
```

Then have your proxy forward the whole site to `127.0.0.1:8080` and terminate HTTPS. An nginx example is in [deploy-caddy.md](deploy-caddy.md) (Chinese).

The proxy must:

- **Not buffer the event streams.** Sending a message and the conversation events are SSE (`POST /api/v1/conversations/{id}/messages`, `GET .../events`). Turn buffering off and give them a long read timeout, or long answers are cut off.
- **Serve HTTPS.** In production mode the session cookie is `Secure`, and browsers send it only over HTTPS.

### Per-IP limits depend on correct forwarded headers

Guest limits and the sign-in and sign-up attempt limits are all **counted per visitor IP**. The app takes the IP from request headers: `True-Client-IP` first, then `X-Real-IP`, and only then `X-Forwarded-For` (its leftmost value); with none of them it uses the connection's peer address. So:

- Your outermost proxy must **overwrite** these headers with the real visitor address and drop any value the visitor sent. Otherwise anyone can claim any address and slip past the limits.
- If another proxy or a CDN sits further out, make sure the address is passed correctly through every hop. If it is not, the app sees the proxy's address for everyone and all visitors share one allowance: the guest limit runs out quickly, and failed sign-ins are counted site-wide, so a few mistyped passwords can lock everyone out.
- With Caddy behind a CDN (Cloudflare, for example), Caddy sees the CDN's address as the peer. Make Caddy trust the CDN's address ranges (Caddy's `trusted_proxies` option) so it takes the visitor address from the CDN's header; see Caddy's documentation for the exact syntax.

The `deploy/Caddyfile` in the repository is right when there is no CDN: it overwrites the forwarding headers with the connection address. Understand the points above before you change it.

## Running without Docker

The app is one binary with the front end embedded. To build it from source you need Go 1.26, Node.js 22 and pnpm:

```bash
make build        # builds the web app, then compiles bin/openchat
```

Then provide a Postgres 18, set the environment and run the binary:

```bash
export APP_ENV=production
export DATABASE_URL='postgres://user:password@host:5432/dbname?sslmode=disable'
export CORS_ORIGINS='https://your-domain'
./bin/openchat    # listens on :8080, migrates on start
```

It serves the web app and `/api/v1` together. You still need a reverse proxy in front that terminates HTTPS, with the same requirements as above. The local attachment directory is `./data` under the process's working directory; keep it on persistent disk.

The binary has two more subcommands: `./bin/openchat migrate` only runs migrations, and `./bin/openchat rollback` reverts the latest one.

## Releasing a new version

This section is for the repository maintainer. A release is triggered by a Git tag:

```bash
git tag v1.0.0 && git push origin v1.0.0
```

When a `v*` tag is pushed, the Release workflow in GitHub Actions:

1. builds the image for `linux/amd64` and `linux/arm64` and pushes it to `ghcr.io/qunqin24/openchat` as `1.0.0` and `latest`;
2. creates a GitHub Release of the same name, with notes generated from the commits and the `docker pull` command.

A tag with a suffix (for example `v1.1.0-rc.1`) pushes only `1.1.0-rc.1`, leaves `latest` alone, and the Release is marked as a pre-release.

Before tagging, make sure CI on `main` is green (`make check` runs the same set locally).

> **Check the image's visibility after the first release.** A newly created GitHub container package may be private by default. If `docker pull` is denied for others, open `openchat` under Packages on the repository page and set its visibility to Public in the package settings.

## Troubleshooting

### Signed in, then immediately back at the sign-in page

The most common cause: the site is opened over `http://` while `APP_ENV` is `production`. In production mode the session cookie is `Secure` and browsers do not send it over HTTP, so sign-in "succeeds" and you are logged out at once. Use HTTPS. For a temporary trial, see ["Trying it without a domain"](../README.en.md#trying-it-without-a-domain) in the README.

Also check that the browser is not blocking cookies and that your proxy does not strip `Set-Cookie`.

### The HTTPS certificate is not issued

Read Caddy's log first:

```bash
docker compose -f deploy/docker-compose.yml logs caddy
```

Then work through these:

- Does the domain's A/AAAA record point at this server? A stray AAAA record pointing elsewhere is a common trap.
- Are 80 and 443 reachable from outside? Check both the server firewall and the cloud security group.
- Is another program using 80/443 (`ss -ltnp | grep -E ':(80|443)\b'`)?
- Is the domain behind a CDN in proxy mode? That can block the validation request. Turn the proxy off until the certificate is issued, then back on.
- Repeated failures in a short time trigger the certificate authority's rate limits; wait a while and try again.

### Pulling the image is denied (denied / unauthorized)

The image package is private. Ask the maintainer to set the `ghcr.io/qunqin24/openchat` package to Public (see [Releasing a new version](#releasing-a-new-version)), or run `docker login ghcr.io` first.

### The app keeps restarting, or Caddy answers 502

Read the app's log:

```bash
docker compose -f deploy/docker-compose.yml logs --tail 100 openchat
```

Common causes: the database cannot be reached (`POSTGRES_PASSWORD` was changed after the database was initialised, see the note under [Configuration](#configuration)); or the app is still starting or migrating, so wait a few seconds and retry.

### Logs, and more detailed logs

```bash
docker compose -f deploy/docker-compose.yml logs -f openchat   # the app
docker compose -f deploy/docker-compose.yml logs -f caddy      # the proxy and certificates
docker compose -f deploy/docker-compose.yml logs -f postgres   # the database
```

Admin → System has a "Debug mode" switch. It logs at debug level, takes effect at once without a restart, produces a lot of output and may include request details, so turn it off when you are done.

### Attachment uploads fail (with S3)

Most likely the bucket has no CORS rules; see [Switching attachments to S3](#switching-attachments-to-s3). Use "Test connection" in the admin panel first to confirm that the keys and the bucket themselves work.

### Is the service alive?

```bash
curl -sS --max-time 20 https://your-domain/healthz
```
