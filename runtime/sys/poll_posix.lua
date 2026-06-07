-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/poll_posix (low-level POSIX I/O readiness + monotonic clock for the reactor)
--
-- Purpose:
--   The thin FFI layer the Novel scheduler's event loop (novel.run) uses to
--   block on file-descriptor readiness and to time sleeps. Wraps poll(2) and
--   clock_gettime(CLOCK_MONOTONIC). Kept separate from novel.lua so the
--   scheduler stays readable and the raw FFI lives in one place.
--
-- Key Components:
--   - poll.poll(entries, timeout_ms): wait for readiness on a set of fds
--   - poll.monotonic(): seconds from a monotonic clock (for timers/sleep)
--   - poll.errno()/poll.strerror(): last-call errno and its message
--   - POLLIN/POLLOUT/POLLERR/POLLHUP/POLLNVAL: event flags
--
-- Platform:
--   POSIX (Linux, macOS, BSD). struct pollfd and the POLL* flag values are
--   identical across these. CLOCK_MONOTONIC's numeric id differs by OS and is
--   branched on ffi.os.

local ffi = require("ffi")

ffi.cdef[[
struct pollfd { int fd; short events; short revents; };
int poll(struct pollfd *fds, unsigned long nfds, int timeout);

struct novel_timespec { long tv_sec; long tv_nsec; };
int clock_gettime(int clk_id, struct novel_timespec *tp);

char *strerror(int errnum);
]]

local M = {}

-- poll(2) event flags. Same numeric values on Linux/macOS/BSD.
M.POLLIN   = 0x001 -- data available to read
M.POLLOUT  = 0x004 -- writable without blocking
M.POLLERR  = 0x008 -- error condition (output only)
M.POLLHUP  = 0x010 -- peer hung up (output only)
M.POLLNVAL = 0x020 -- fd not open (output only)

-- CLOCK_MONOTONIC id by platform (Linux 1, macOS 6, FreeBSD 4).
local CLOCK_MONOTONIC = 1
if ffi.os == "OSX" then
  CLOCK_MONOTONIC = 6
elseif ffi.os == "BSD" then
  CLOCK_MONOTONIC = 4
end

-- errno of the most recent C call, and its human-readable message.
function M.errno() return ffi.errno() end
function M.strerror(e)
  return ffi.string(ffi.C.strerror(e or ffi.errno()))
end

-- monotonic returns seconds (float) from a clock that never steps backward, so
-- timer deadlines are immune to wall-clock adjustments. Falls back to os.clock
-- if clock_gettime is unavailable.
local ts = ffi.new("struct novel_timespec[1]")
function M.monotonic()
  if ffi.C.clock_gettime(CLOCK_MONOTONIC, ts) ~= 0 then
    return os.clock()
  end
  return tonumber(ts[0].tv_sec) + tonumber(ts[0].tv_nsec) / 1e9
end

-- A growing scratch buffer for pollfd arrays, reused across calls so the hot
-- loop in the scheduler does not allocate every tick.
local pollbuf = nil
local pollcap = 0
local function ensure(n)
  if n > pollcap then
    pollcap = math.max(n, pollcap * 2, 8)
    pollbuf = ffi.new("struct pollfd[?]", pollcap)
  end
  return pollbuf
end

-- poll waits up to timeout_ms for readiness on the given fds.
--   entries:    array of { fd = <int>, events = <POLL* mask> }
--   timeout_ms: -1 block indefinitely, 0 return immediately, >0 milliseconds
-- Returns (ready, rc):
--   ready: array of { fd = <int>, revents = <POLL* mask> } for fds that fired
--   rc:    the poll(2) return (>0 count, 0 timeout, <0 error e.g. EINTR)
-- With no entries it performs a pure timed sleep (poll(NULL, 0, timeout_ms)).
function M.poll(entries, timeout_ms)
  local n = #entries
  if n == 0 then
    ffi.C.poll(nil, 0, timeout_ms)
    return {}, 0
  end
  local buf = ensure(n)
  for i = 1, n do
    buf[i - 1].fd = entries[i].fd
    buf[i - 1].events = entries[i].events
    buf[i - 1].revents = 0
  end
  local rc = ffi.C.poll(buf, n, timeout_ms)
  local ready = {}
  if rc > 0 then
    for i = 1, n do
      local rev = buf[i - 1].revents
      if rev ~= 0 then
        ready[#ready + 1] = { fd = buf[i - 1].fd, revents = rev }
      end
    end
  end
  return ready, rc
end

return M
