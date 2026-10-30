# syntax=docker/dockerfile:1

# The image holds one binary that serves both the API and the web app, so the
# build needs the whole repository. Run it from the repo root:
#
#   docker build -f apps/server/Dockerfile -t openchat .
#
# Multi-platform builds (the release workflow makes linux/amd64 and
# linux/arm64) run the web and Go stages on the builder's own platform and
# cross-compile: the web bundle is the same everywhere, and Go needs no
# emulation to target another architecture.

# ---- web: build the SPA, precompressed ---------------------------------------
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
# Corepack installs the pnpm version pinned by "packageManager" in the root
# package.json, so that field stays the one place the version lives.
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable
WORKDIR /repo

# Manifests first: the dependency layer is rebuilt only when they change.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
# pnpm checks the patches the workspace lists (patchedDependencies), even ones for the app.
COPY patches patches
COPY apps/web/package.json apps/web/
COPY packages/api-client/package.json packages/api-client/
COPY packages/chat-core/package.json packages/chat-core/
RUN pnpm install --frozen-lockfile --filter "@openchat/web..."

COPY apps/web apps/web
COPY packages packages
COPY scripts/embed-web.mjs scripts/
# `build` typechecks, then runs vite build, which also writes the .br and .gz
# files next to every compressible output. embed-web copies the result to
# apps/server/internal/webui/dist, leaving the source maps behind.
RUN pnpm --filter @openchat/web build && node scripts/embed-web.mjs

# ---- server: embed the build into the binary ---------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26.10-alpine AS build
ARG TARGETOS TARGETARCH
# The version the server reports, written into the binary (config.buildVersion);
# the release workflow passes the tag. Not an ENV of the image: a container
# recreated from an old one keeps the old one's environment.
ARG APP_VERSION=dev
WORKDIR /src

COPY apps/server/go.mod apps/server/go.sum ./
RUN go mod download

COPY apps/server/ .
# Over the placeholder the repo keeps there so `go build` works without a web
# build: this is what `go:embed all:dist` picks up.
COPY --from=web /repo/apps/server/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags="-s -w -X github.com/qunqin24/openchat/apps/server/internal/config.buildVersion=$APP_VERSION" \
      -o /out/openchat ./cmd/openchat \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/adminctl ./cmd/adminctl \
 && mkdir -p /out/data

# ---- runtime -----------------------------------------------------------------
# Alpine rather than a distroless image: reading the words of a PDF takes
# poppler's pdftotext (internal/doctext), a program of its own, and
# poppler-data, without which Chinese, Japanese and Korean PDFs that use the
# standard character maps read as nothing. Alpine is the smallest base that
# has them (about 40 MB to download in all, against 70 MB on Debian slim); the
# server is a static binary, so its C library does not matter. It runs as the
# same unprivileged user (65532) the distroless image had, so a data volume
# made by an older version keeps working.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates poppler-utils poppler-data \
 && addgroup -S -g 65532 nonroot \
 && adduser -S -D -H -u 65532 -G nonroot -h /nonexistent -s /sbin/nologin nonroot
# The local storage driver writes to ./data, relative to the working directory:
# here /app/data, owned by the user the server runs as. Mount a volume on it; a
# new named volume takes this ownership from the image.
WORKDIR /app
# Secure cookies and JSON logs unless told otherwise: the image is for serving.
# The plain-HTTP trial sets development explicitly.
ENV APP_ENV=production
# A copy of the log on the data volume, rotated at 20 MB with 5 kept: the
# container's own log goes when the container is replaced, as on an upgrade.
ENV LOG_FILE=/app/data/logs/openchat.log
COPY --from=build --chown=nonroot:nonroot /out/data /app/data
COPY --from=build /out/openchat /app/openchat
# For when the only administrator is locked out (see docs/deployment.md):
#   docker compose run --rm --entrypoint /app/adminctl openchat -email … -password …
COPY --from=build /out/adminctl /app/adminctl
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/openchat"]
