# Novel toolchain build automation.
#
# The repo is polyglot: a Go transpiler + language server (cmd/, internal/),
# a Lua runtime shim (runtime/), and a TypeScript VS Code extension
# (editors/vscode/). These targets wrap each toolchain's native commands.

BIN      := bin
# Single-token ldflags (no spaces, no quotes) so the recipe survives tooling
# that reformats the Makefile.
GOFLAGS  := -ldflags=-X=main.version=0.2.8
# C toolchain for the optional native image library (std/image over stb).
CC       ?= cc
# Shared-library/executable extensions by host OS.
ifeq ($(OS),Windows_NT)
EXE      := .exe
IMGEXT   := dll
else
EXE      :=
UNAME_S  := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
IMGEXT   := dylib
else
IMGEXT   := so
endif
endif
IMGLIB   := runtime/libnovel_image.$(IMGEXT)
WLLIB    := runtime/libnovel_wayland.$(IMGEXT)
# Docker image tag and the host directory the demo store is extracted into.
IMAGE    := novel:latest
NVLDEMO  := examples/NVLPATH_examples
.PHONY: all build novel lsp image-lib wl-lib test test-go test-runtime test-std test-net test-image bench lint fmt clean image-clean wl-clean ext-build ext-install help docker-build docker-run docker-demo docker-clean

all: build

## build: compile the novel CLI and novel-lsp into ./bin
build: novel lsp

novel:
	go build $(GOFLAGS) -o $(BIN)/novel$(EXE) ./cmd/novel

lsp:
	go build $(GOFLAGS) -o $(BIN)/novel-lsp$(EXE) ./cmd/novel-lsp

## image-lib: build the optional native image library (std/image, via stb).
## Requires a C compiler. std/image degrades to a clear error without it, so
## this is not part of the default `build`.
image-lib: $(IMGLIB)

$(IMGLIB): runtime/csrc/novel_image.c runtime/csrc/vendor/stb_image.h runtime/csrc/vendor/stb_image_write.h
	$(CC) -O2 -fPIC -shared -o $(IMGLIB) runtime/csrc/novel_image.c

XDG_SHELL_XML := $(shell find /usr/share -name xdg-shell.xml 2>/dev/null | head -n 1)

## wl-lib: build the native Wayland helper library (std/wl_ui)
wl-lib: $(WLLIB)

runtime/csrc/xdg-shell.h:
	wayland-scanner client-header $(XDG_SHELL_XML) runtime/csrc/xdg-shell.h

runtime/csrc/xdg-shell.c:
	wayland-scanner private-code $(XDG_SHELL_XML) runtime/csrc/xdg-shell.c

$(WLLIB): runtime/csrc/novel_wayland.c runtime/csrc/xdg-shell.c runtime/csrc/xdg-shell.h runtime/csrc/font8x8_basic.h
	$(CC) -O2 -fPIC -shared -o $(WLLIB) runtime/csrc/novel_wayland.c runtime/csrc/xdg-shell.c -lwayland-client

## test: run all Go tests and the Lua runtime smoke test
test: test-go test-runtime test-std test-net

test-go:
	go test ./...

## test-runtime: exercise runtime/novel.lua under LuaJIT
test-runtime:
	luajit runtime/test_novel.lua

## test-std: exercise the std/* Lua modules under LuaJIT
test-std:
	luajit runtime/test_std.lua

## test-net: exercise std/net and std/http over the async scheduler
test-net:
	luajit runtime/test_net.lua

## test-image: build the native image lib, then exercise std/image under LuaJIT
test-image: image-lib
	luajit runtime/test_image.lua

## bench: build then run the benchmark suite (BENCH_REPS reps each, default 3)
bench:
	bash benchmark/run.sh $(BENCH_REPS)

## fmt: gofmt the Go sources
fmt:
	gofmt -w .

## lint: vet the Go sources
lint:
	go vet ./...

## ext-build: compile the VS Code extension (requires npm install first)
ext-build:
	cd editors/vscode && npm run compile

## ext-install: install extension deps
ext-install:
	cd editors/vscode && npm install

## docker-build: build the Novel image (compiles the CLI, bakes an NVLPATH store)
docker-build:
	docker build -t $(IMAGE) .

## docker-run: run the demo app baked into the image
docker-run: docker-build
	docker run --rm $(IMAGE)

## docker-demo: extract the baked NVLPATH store to $(NVLDEMO)/nvl, then run the
## demo app with that host store bind-mounted as NVLPATH (proves the store is
## relocatable and overridable at runtime)
docker-demo: docker-build
	rm -rf $(NVLDEMO)/nvl
	mkdir -p $(NVLDEMO)
	cid=$$(docker create $(IMAGE)); \
	docker cp $$cid:/nvl $(NVLDEMO)/nvl; \
	docker rm $$cid >/dev/null
	@echo "--- store extracted to $(NVLDEMO)/nvl; running app against it ---"
	docker run --rm \
		-e NVLPATH=/nvl \
		-v "$(CURDIR)/$(NVLDEMO)/nvl:/nvl:ro" \
		-v "$(CURDIR)/docker/app:/work:ro" \
		-w /work \
		$(IMAGE) run /work/main.nv

## docker-clean: remove the image and the extracted demo store
docker-clean:
	-docker rmi $(IMAGE)
	rm -rf $(NVLDEMO)/nvl

## image-clean: remove the built native image library
image-clean:
	rm -f $(IMGLIB)

## wl-clean: remove the built native Wayland library and generated shell files
wl-clean:
	rm -f $(WLLIB) runtime/csrc/xdg-shell.c runtime/csrc/xdg-shell.h

clean: image-clean wl-clean
	rm -rf $(BIN)

help:
	@grep -E '^##' Makefile | sed 's/## //'
