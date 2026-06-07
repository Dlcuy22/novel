# Standard Library

Novel's standard library ships as Lua modules under `runtime/std/`. A Novel
`import std.x` lowers directly to Lua's `require("std/x")`, and `novel run`
puts `runtime/` on the Lua path, so the modules resolve with no extra setup.
Call their functions with dot syntax: `math.sqrt(x)`, `str.trim(s)`.

Some modules (`net`, `http`, parts of `io` and `time`) do **asynchronous I/O**:
when an operation would block, only the calling green thread parks while the
others keep running. This works because the runtime scheduler (`novel.run()`) is
also a `poll(2)`-based reactor; see [`concurrency.md`](./concurrency.md). A few
modules (`ffi`, `image`) use LuaJIT's FFI to call C; those note their platform
support and any build step.

## How modules work

There is no magic. A module is a Lua file that returns a table of functions.
`import std.math` becomes `local math = require("std/math")`, and
`math.sqrt(x)` becomes `math.sqrt(x)` in the emitted Lua. This is the same
mechanism the runtime shim (`require("novel")`) uses.

Because of that, adding a stdlib module is just writing a Lua file under
`runtime/std/` that returns a table; no changes to the Go transpiler are
needed.

## std/math

Numeric helpers over LuaJIT's math library.

| Function | Description |
|----------|-------------|
| `math.pi`, `math.e` | constants |
| `math.sqrt(x)` | square root |
| `math.pow(x, y)` | x raised to y |
| `math.abs(x)` | absolute value |
| `math.floor(x)`, `math.ceil(x)` | rounding toward -inf / +inf |
| `math.round(x)` | nearest integer, halves round up |
| `math.sin/cos/tan(x)`, `math.atan2(y, x)` | trigonometry |
| `math.exp(x)`, `math.log(x)` | exponential and natural log |
| `math.min(a, b)`, `math.max(a, b)` | smaller / larger of two |
| `math.clamp(v, lo, hi)` | constrain v to [lo, hi] |

## std/str

String operations beyond interpolation. Returned indices are 1-based, matching
Novel's slice convention in this build. All matching is literal, so characters
like `.` are not treated as patterns.

| Function | Description |
|----------|-------------|
| `str.len(s)` | length |
| `str.upper(s)`, `str.lower(s)` | case conversion |
| `str.repeat_(s, n)` | repeat s n times |
| `str.trim(s)`, `str.trimLeft(s)`, `str.trimRight(s)` | strip whitespace |
| `str.contains(s, sub)` | true if sub appears in s |
| `str.indexOf(s, sub)` | 1-based index of sub, or 0 if absent |
| `str.startsWith(s, prefix)`, `str.endsWith(s, suffix)` | prefix/suffix test |
| `str.split(s, sep)` | split on sep into a slice |
| `str.join(parts, sep)` | join a slice with sep |
| `str.replace(s, old, new)` | replace every occurrence |
| `str.substring(s, from, to)` | 1-based inclusive slice |

`repeat_` has a trailing underscore because `repeat` is a Lua keyword.

## std/rand

Random numbers. The PRNG helpers are for games, sampling, and jitter, not for
security. Use `bytes` when you need unpredictable, security-grade randomness.

| Function | Description |
|----------|-------------|
| `rand.seed(n)` | seed the PRNG for a reproducible sequence |
| `rand.float()` | float in [0, 1) |
| `rand.intn(n)` | integer in [0, n-1] |
| `rand.between(lo, hi)` | integer in [lo, hi] inclusive |
| `rand.bytes(n)` | n secure bytes from the OS entropy source |

`bytes` reads the OS cryptographically secure entropy source (`/dev/urandom` on POSIX or `CryptGenRandom` on Windows) and is suitable for tokens, salts, and keys. It returns `nil` if the entropy source cannot be opened or initialized.

## std/crypto

Hashing. SHA-256 is a correct pure-Lua implementation (verified against the
NIST test vectors in `runtime/test_std.lua`), suitable for integrity and
fingerprinting; it is not tuned for high-volume hashing. FNV-1a is a fast
non-cryptographic hash for hash tables and checksums only.

| Function | Description |
|----------|-------------|
| `crypto.sha256(s)` | hex-encoded SHA-256 digest of a string |
| `crypto.fnv1a(s)` | 32-bit FNV-1a hash as an integer |

```nv
import std.crypto

let digest = crypto.sha256("abc")
// ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
```

## Using Lua libraries

Since `import` lowers to `require`, any Lua module on the Lua path works the
same way as a std module. LuaJIT's built-in `bit` library needs no
installation:

