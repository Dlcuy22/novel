-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/time (timing utilities for the Novel runtime)
--
-- Purpose:
--   Timing for benchmarks and programs: a CPU-time clock, a wall-clock unix
--   timestamp, a monotonic clock, and a cooperative sleep that parks just the
--   calling green thread (via the scheduler's reactor) instead of stalling the
--   VM. Imported with `import std.time`; calls lower to dot-calls (time.sleep(1)).
--
-- Key Components:
--   - now(): CPU seconds (float, via os.clock) -- cheap, for relative timing
--   - unix(): wall-clock seconds since the Unix epoch (float, sub-second)
--   - monotonic(): seconds from a clock that never steps backward
--   - sleep(seconds): cooperatively pause this green thread for >= seconds
--
-- Note:
--   sleep yields to the scheduler: other green threads run while it waits, and
--   novel.run() blocks in poll(2) until the deadline. A non-positive duration
--   is a plain yield (lets other ready threads run, then resumes).

local novel = require("novel")
local ffi = require("ffi")

ffi.cdef[[
struct novel_timeval { long tv_sec; long tv_usec; };
int gettimeofday(struct novel_timeval *tv, void *tz);
]]

local time = {}

-- now returns CPU time in seconds (os.clock). Useful for measuring compute,
-- not wall-clock elapsed time (which includes sleeps and I/O).
function time.now()
  return os.clock()
end

-- monotonic returns seconds from a clock immune to wall-clock adjustments, the
-- right choice for measuring elapsed durations. Shares the scheduler's clock.
function time.monotonic()
  return novel.monotonic()
end

-- unix returns wall-clock seconds since the Unix epoch as a float with
-- microsecond resolution. Falls back to os.time() (whole seconds) if the FFI
-- call fails.
local tv = ffi.new("struct novel_timeval[1]")
function time.unix()
  if ffi.C.gettimeofday(tv, nil) ~= 0 then
    return os.time()
  end
  return tonumber(tv[0].tv_sec) + tonumber(tv[0].tv_usec) / 1e6
end

-- sleep cooperatively pauses the calling green thread for at least `seconds`.
-- Other green threads continue to run while it waits.
function time.sleep(seconds)
  novel.sleep(seconds)
end

return time
