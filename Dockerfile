# Builds the dev-server dashboard UI fresh from source, matching upstream's
# goreleaser `make build-ui` step. Without this stage, go:embed picks up
# whatever stale pkg/devserver/static/ snapshot happens to be committed.
FROM --platform=$BUILDPLATFORM node:22-alpine AS ui-build
WORKDIR /app
COPY ui ui
COPY pkg/coreapi pkg/coreapi
RUN corepack enable && corepack prepare pnpm@10.18.2 --activate
WORKDIR /app/ui
RUN pnpm install --frozen-lockfile
WORKDIR /app/ui/apps/dev-server-ui
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine AS build
RUN apk add build-base
WORKDIR /app
COPY vendor vendor
COPY . .
# Vite content-hashes asset filenames, so a plain COPY would merge the fresh
# build into the committed snapshot rather than replacing it, leaving stale
# hashed assets (e.g. the buggy index-*.js) embedded alongside the new ones.
RUN rm -rf pkg/devserver/static/*
COPY --from=ui-build /app/ui/apps/dev-server-ui/dist pkg/devserver/static
ARG TARGETARCH
ARG TARGETOS
RUN --mount=type=cache,target=/root/.cache/go-build \
    GOFLAGS=-mod=vendor GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /go/bin/inngest ./cmd

FROM alpine:3.24 AS inngest
RUN apk add --no-cache ca-certificates tzdata curl jq && update-ca-certificates
COPY --from=build /go/bin/inngest /bin/inngest
COPY --chmod=0555 scripts/awcp-bootstrap.sh /bin/awcp-bootstrap
CMD ["inngest"]