```nv
import bit

fn main() {
    let masked = bit.band(0xFF, 0x3C)   // 60
    let shifted = bit.lshift(1, 4)      // 16
}
```

See `examples/lua_interop.nv`. To use a third-party Lua library, install it
where LuaJIT can find it (the `LUA_PATH` / `LUA_CPATH` the OS package manager or
LuaRocks sets up), then `import modulename`. Set `NOVEL_RUNTIME` or extend
`LUA_PATH` if the module lives somewhere `novel run` does not already search.

A few practical notes:

- Functions are called with dot syntax (`mod.fn(args)`), because the emitter
  treats imported names as modules, not method receivers.
- Names with dots (`import socket.core`) bind to the last segment (`core`) as
  the local. Alias to be explicit: `import socket.core as s` then `s.fn(...)`.
- A Lua library that returns a non-table value, or that must be called rather
  than indexed, will not fit the `mod.fn` shape; wrap it in a small std module.
- A raw FFI namespace can't be dot-called either (the emitter would lower
  `lib.fn(x)` to a method call `lib:fn(x)`). Use `std/ffi`'s `ffi.fn(lib, sym)`
  to get a plain callable.

## std/os

Platform detection, environment, process arguments, and exit. `platform()` and
`arch()` are the canonical way to branch on the host OS.

| Function | Description |
|----------|-------------|
| `os.platform()` | `"linux"`, `"macos"`, `"windows"`, `"bsd"`, or `"other"` |
| `os.isUnix()`, `os.isWindows()` | platform predicates |
| `os.arch()` | CPU architecture (`"x64"`, `"arm64"`, ...) |
| `os.getenv(name)` | environment variable value, or `nil` |
| `os.args()` | slice of command-line arguments after the `.nv` file |
| `os.exit(code)` | terminate the process with a status code |

Arguments are forwarded by the CLI: `novel run prog.nv a b c` makes
`os.args()` return `["a", "b", "c"]`.

## std/io

Console and file I/O. The read helpers are **asynchronous**: a read that waits
for stdin parks only the calling green thread (other threads keep running).

| Function | Description |
|----------|-------------|
| `io.println(...)`, `io.print(...)` | write to stdout (with / without newline) |
| `io.eprintln(...)`, `io.eprint(...)` | write to stderr |
| `io.readLine()` | one line from stdin (newline stripped), `nil` at EOF |
| `io.readAll()` | all of stdin until EOF, as one string |
| `io.readFile(path)` | `(contents, err)` |
| `io.writeFile(path, data)` | `(ok, err)` |

```nv
import std.io

fn main() {
    io.print("name? ")
    let name = io.readLine()
    io.println($"hello {name}")
}
```

## std/time

Timing. `sleep` is cooperative: it parks the calling thread and lets others run
while the reactor waits for the deadline.

| Function | Description |
|----------|-------------|
| `time.now()` | CPU seconds (float, `os.clock`) — for relative timing |
| `time.unix()` | wall-clock seconds since the Unix epoch (sub-second) |
| `time.monotonic()` | seconds from a clock that never steps backward |
| `time.sleep(seconds)` | cooperatively pause this green thread |

## std/ffi

Direct C interop over LuaJIT's FFI. Use `ffi.fn(lib, symbol)` to get a plain
callable for a C function — a raw FFI namespace can't be dot-called from Novel
(see the call-lowering note under "Using Lua libraries").

