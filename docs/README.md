# Novel Documentation

Novel is a statically typed language that transpiles to Lua 5.1 and runs on
LuaJIT. Start here.

## Guides

- [Getting Started](getting-started.md): install prerequisites, build the
  toolchain, and run your first program.
- [Language Guide](language-guide.md): syntax and semantics, from variables and
  types through functions, structs, control flow, collections, and errors.
- [Concurrency](concurrency.md): green threads, channels, and the patterns that
  work today.
- [Standard Library](stdlib.md): standard modules (math, str, rand, crypto, fs, exec, net, http, wl_ui), using Lua libraries from Novel, and platform-specific FFI backends.
- [Architecture](architecture.md): how the transpiler is wired, for people
  working on the toolchain itself.

## Examples

Runnable programs live in `examples/`. Each starts with the command to run it.

| File | Shows |
|------|-------|
| `basics.nv` | variables, functions, multi-return, interpolation |
| `control_flow.nv` | if/elif/else and all four for-loop forms |
| `collections.nv` | slices and maps |
| `structs.nv` | struct types, methods, composition |
| `errors.nv` | the `(T, error)` return pattern |
| `concurrency.nv` | a two-stage channel pipeline |
| `channels.nv` | closing a channel and two-value `recv` |
| `worker_pool.nv` | fan-out work, fan-in results |
| `stdlib.nv` | the std/* modules (math, str, rand, crypto) |
| `lua_interop.nv` | importing a Lua library (`bit`) directly |
| `fizzbuzz.nv` | the classic |
| `game_of_life.nv` | a terminal simulation tying features together |
| `hello.nv` | spawning tasks over a channel |
| `fs_demo.nv` | filesystem operations (stat, mkdir, readDir, etc.) |
| `exec_demo.nv` | spawning subprocesses and capturing output |
| `static_server.nv` | concurrent static file HTTP server |
| `http_server.nv` | basic HTTP handler server |
| `tcp_echo.nv` | concurrent TCP echo server over the reactor |
| `wayland_demo.nv` | experimental Wayland graphical window and font rendering |
| `image_invert.nv` | image decoding and pixel inversion |
| `json_roundtrip.nv` | JSON serialization and parsing |


Run one with:

```sh
bin/novel run examples/basics.nv
```

## Reference

[`language-spec.md`](./language-spec.md) is the authoritative language design,
including planned features. The Language Guide marks which spec features are not
built yet.

## Status

Novel runs real programs end-to-end: the lexer, parser, type checker, emitter, and runtime are operational. The type checker reports semantic errors like undefined identifiers, unused variables, constant re-assignment, call arity, unknown types, and `!` propagation. See the "Known limitations" section of the Language Guide for the remaining gaps.
