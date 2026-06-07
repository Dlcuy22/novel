# Novel toolchain image.
#
# Builds the `novel` CLI from source, then assembles a self-contained Alpine
# runtime image that carries a global NVLPATH store (core runtime + global
# modules) and a demo app. A build-time smoke test proves the store resolves.
#
#   docker build -t novel:latest .       # or: make docker-build
#   docker run --rm novel:latest         # runs the baked demo app
#
# The store lives at /nvl (ENV NVLPATH=/nvl). It can be overridden at runtime by
# bind-mounting another store over /nvl; see `make docker-demo`.

ARG ALPINE_VERSION=3.23
ARG GO_VERSION=1.26.1
# Core-runtime version: the runtime files land in /nvl/runtime/<version>/ and
# the store's config.toml advertises the same version.
ARG NOVEL_VERSION=0.2.8

# --- build stage: compile the novel CLI (no external Go deps) ---------------
FROM golang:${GO_VERSION}-alpine AS builder
ARG NOVEL_VERSION
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -ldflags="-X main.version=${NOVEL_VERSION}" -o /out/novel ./cmd/novel

# --- runtime stage: Alpine + LuaJIT + the novel binary + an NVLPATH store ----
FROM alpine:${ALPINE_VERSION}
ARG NOVEL_VERSION
RUN apk add --no-cache luajit

COPY --from=builder /out/novel /usr/local/bin/novel

# Assemble the global store at /nvl:
#   runtime/<version>/   core runtime (novel.lua, repl.lua, sys/, std/)
#   modules/novel/       global .nv modules (bundled at compile time)
#   modules/lua/         global .lua modules (resolved at runtime)
#   config.toml          advertises the runtime version + luajit pin
ENV NVLPATH=/nvl
COPY runtime/novel.lua runtime/repl.lua /nvl/runtime/${NOVEL_VERSION}/
COPY runtime/sys /nvl/runtime/${NOVEL_VERSION}/sys
COPY runtime/std /nvl/runtime/${NOVEL_VERSION}/std
COPY docker/store/config.toml /nvl/config.toml
COPY docker/store/modules /nvl/modules

# Demo app importing both a global .nv module and a global .lua module.
COPY docker/app /app
WORKDIR /app

# Build-time smoke test: fail the build if the store or app does not resolve.
RUN novel env && novel run /app/main.nv

ENTRYPOINT ["novel"]
CMD ["run", "/app/main.nv"]
