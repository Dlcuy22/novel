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
- [Standard Library](stdlib.md): the std/* modules (math, str, rand, crypto),
  using Lua libraries from Novel, and the planned net module.
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

Run one with:

```sh
bin/novel run examples/basics.nv
```

## Reference

[`language-spec.md`](./language-spec.md) is the authoritative language design,
including planned features. The Language Guide marks which spec features are not
built yet.

## Status

Novel is an early bootstrap. The lexer, parser, emitter, and runtime run real
programs; the type checker is still a no-op walk, so type errors are not yet
reported at compile time. See the "Known limitations" section of the Language
Guide for the current gaps.
