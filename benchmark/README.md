# Benchmarks

A suite of Novel programs that measure execution speed of the transpiled Lua.
The point is relative comparison: run the suite now to get a baseline, optimize
the codegen later, run it again, and use `compare.sh` to see what moved. The
absolute numbers depend on your machine and LuaJIT build; only the deltas
matter.

## How a benchmark is structured

Each `*.nv` file times its own hot region and prints one tab-separated line:

```
<name>\t<milliseconds>\t<checksum>
```

Three conventions make the numbers meaningful:

- **Warmup pass.** Every benchmark runs its workload once, untimed, before the
  timed pass. LuaJIT compiles a hot loop only after it has run enough times
  (its trace hotcount), so the warmup ensures we time compiled code, not the
  interpreter warming up.
- **Self-timing.** Timing brackets only the compute, using `std/time.now()`.
  The transpile step and LuaJIT startup happen outside the measured region, so
  they do not pollute the result.
- **Checksum.** Each benchmark prints a deterministic checksum of its output. If
  a codegen change speeds things up but changes the checksum, the optimization
  altered behavior and the timing is not a valid comparison. `compare.sh` flags
  this automatically.

## The benchmarks

| File | Exercises |
|------|-----------|
| `loop_sum.nv` | tight integer-accumulation loop |
| `fib.nv` | recursion and function-call overhead (`fib(35)`) |
| `sieve.nv` | slice allocation and indexed read/write |
| `mandelbrot.nv` | float math in a nested escape loop |
| `nbody.nv` | struct field mutation through by-reference tables, float math |
| `matrix_mul.nv` | nested-slice indexing, triple-nested loop |
| `vector.nv` | struct construction and method (colon-call) dispatch |
| `map_ops.nv` | map insert and lookup (table hashing) |
| `channel_throughput.nv` | scheduler and channel send/recv steady state |
| `spawn_storm.nv` | green-thread creation and scheduling |

`loop_sum`, `fib`, `mandelbrot`, and `nbody` are the long runners that keep the
JIT busy; the others finish faster but still warm up first.

## Running

From the repository root:

```sh
make bench            # build, run each benchmark 3 times, keep the fastest
```

Or call the harness directly with a custom repetition count:

```sh
benchmark/run.sh 5    # 5 reps each
```

The harness builds `bin/novel`, runs every benchmark, prints a table, and writes
a timestamped result file under `benchmark/results/`.

## Comparing two runs

```sh
benchmark/compare.sh benchmark/results/OLD.tsv benchmark/results/NEW.tsv
```

It prints old vs new milliseconds and the percent change per benchmark
(negative means faster). Any benchmark whose checksum changed between the two
runs is flagged, because that invalidates the timing comparison.

A typical optimization workflow:

```sh
make bench                                   # baseline, note the filename
# ... change the emitter, rebuild ...
make bench                                   # new run
benchmark/compare.sh benchmark/results/<old>.tsv benchmark/results/<new>.tsv
```

## Notes and caveats

- **`os.clock` measures CPU time, not wall-clock.** On Novel's cooperative
  single-threaded runtime the two track closely, which is fine for relative
  comparison. If you later need true wall-clock timing (to include sleeps or
  blocking I/O), swap `runtime/std/time.lua` to an ffi `gettimeofday` call.
- **Keep the machine quiet.** Background load adds noise. The harness keeps the
  minimum across repetitions to reduce it, but a dedicated, idle machine gives
  the cleanest deltas.
- **Checksums are bounded on purpose.** Accumulators use modular arithmetic or
  capped ranges so they stay exact (well under 2^53) and do not overflow into
  floats, which keeps the checksum reproducible across runs and machines.
- **`results/` is kept but its contents are not committed** except `.gitkeep`.
  Result files are local measurements, not source.
