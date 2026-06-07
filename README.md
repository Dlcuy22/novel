# Novel

Novel is a statically typed language that transpiles to Lua 5.1 and runs on LuaJIT.
Novel borrows Go's type discipline and explicit error handling, adds keyword-rich
syntax, and provides lightweight concurrency via green threads on LuaJIT coroutines.

The language design lives in [`docs/language-spec.md`](./docs/language-spec.md).

## Status

The language pipeline runs end-to-end:
- **Compiler**: Lexer, parser, type checker, and emitter are operational. The type checker enforces semantic rules (undefined/unused variables, constant re-assignment, call arity, unknown types, and `!` propagation).
- **Runtime & Concurrency**: Cooperative scheduler and channels work under LuaJIT.
- **Cross-Platform Support**: Full native support for Windows (Win32/Winsock2 cooperative reactor) and POSIX (poll/sockets/exec backends).

## Prerequisites

- Go 1.26+ (transpiler and language server)
- LuaJIT 2.1+ (runtime; `novel run` and `make test-runtime` shell out to `luajit`)
- Node 20+ / npm (only to build the VS Code extension)

## Quick start

```sh
make build          # builds bin/novel and bin/novel-lsp
make test           # Go tests + Lua runtime smoke test

bin/novel build examples/hello.nv   # transpile to examples/hello.lua
bin/novel run   examples/hello.nv   # transpile + execute via luajit
bin/novel repl                      # interactive prompt
```

Run `make help` for all targets.

## Performance

Novel's transpiler is designed to be extremely fast. Since it is written in Go, parsing, type-checking, and code generation happen in sub-millisecond times:

- **Single-File Transpilation**: **~0.33 ms** (~3,000 files/sec) for Conway's Game of Life example (138 lines).
- **End-to-End Bundling (E2E)**: **~0.80 ms** (~1,250 files/sec) including full project dependency-graph loading, parsing, type-checking, and bundling.

To run the transpiler performance benchmark suite:
```sh
make bench-transpiler
```

## How it fits together

`internal/compiler.Compile` runs the full pipeline (lex -> parse -> type-check ->
emit) and is the single source of truth used by the CLI, the REPL, and the
language server, so diagnostics stay consistent everywhere. Emitted Lua does
`require("novel")` to pull in the runtime shim, which lowers `spawn`/`chan`/
`send`/`recv` onto LuaJIT coroutines (see `docs/language-spec.md` §13.2).

## Quick syntax reference

- **No package declarations**: a file is runnable when it declares `fn main()`.
- **Dotted imports**: `import std.io`, `import geometry` (no quotes, no `./` prefix).
- **`ret` keyword**: `ret a + b` (not `return`).
- **Error handling**: `(T, error)` returns; `!` suffix propagates errors.
- **Concurrency**: `spawn`, `chan<T>()`, `send(ch, v)`, `recv(ch)`.

We don't try to replace Lua, we just want to elevate the developer experience. By bringing static typing and explicit error handling to prevent runtime errors, Novel turns Lua from a simple scripting tool into a robust language for building full-scale applications.

## Repository layout

```
cmd/        the `novel` CLI and LSP server binaries
internal/   core compiler pipeline (lexer, parser, type checker, emitter)
runtime/    core runtime shim and cooperative FFI standard library
examples/   runnable sample .nv programs
docs/       language specification and documentation
```
