# Novel in Docker

A self-contained Novel toolchain image: the `novel` CLI on Alpine, plus a
global NVLPATH store (core runtime + global modules) and a demo app.

## Layout

```
Dockerfile              multi-stage build (Go builder -> Alpine runtime)
docker/
  app/main.nv           demo app: imports a global .nv module and a Lua module
  store/
    config.toml         store defaults (runtime version + luajit pin)
    modules/novel/      global .nv modules (bundled at compile time)
    modules/lua/        global .lua modules (resolved at runtime)
```

In the image the store is baked at `/nvl` with `NVLPATH=/nvl`. Its runtime lives
at `/nvl/runtime/<version>/`; `config.toml` advertises that version so the store
is self-describing, independent of the toolchain binary.

## Build and run

```sh
make docker-build      # build the image (runs a build-time smoke test)
make docker-run        # run the baked demo app
```

Equivalent raw Docker:

```sh
docker build -t novel:latest .
docker run --rm novel:latest                 # default: novel run /app/main.nv
docker run --rm --entrypoint novel novel:latest env   # inspect the store
```

## Demonstrate a relocatable store (bind mount)

`make docker-demo` extracts the baked store to `examples/NVLPATH_examples/nvl`
on the host, then runs the demo app with that host store bind-mounted as
`NVLPATH`. This shows the store is relocatable and overridable at runtime:

```sh
make docker-demo
```

`examples/NVLPATH_examples/` is gitignored (only `.gitkeep` is tracked), so the
extracted store never lands in version control.

## Run your own app

Mount your store and sources, then point `NVLPATH` at the store:

```sh
docker run --rm \
  -e NVLPATH=/nvl \
  -v "$PWD/my-store:/nvl:ro" \
  -v "$PWD/my-app:/work:ro" \
  -w /work \
  novel:latest run /work/main.nv
```
