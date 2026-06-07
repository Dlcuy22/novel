-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/posix_posix (low-level POSIX file-descriptor I/O for the stdlib)
--
-- Purpose:
--   Shared FFI wrappers for raw fd operations (read/write/close, non-blocking
--   mode) used by the async stdlib modules: std/io reads stdin through here,
--   and std/net layers sockets on top. Keeping the cdefs in one place avoids
--   duplicate (and potentially conflicting) FFI declarations across modules.

local ffi = require("ffi")

ffi.cdef[[
long read(int fd, void *buf, unsigned long count);
long write(int fd, const void *buf, unsigned long count);
int close(int fd);
int fcntl(int fd, int cmd, int arg);
char *strerror(int errnum);
]]

local M = {}

-- strerror returns the message for an errno (the current errno if omitted).
function M.strerror(e)
  return ffi.string(ffi.C.strerror(e or ffi.errno()))
end

-- errno values (identical on Linux/macOS/BSD for these).
M.EINTR        = 4
M.EAGAIN       = 11   -- == EWOULDBLOCK on Linux
M.EWOULDBLOCK  = 11
M.EINPROGRESS  = 115  -- Linux; macOS uses 36 (handled in socket layer)

-- fcntl command/flag constants (Linux/macOS/BSD share these values).
local F_GETFL = 3
local F_SETFL = 4
local O_NONBLOCK = 0x800   -- Linux
if ffi.os == "OSX" or ffi.os == "BSD" then
  O_NONBLOCK = 0x0004
  M.EINPROGRESS = 36
end

local readbuf = ffi.new("char[?]", 65536)

-- read reads up to n bytes from fd. Returns:
--   (data, nil)  on success ("" indicates end of file)
--   (nil, errno) on error (caller checks EAGAIN/EINTR for retry)
function M.read(fd, n)
  n = n or 4096
  if n > 65536 then n = 65536 end
  local got = ffi.C.read(fd, readbuf, n)
  if got < 0 then
    return nil, ffi.errno()
  end
  return ffi.string(readbuf, got), nil
end

-- write writes data to fd. Returns (bytes_written, nil) or (nil, errno). A
-- short write (fewer bytes than #data) is normal on a non-blocking fd; the
-- caller loops on the remainder.
function M.write(fd, data)
  local n = ffi.C.write(fd, data, #data)
  if n < 0 then
    return nil, ffi.errno()
  end
  return tonumber(n), nil
end

-- close closes fd, returning true on success.
function M.close(fd)
  return ffi.C.close(fd) == 0
end

-- setNonblocking switches fd to non-blocking mode so reads/writes return
-- EAGAIN instead of stalling the VM, letting the reactor schedule around them.
function M.setNonblocking(fd)
  local flags = ffi.C.fcntl(fd, F_GETFL, 0)
  if flags < 0 then
    return false, ffi.errno()
  end
  if ffi.C.fcntl(fd, F_SETFL, bit.bor(flags, O_NONBLOCK)) < 0 then
    return false, ffi.errno()
  end
  return true, nil
end

return M
