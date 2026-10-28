<div align="center">

<img src="design/logo-tangent.svg" width="112" height="112" alt="OpenChat">

# OpenChat

**A self-hosted AI chat app: bring your own model providers, ship it as one binary or one Docker image.**

[简体中文](README.md) · **English**

[![License: AGPL-3.0](https://img.shields.io/github/license/qunqin24/OpenChat)](LICENSE)
[![Release](https://img.shields.io/github/v/release/qunqin24/OpenChat?include_prereleases&sort=semver)](https://github.com/qunqin24/OpenChat/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/qunqin24/OpenChat/ci.yml?branch=main&label=CI)](https://github.com/qunqin24/OpenChat/actions/workflows/ci.yml)
[![Docker image](https://img.shields.io/badge/ghcr.io-qunqin24%2Fopenchat-2496ED?logo=docker&logoColor=white)](https://github.com/qunqin24/OpenChat/pkgs/container/openchat)

[Quick start](#-quick-start) · [Deployment guide](docs/deployment.en.md) · [Features](#-features) · [Development](#-development) · [Conventions](AGENTS.md)

</div>

<!--
Screenshot placeholder: uncomment the lines below once there are screenshots,
and put the images in docs/images/.
<p align="center">
  <img src="docs/images/chat.png" width="860" alt="The chat interface">
</p>
-->

## What it is

OpenChat is an AI chat website you run yourself. You enter the keys of your model vendors or relay services in the admin panel (these are called channels), and your users chat with those models in the browser. It suits:

- people who want one chat entry point for themselves, family, a team or a small community, without handing the conversations to a third-party product;
- people with API accounts at several model vendors who want one interface, one switcher and one place to see usage and cost;
- developers who want a clearly structured full-stack project (Go + React) to read and modify.

The server is a Go program with the web app embedded in the binary, and PostgreSQL holds the data. There is no Redis, no message queue and no separate front-end site.

## ✨ Features

### Models

- **Many providers.** More than twenty channel types are built in, including Anthropic, OpenAI, Google Gemini, DeepSeek, Moonshot, Zhipu, Alibaba DashScope, Volcengine, SiliconFlow, StepFun, MiniMax, OpenRouter and OpenCode Go. Ollama, LM Studio and any OpenAI-compatible relay work too. Several vendors have separate China and international sites; you pick the one your key belongs to.
- **Model marketplace.** Capabilities, context length and pricing metadata come from the [models.dev](https://models.dev) catalogue. You fetch the model list from each channel and choose which models to enable.
- **Per-model settings.** Override the model kind, reasoning and tool support, input modalities, which thinking levels are offered, and the input/output token limits. A model can also be made **admin-only**.
- **Reasoning display and effort.** Models that reason show their thinking, and users choose the effort level in the composer.

### Chat

- **Streaming answers**, which you can stop at any time; what was generated so far is kept.
- **Web search and page reading.** The model can call both. Search runs on keyless public endpoints (Exa, Parallel, Firecrawl and Keenable in turn, meant for light use) or on Tavily, with a site blocklist.
- **Generative UI.** The model can lay out answers with cards, metrics, tables, steps and similar components. Admin switch, off by default.
- **Questions to the user.** When only the user can decide something, the model asks a multiple-choice question and waits for the answer. Admin switch, on by default.
- **Attachments.** Images (PNG, JPEG, WebP, GIF, up to 5 MB each) and PDFs (up to 10 MB), when the model accepts that kind of input.
- **Editing and regenerating.** Rewriting a question, or having the model answer the last turn again, **replaces** everything that came after it. What was replaced is deleted.
- **Context window meter.** Shows how much of the model's context window the conversation has used.
- **Automatic context compression.** When a conversation nears the model's context window, earlier history is summarised by the conversation's own model and the latest question is kept verbatim. The original messages stay visible with a divider between them, and the compression can be undone. A request the model refuses for its length is compressed once and retried. Compression is only ever automatic; there is no manual button. On by default, with an admin switch.
- **Conversation search.** Search the titles and message contents of all your conversations.
- **Temporary chats.** Not listed, not titled, no memory; deleted automatically after 24 hours.

### Personalisation and privacy

- **Custom instructions and personal memory.** Users write down things about themselves and how they like answers, and the model can remember long-term facts for later conversations. Users can view, edit and delete memories at any time, and undo the model's recent changes. Administrators get a single master switch and **cannot see anyone's memories or instructions**.
- **My data.** Export all your conversations as JSON, or delete them all in one step.
- The interface is available in **Simplified Chinese and English**, with system, light and dark appearance.

### Accounts and sign-up

- **Sign-up control.** Two admin switches, "allow registration" and "new users need approval" (both on by default), give three modes: **closed**, **open** and **approval**. **The first account to register becomes the administrator**, whatever the switches say.
- A pending or disabled account signing in sees a full-window notice with the administrator to contact.
- Sign-in and sign-up attempts are rate-limited per IP.
- Sessions use an HttpOnly cookie, and the database stores only a hash of the token.

### Spend and safety

- **Per-model spending caps.** A dollar cap per model over a total, monthly or daily period; once reached the model is closed to users (administrators can be exempt). Spend comes from the usage log; a request stopped midway, or one whose provider reported no usage, is charged an estimate and marked as such.
- **Answers at once.** Each account (3 by default) can have only so many answers being written at the same time; administrators are exempt.
- **Content moderation.** User input and images are checked before they reach the upstream (OpenAI Moderation or a model of your choice), blocking only high-risk categories. A blocked request writes nothing and goes into the moderation log; repeat offenders are disabled automatically. Off by default.
- **End-user identifiers.** Requests to the official APIs carry a non-reversible identifier of the user, so a provider that finds abuse can tell which user it was rather than blaming the whole account.

### Admin panel

- **Dashboard.** Requests, tokens, cost, cache hit rate and the cost the cache saved, errors and latency, channel health, and spend by user.
- **Usage logs.** Every model request, with filters and cursor pagination.
- **Channels, models, users.** Add and remove channels, manage models, enable or disable accounts in bulk and approve new users.
- **File storage.** Attachments go to local disk or any S3-compatible store (AWS S3, MinIO, Cloudflare R2 and others). You switch in the panel, it takes effect immediately without a restart, and existing files stay readable.
- **Retention.** Set how long deleted conversations, unsent uploads and orphaned files are kept.
- **System switches.** Memory, generative UI, questions to the user, avatar uploads, the model used for background tasks such as titles, debug logging and more.

### Deployment

- **One binary with the web app embedded.** One process, one origin, no separate static site. Static assets are precompressed (brotli and gzip) at build time.
- **Multi-arch Docker image** at `ghcr.io/qunqin24/openchat`, for `linux/amd64` and `linux/arm64`.
- Database migrations are embedded in the binary and run on startup.

## 🚀 Quick start

You need a Linux server with Docker (and Compose), a domain that resolves to it, and ports 80 and 443 open.

```bash
git clone https://github.com/qunqin24/OpenChat.git openchat && cd openchat
cp deploy/.env.example deploy/.env
# Edit deploy/.env: set DOMAIN to your domain and POSTGRES_PASSWORD to random characters (openssl rand -hex 24)
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

Then open `https://your-domain`:

1. **Register the first account right away.** It becomes the administrator.
2. Go to Admin → Channels, add a model channel, then fetch and enable models.
3. Go back to the chat page and start talking.

Caddy requests and renews the HTTPS certificate on its own; the first start can take a few dozen seconds. If the certificate is not issued or sign-in does not stick, see [Troubleshooting](docs/deployment.en.md#troubleshooting).

### Trying it without a domain

In production mode the session cookie is marked `Secure`, so browsers only send it over HTTPS. Over plain HTTP you would sign in and be logged out again immediately. To just try the app, switch to development mode and publish the app port directly:

`deploy/docker-compose.trial.yml` has those changes ready; add it and leave Caddy out:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.trial.yml --env-file deploy/.env up -d postgres openchat
```

Open `http://your-server:8080`. **This is for trying only.** Use the HTTPS setup above for real use.

## 📦 Deployment guide

The full guide is **[docs/deployment.en.md](docs/deployment.en.md)**. It covers:

- requirements, configuration variables and pinning an image version;
- first start, becoming the admin, adding channels;
- updating, backup and restore;
- moving attachments to S3;
- running behind another reverse proxy or a CDN, and why per-IP limits depend on forwarded headers;
- releasing a new version;
- troubleshooting.

The short notes next to the deployment files are in [deploy/README.md](deploy/README.md) (Chinese). Using nginx instead of Caddy is in [docs/deploy-caddy.md](docs/deploy-caddy.md) (Chinese).

## 🛠 Development

You need Go 1.26, Node.js 22 and pnpm (the version is set by `packageManager` in the root `package.json`), plus a PostgreSQL 18: either start one with the repository's Docker Compose file or point `DATABASE_URL` at any existing Postgres 18.

```bash
make setup   # install dependencies, create .env
make up      # Postgres + MinIO through docker compose (skip it if you have your own Postgres)
make ser     # the API on :8080, migrates on start
make web     # Vite on :5173, proxies /api to :8080
make check   # lint + test + build, the same as CI
```

Open `http://localhost:5173`. `make help` lists every command.

- For the same single binary you deploy: `make build`, which produces `bin/openchat`.
- The contract is `api/openapi.yaml`. After changing it run `make generate`, which regenerates the Go server interface and the TypeScript client. Do not hand-edit files marked `DO NOT EDIT`.
- Database-backed Go tests read `TEST_DATABASE_URL` and are skipped without it; `make test` sets it up from the database in `.env`.
- Layout: `api/` the contract, `apps/server/` the Go API, `apps/web/` the React app, `packages/api-client/` the generated typed client, `deploy/` production files, `design/` the logo.
- Architecture, the boundary between the two design systems and the detailed behaviour of each feature are written down in **[AGENTS.md](AGENTS.md)** (in Chinese). Read it before changing code.

## 🧱 Tech stack

| Layer | Choice |
| --- | --- |
| Backend | Go, chi, pgx, sqlc, goose (migrations embedded in the binary), oapi-codegen |
| Database | PostgreSQL 18 |
| Contract | OpenAPI 3.1, generating both the Go server interface and the TypeScript client |
| Frontend | React 19, TypeScript, Vite, TanStack Router / Query, Zustand, Tailwind CSS 4 |
| UI | A design system of its own for the chat interface; shadcn/ui for the admin panel |
| i18n | i18next (Simplified Chinese, English) |
| File storage | Local disk, or S3-compatible storage |
| Deployment | One binary with the web app embedded, a distroless Docker image, Caddy for HTTPS |

## 📄 License

Copyright (C) 2026 qunqin

OpenChat is released under the [GNU Affero General Public License v3.0 (AGPL-3.0)](LICENSE). You may use, modify and distribute it, but if you distribute a modified version, or let others use a modified version over a network, you must make its complete source code available under the same license.

v1.2.0 and earlier remain available under the MIT license they were released with.
