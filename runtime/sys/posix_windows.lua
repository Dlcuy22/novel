-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/posix_windows (low-level Windows file-descriptor I/O FFI backend)
--
-- Purpose:
--   Wraps standard C runtime file descriptor operations (_read, _write, _close)
--   using MSVCRT/UCRT via ffi.C.

local ffi = require("ffi")

ffi.cdef[[
int _read(int fd, void *buf, unsigned int count);
int _write(int fd, const void *buf, unsigned int count);
int _close(int fd);
char *strerror(int errnum);
]]

local M = {}

function M.strerror(e)
  return ffi.string(ffi.C.strerror(e or ffi.errno()))
end

-- Standard C errno values on Windows
M.EINTR       = 4
M.EAGAIN      = 11
M.EWOULDBLOCK = 11
M.EINPROGRESS = 36  -- Not typically used for Windows standard handles

local readbuf = ffi.new("char[?]", 65536)

-- read reads up to n bytes from fd using MSVCRT _read.
function M.read(fd, n)
  n = n or 4096
  if n > 65536 then n = 65536 end
  local got = ffi.C._read(fd, readbuf, n)
  if got < 0 then
    return nil, ffi.errno()
  end
  return ffi.string(readbuf, got), nil
end

-- write writes data to fd using MSVCRT _write.
function M.write(fd, data)
  local n = ffi.C._write(fd, data, #data)
  if n < 0 then
    return nil, ffi.errno()
  end
  return tonumber(n), nil
end

-- close closes fd using MSVCRT _close.
function M.close(fd)
  return ffi.C._close(fd) == 0
end

-- setNonblocking is a no-op on Windows standard handles.
function M.setNonblocking(fd)
  return true, nil
end

return M
