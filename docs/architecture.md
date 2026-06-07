# Toolchain Architecture

How a `.nv` file becomes a running program, and where each piece lives.

## Pipeline

```
.nv source
   │
   ▼
internal/lexer      source text -> []token.Token
   │
   ▼
internal/parser     tokens -> *ast.File
   │
   ▼
internal/types      semantic checks (no AST mutation yet)
   │
   ▼
internal/emit       *ast.File -> Lua 5.1 source string
   │
   ▼
LuaJIT              luajit - (direct) or luajit -b (bytecode)
```

`internal/compiler.Compile(src) Result` glues these stages together and returns
the emitted Lua plus a flat list of `Diagnostic`s. Every front end (CLI, REPL,
LSP) calls this one function so behavior never diverges between them.

## Why a shared driver

Three consumers need identical results:

- `cmd/novel`: `build` writes `.lua`, `run` pipes Lua to `luajit` over stdin.
- the REPL: a `compiler.ReplSession` lowers each line (import, declaration,
  statement, or bare expression) to a Lua chunk and streams it to a persistent
  `luajit` process running `runtime/repl.lua`, so session state persists.
- `cmd/novel-lsp`: compiles on document change and publishes diagnostics.

Keeping the pipeline in `internal/compiler` means a parser or type-checker fix
improves all three at once.

## Runtime shim (`runtime/novel.lua`)

Emitted Lua is untyped and depends on the shim for concurrency:

| Novel              | lowers to                       |
|--------------------|---------------------------------|
| `spawn f()`        | `novel.spawn(function() ... end)` |
| `chan<T>()`        | `novel.chan()` (unbuffered)     |
| `chan<T>(n)`       | `novel.chan(n)` (buffered)      |
| `send(ch, v)`      | `novel.send(ch, v)`             |
| `recv(ch)`         | `novel.recv(ch)` -> value, ok   |
| `close(ch)`        | `novel.close(ch)`               |
| `error("msg")`     | `novel.error("msg")` -> `{msg=}` |

The scheduler is cooperative: green threads run until they block on a channel or
finish. `novel.run()` drains the run queue and must be emitted at the end of
`main`. Channel blocking is implemented with `coroutine.yield`/`resume`; a
blocked sender/receiver is re-enqueued by the operation that unblocks it.

See `language-spec.md` §13 for the full type-mapping table and §13.2 for the
concurrency model.

## Platform-Specific Module Dispatching Pattern

For standard library modules that call platform-specific C APIs via LuaJIT FFI (such as filesystem or networking), we use a dynamic file-based dispatching pattern.

Instead of combining all platforms into a single file with complex OS checks, we split the platform-specific logic into clean backend files (`*_posix.lua` and `*_windows.lua`) and use a lightweight dispatcher file that queries the host OS via `std/os` or `ffi.os`.

This pattern is used for standard library and runtime modules, including:
* `std/fs` (split into `sys/fs_posix.lua`, `sys/fs_windows.lua`, and `std/fs.lua`)
* `sys/poll` (split into `sys/poll_posix.lua`, `sys/poll_windows.lua`, and `sys/poll.lua`)
* `sys/posix` (split into `sys/posix_posix.lua`, `sys/posix_windows.lua`, and `sys/posix.lua`)
* `sys/socket` (split into `sys/socket_posix.lua`, `sys/socket_windows.lua`, and `sys/socket.lua`)

Because LuaJIT evaluates imports and C declarations dynamically at runtime, this prevents compilation errors and keeps the implementation files modular and maintainable.

## Adding a language feature

Most features touch the pipeline in order:

1. `internal/token`: new tokens, if any.
2. `internal/lexer`: lex them (add a case + a `lexer_test.go` assertion).
3. `internal/ast`: node types for the construct.
4. `internal/parser`: parse into those nodes (add a `parser_test.go` case).
5. `internal/types`: semantic rules (see spec §13.3: type mismatches, unused
   vars, arity, `!` propagation).
6. `internal/emit`: lower to Lua; if it needs runtime support, extend
   `runtime/novel.lua` and add a check to `runtime/test_novel.lua`.

Run `make test` after each stage.