| Function | Description |
|----------|-------------|
| `ffi.cdef(decls)` | declare C types/functions |
| `ffi.load(name)` | load a shared library, returns its namespace |
| `ffi.C` | the default C namespace (libc + cdef'd symbols) |
| `ffi.fn(lib, symbol)` | a plain callable for a C function |
| `ffi.new/cast/sizeof/typeof` | the usual FFI value helpers |
| `ffi.string(ptr, len?)` | C pointer → Lua string |
| `ffi.os()`, `ffi.arch()` | build target, for branching cdefs |

```nv
import std.ffi as ffi

fn main() {
    ffi.cdef("double cos(double x);")
    let cos = ffi.fn(ffi.C, "cos")
    let y = cos(0.0)   // 1.0
}
```

## std/fs

Filesystem operations, platform-independent with POSIX and Windows FFI backends.

| Function | Description |
|----------|-------------|
| `fs.readDir(path)` | `(filenames, err)` — returns a slice of file/folder names inside the directory |
| `fs.stat(path)` | `(info, err)` — returns file/folder information |
| `fs.exists(path)` | `bool` — check if a path exists |
| `fs.mkdir(path)` | `(ok, err)` — create a single directory |
| `fs.mkdirAll(path)` | `(ok, err)` — recursively create directories |
| `fs.remove(path)` | `(ok, err)` — delete a file or empty directory |
| `fs.removeAll(path)` | `(ok, err)` — recursively delete directory/file and its contents |
| `fs.rename(old, new)` | `(ok, err)` — rename or move a path |

The returned `FileInfo` structure for `fs.stat` has the following fields:

```nv
pub type FileInfo struct {
    pub name     string // name of the file
    pub size     int64  // file size in bytes
    pub is_dir   bool   // true if path is a directory
    pub mod_time int64  // modification time as a Unix timestamp
}
```

```nv
import std.fs
import std.io

fn main() {
    let ok, err = fs.mkdir("test")
    if !ok {
        io.println($"failed to create dir: {err.msg}")
        ret
    }
}
```

## std/json

JSON encode/decode, pure Lua. Decoded values are dynamic, so bind them as `any`.

| Function | Description |
|----------|-------------|
| `json.encode(value)` | serialize a value to a JSON string |
| `json.decode(text)` | `(value, err)` |
| `json.null` | sentinel for JSON `null` (distinct from `nil`) |

Mapping: object ↔ table with string keys, array ↔ 1..n sequence, plus
string/number/bool. An empty table encodes as `[]`.

## std/encoding

Binary-to-text codecs, pure Lua.

| Function | Description |
|----------|-------------|
| `encoding.base64Encode(data)` | standard base64 with `=` padding |
| `encoding.base64Decode(text)` | `(data, err)` |
| `encoding.hexEncode(data)` | lowercase hex |
| `encoding.hexDecode(text)` | `(data, err)` |

## std/net

Asynchronous TCP and UDP over LuaJIT FFI sockets, integrated with the scheduler:
non-blocking sockets whose operations park the calling green thread on fd
readiness and resume via the reactor, so connections are handled concurrently.

| Function | Description |
|----------|-------------|
| `net.listenTCP(host, port)` | `(listener, err)` — bound, listening socket |
| `net.dialTCP(host, port)` | `(conn, err)` — established client connection |
| `net.bindUDP(host, port)` | `(udp, err)` — bound datagram socket |
| `listener.accept()` | `(conn, err)` — next incoming connection (parks) |
| `conn.read(n)` | `(data, err)` — `""` means the peer closed |
| `conn.readLine()` | `(line, err)` — newline stripped, `nil` at EOF |
| `conn.write(data)` | `(ok, err)` — sends all of `data` |
| `conn.close()` | close the connection |
| `udp.recvfrom()` | `(data, ip, port)` |
| `udp.sendto(data, ip, port)` | `(ok, err)` |

Listener/conn/udp are objects: call their methods with dot syntax
(`conn.read(n)`), which the emitter lowers to a method call. They avoid the
method names `len` and `append`, which the emitter rewrites to `#x` /
`table.insert` on any receiver.

**Platform:** Linux, macOS, and Windows (Winsock2). Standard sockets and HTTP features are fully cross-platform.

**Security:** plaintext only, no TLS, no auth. Bind to `127.0.0.1` unless a
service is meant to be reachable off-host, and validate untrusted input.
Hostname resolution (`getaddrinfo`) currently blocks the VM for the lookup.

## std/http

A minimal HTTP/1.1 server and client over `std/net`, pure Lua. The server
spawns a green thread per connection (real concurrency on the async scheduler).

| Function | Description |
|----------|-------------|
| `http.serve(host, port, handler)` | `(listener, err)`; spawns the accept loop |
| `http.response(status, body, headers?)` | build a response table |
| `http.serveFile(path)` | serve a static file from disk with guessed MIME type |
| `http.parseRequest(rawHead, body?)` | parse a raw HTTP request head and optional body |
| `http.writeResponse(conn, response)` | serialize and write a response table to a connection |
| `http.get(url)` | `(response, err)`, one-shot GET (http:// only) |

The handler is `fn(req any) any` returning a response table (or a string, sent
as a 200). `req` has `method`, `path`, `version`, `headers` (lowercased keys),
`body`, and `conn` (the raw connection object).

### Connection Hijacking

A handler can take direct control of the underlying connection by setting `req.hijacked = true`. When set, the HTTP server skips writing its own response and closing the connection. This is useful for protocols like WebSockets or Server-Sent Events (SSE).

```nv
import std.http
import std.time

fn handle(req any) any {
    if req.path == "/hijack" {
        req.hijacked = true
        // Write manually to the underlying connection
        req.conn.write("HTTP/1.1 200 OK\r\nConnection: close\r\n\r\ncustom hijack response")
        req.conn.close()
        ret nil
    }
    ret http.response(200, $"you hit {req.path}")
}

fn main() {
    http.serve("127.0.0.1", 8080, handle)
    for { time.sleep(3600.0) }
}
```

**Security:** same as `std/net`, plaintext, no auth, no TLS. Don't expose a
handler to untrusted networks without your own validation and limits.

## std/exec

Spawns external subprocesses and captures stdout/stderr cooperatively using the async scheduler.

| Function | Description |
|----------|-------------|
| `exec.command(path, args)` | returns a `Cmd` execution controller |
| `cmd.run()` | `(code, err)`, runs command, blocks asynchronously, and returns exit code |
| `cmd.output()` | `(stdout, err)`, runs command and returns stdout |
| `cmd.combinedOutput()` | `(combined, err)`, runs command and returns combined stdout and stderr |

If a command exits with a non-zero status code, the returned error object represents the exit status error. If the binary is not found in the PATH or cannot be accessed, the resolution error is returned.

```nv
import std.exec
import std.io

fn main() {
    let cmd = exec.command("echo", ["hello"])
    let out, err = cmd.output()
    if err != nil {
        io.println($"failed: {err.msg}")
        ret
    }
    io.println(out)
}
```

## std/image

Load, edit, and save PNG/JPEG images via `stb_image` / `stb_image_write` through
a small C shim. Pixels are 8-bit RGBA, row-major, origin top-left, 0-based.

| Function | Description |
|----------|-------------|
| `image.load(path)` | `(img, err)`; decode PNG/JPEG/BMP/TGA/GIF/... to RGBA |
| `image.new(w, h)` | `(img, err)`; a blank transparent image |
| `img.width`, `img.height` | dimensions (fields) |
| `img.get(x, y)` | `r, g, b, a` (each 0–255) |
| `img.set(x, y, r, g, b, a)` | write a pixel (`a` defaults to 255) |
| `img.savePNG(path)` | `(ok, err)` |
| `img.saveJPG(path, quality)` | `(ok, err)` — quality 1–100 |

**Build step:** `std/image` needs the native library, built once with
`make image-lib` (requires a C compiler). Without it, `load`/`new` return a
clear error instead of crashing, so programs that never touch images still run.

## std/wl_ui

Wayland client window spawning and text drawing. Exposes a native shared-memory window interface mapped through `xdg-shell`.

| Function | Description |
|----------|-------------|
| `wl_ui.createWindow(title, w, h)` | `(win, err)`; spawn a new Wayland window |
| `wl_ui.clear(win, color)` | clear the screen with an XRGB8888 color |
| `wl_ui.drawText(win, x, y, text, color)` | render text in the buffer using an 8x8 font |
| `wl_ui.present(win)` | flush client buffer updates to the compositor |
| `wl_ui.dispatch(win)` | `status`; poll the Wayland socket non-blockingly (returns 0 if active, -1 if closed) |
| `wl_ui.close(win)` | close display connection and free window resources |

**Build step:** `std/wl_ui` requires compiling the helper library with `make wl-lib` (requires `wayland-client` development packages and `wayland-scanner` on the host system).

## std/test

A tiny unit-test runner so Novel programs can test themselves. `done()` exits
non-zero on any failure, so CI can gate on `novel run tests.nv`.

| Function | Description |
|----------|-------------|
| `test.run(name, fn)` | run `fn` as a named case |
| `test.assert(cond, msg?)` | fail the case if `cond` is falsy |
| `test.eq(a, b, msg?)`, `test.neq(a, b, msg?)` | equality assertions |
| `test.summary()` | print the tally, return the failure count |
| `test.done()` | print the summary and exit (0 all-pass, else 1) |

## Testing

The modules have Lua test suites with assertions, including the SHA-256 NIST
vectors, RFC 4648 base64 vectors, JSON roundtrips, and end-to-end net/http
loopback over the async scheduler:

```sh
make test-std        # std/* pure-Lua modules (math, str, rand, crypto, os, ffi, json, encoding)
make test-net        # std/net + std/http over the reactor (TCP, UDP, concurrency, HTTP)
make test-image      # std/image (builds the native library first; needs a C compiler)
make wl-lib          # std/wl_ui (builds the native Wayland helper library; needs wayland-client)
make test            # Go tests + runtime + test-std + test-net
```

`make test-image` is separate because it needs a C compiler to build
`libnovel_image`.

