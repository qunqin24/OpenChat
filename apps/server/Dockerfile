# syntax=docker/dockerfile:1

# The image holds one binary that serves both the API and the web app, so the
# build needs the whole repository. Run it from the repo root:
#
#   docker build -f apps/server/Dockerfile -t openchat .

# ---- web: build the SPA, precompressed ---------------------------------------
FROM node:22-alpine AS web
# Corepack installs the pnpm version pinned by "packageManager" in the root
# package.json, so that field stays the one place the version lives.
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable
WORKDIR /repo

# Manifests first: the dependency layer is rebuilt only when they change.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/
COPY packages/api-client/package.json packages/api-client/
RUN pnpm install --frozen-lockfile --filter "@openchat/web..."

COPY apps/web apps/web
COPY packages packages
COPY scripts/embed-web.mjs scripts/
# `build` typechecks, then runs vite build, which also writes the .br and .gz
# files next to every compressible output. embed-web copies the result to
# apps/server/internal/webui/dist, leaving the source maps behind.
RUN pnpm --filter @openchat/web build && node scripts/embed-web.mjs

# ---- server: embed the build into the binary ---------------------------------
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY apps/server/go.mod apps/server/go.sum ./
RUN go mod download

COPY apps/server/ .
# Over the placeholder the repo keeps there so `go build` works without a web
# build: this is what `go:embed all:dist` picks up.
COPY --from=web /repo/apps/server/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openchat ./cmd/openchat \
 && mkdir -p /out/data

# ---- runtime -----------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
# The local storage driver writes to ./data, relative to the working directory:
# here /app/data, made in the build stage (distroless has no shell to mkdir)
# and owned by the user the server runs as. Mount a volume on it; a new named
# volume takes this ownership from the image.
WORKDIR /app
COPY --from=build --chown=nonroot:nonroot /out/data /app/data
COPY --from=build /out/openchat /app/openchat
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/openchat"]
