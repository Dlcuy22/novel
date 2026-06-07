# Concurrency

Novel's concurrency model is deliberately small: `spawn`, channels, `send`, and
`recv`. It is built on LuaJIT coroutines, exposed as green threads scheduled
cooperatively by the runtime shim (`runtime/novel.lua`).

This guide explains the model and the patterns that work today.

## The model

A green thread is a function running on the cooperative scheduler. Threads run
until they block on a channel, on I/O, or on a timer (or finish), then yield so
another thread can run. There is no preemption and no OS threads, so there are
no data races on shared memory between yield points.

`main` is itself launched as a green thread, and the program runs until the
scheduler has drained every spawned thread and there is no pending I/O or timer.
You do not call anything to start or stop the scheduler; the toolchain wires
that up.

## Spawning

`spawn` runs a named function as a green thread:

```nv
fn worker(id int, jobs chan<int>) {
    // ...
}

spawn worker(1, jobs)
```

> Anonymous function spawns (`spawn fn() { ... }()`) are not supported yet. Give
> the work a named function.

## Channels

A channel passes values between threads. Create one with `chan<T>()` for an
unbuffered channel, or `chan<T>(n)` for a buffer of capacity `n`:

```nv
let signals = chan<int>()    // unbuffered
let jobs = chan<int>(16)     // buffered, holds up to 16 without blocking
```

Send and receive:

```nv
send(ch, 42)     // function form
ch <- 42         // sugar, identical to send(ch, 42)

let v = recv(ch) // blocks until a value arrives
```

On an unbuffered channel, a `send` blocks until another thread is ready to
`recv`, and vice versa; this is a rendezvous. On a buffered channel, `send`
only blocks when the buffer is full, and `recv` only blocks when it is empty.

## Sentinel pattern

Novel does not yet expose closing a channel from the language, so signal "no
more values" with a sentinel the receiver recognizes. A negative number is a
common choice for an integer pipeline:

```nv
fn produce(out chan<int>, count int) {
    for i = 1; i <= count; i++ {
        send(out, i)
    }
    send(out, -1)   // sentinel: done
}

fn main() {
    let ch = chan<int>(8)
    spawn produce(ch, 5)
    for {
        let v = recv(ch)
        if v < 0 {
            break
        }
        io.println($"got {v}")
    }
}
```

See `examples/concurrency.nv` for a two-stage pipeline using this pattern.

## Closing channels

A producer can `close` a channel to signal that no more values will come. A
two-value `recv` then reports whether the receive succeeded: `ok` is `false`
once the channel is closed and drained. Any values already buffered are
delivered before `ok` goes false.

```nv
fn producer(ch chan<int>) {
    for i = 1; i <= 3; i++ {
        send(ch, i)
    }
    close(ch)
}

fn main() {
    let ch = chan<int>(8)
    spawn producer(ch)
    for {
        let v, ok = recv(ch)
        if !ok {
            break
        }
        io.println($"got {v}")
    }
    io.println("channel closed")
}
```

This is cleaner than a sentinel when the value type has no spare "done" value.
Sending on a closed channel is a runtime error, so close from the producer side
only.

## Worker pool

Fan work out across several workers, then fan the results back in. Send one
sentinel per worker so each one exits:

```nv
fn worker(id int, jobs chan<int>, results chan<int>) {
    for {
        let job = recv(jobs)
        if job < 0 {
            ret
        }
        send(results, job * job)
    }
}

fn main() {
    let jobs = chan<int>(16)
    let results = chan<int>(16)

    const workers = 3
    for i = 1; i <= workers; i++ {
        spawn worker(i, jobs, results)
    }

    const jobCount = 6
    for j = 1; j <= jobCount; j++ {
        send(jobs, j)
    }
    for i = 1; i <= workers; i++ {
        send(jobs, -1)   // one sentinel per worker
    }

    let total = 0
    for i = 1; i <= jobCount; i++ {
        total += recv(results)
    }
    io.println($"total {total}")
}
```

The full program is `examples/worker_pool.nv`.

## Ordering and fairness

Results from multiple workers arrive in completion order, which is not
necessarily submission order. If you need to correlate a result with its job,
send a small struct carrying both the input and the output rather than a bare
value.

Because scheduling is cooperative, a thread that never blocks (a tight loop with
no `send`/`recv`) will not yield to others. Keep worker loops anchored on a
channel operation, as the examples above do.

## How it lowers

Each construct maps onto the runtime shim:

| Novel | Lowers to |
|-------|-----------|
| `spawn f(args)` | `novel.spawn(function() f(args) end)` |
| `chan<T>()` | `novel.chan()` |
| `chan<T>(n)` | `novel.chan(n)` |
| `send(ch, v)` and `ch <- v` | `novel.send(ch, v)` |
| `recv(ch)` | `novel.recv(ch)` |

Run `novel build yourfile.nv` and read the generated Lua to see exactly how a
program schedules.

## Asynchronous I/O

The scheduler is also a `poll(2)`-based reactor, so I/O composes with `spawn`
and channels instead of stalling the program. When a green thread does a
blocking operation from `std/net`, `std/http`, `std/io` (stdin), or
`time.sleep`, only that thread parks; the others keep running. Under the hood
the thread registers its file descriptor (or a timer deadline) with the
scheduler and yields. When the run queue empties, `novel.run()` blocks in
`poll()` until a descriptor is ready or the nearest timer fires, then re-runs
the woken threads.

This is what makes a concurrent server natural: accept a connection, `spawn` a
handler for it, and loop. Each handler blocks on its own socket without
affecting the accept loop or the other connections.

```nv
import std.net

fn handle(conn any) {
    for {
        let line, err = conn.readLine()
        if err != nil || line == nil {
            conn.close()
            ret
        }
        conn.write($"echo: {line}\n")
    }
}

fn serve(ln any) {
    for {
        let conn, err = ln.accept()   // parks until a client connects
        if err != nil { ret }
        spawn handle(conn)            // one green thread per connection
    }
}
```

See `examples/tcp_echo.nv` and `examples/http_server.nv`. Two caveats carry over
from the cooperative model: a tight loop that never blocks still won't yield,
and hostname resolution (`getaddrinfo`) currently blocks the whole VM for the
lookup. The reactor is fully cross-platform, using `poll(2)` on Linux/macOS and Winsock2 `WSAPoll` / Win32 `WaitForSingleObject` multiplexing on Windows.

## Not yet supported

- `select`-style waiting on multiple channels.
- Anonymous function spawns (`spawn fn() { ... }()`); use a named function.
