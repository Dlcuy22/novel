-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/poll_windows (low-level Windows I/O readiness + monotonic clock)
--
-- Purpose:
--   The Windows FFI layer the Novel scheduler's event loop (novel.run) uses to
--   block on socket readiness and standard input. Sockets are handled via Winsock2
--   WSAPoll; stdin is monitored using QueryPerformanceCounter and WaitForSingleObject/PeekNamedPipe.

local ffi = require("ffi")

if ffi.abi("64bit") then
  ffi.cdef[[
    struct pollfd {
      uint64_t fd;
      short events;
      short revents;
    };
  ]]
else
  ffi.cdef[[
    struct pollfd {
      uint32_t fd;
      short events;
      short revents;
    };
  ]]
end

ffi.cdef[[
  typedef size_t SOCKET;
  int WSAPoll(struct pollfd *fdArray, unsigned long fds, int timeout);
  int WSAGetLastError();
  int ioctlsocket(SOCKET s, unsigned long cmd, unsigned long *argp);

  void *GetStdHandle(unsigned long nStdHandle);
  unsigned long WaitForSingleObject(void *hHandle, unsigned long dwMilliseconds);
  int PeekNamedPipe(void *hNamedPipe, void *lpBuffer, unsigned long nBufferSize, unsigned long *lpBytesRead, unsigned long *lpTotalBytesAvail, unsigned long *lpBytesLeftThisMessage);
  void Sleep(unsigned long dwMilliseconds);

  int QueryPerformanceCounter(int64_t *lpPerformanceCount);
  int QueryPerformanceFrequency(int64_t *lpPerformanceCount);
]]

local ws2_32 = ffi.load("ws2_32")
local kernel32 = ffi.load("kernel32")

local M = {}

-- Winsock2 WSAPoll event flags
M.POLLIN   = 0x0300
M.POLLOUT  = 0x0010
M.POLLERR  = 0x0001
M.POLLHUP  = 0x0002
M.POLLNVAL = 0x0004

-- High-resolution monotonic timing
local qpc = ffi.new("int64_t[1]")
local qpf = ffi.new("int64_t[1]")
local has_qpf = kernel32.QueryPerformanceFrequency(qpf) ~= 0
local freq = tonumber(qpf[0])

function M.monotonic()
  if has_qpf then
    kernel32.QueryPerformanceCounter(qpc)
    return tonumber(qpc[0]) / freq
  end
  return os.clock()
end

function M.errno() return ws2_32.WSAGetLastError() end
function M.strerror(e)
  return "Windows Socket error " .. tostring(e or ws2_32.WSAGetLastError())
end

local STD_INPUT_HANDLE = ffi.cast("unsigned long", -10)
local hStdin = kernel32.GetStdHandle(STD_INPUT_HANDLE)
local INVALID_HANDLE_VALUE = ffi.cast("void *", -1)

-- Helper to check if a socket is valid/alive
local function is_valid_socket(fd)
  local arg = ffi.new("unsigned long[1]")
  local rc = ws2_32.ioctlsocket(fd, 0x4004667f, arg) -- FIONREAD
  if rc ~= 0 then
    local err = ws2_32.WSAGetLastError()
    if err == 10038 then -- WSAENOTSOCK
      return false
    end
  end
  return true
end

-- Helper to check if stdin has input available
local function is_stdin_ready()
  if hStdin == nil or hStdin == INVALID_HANDLE_VALUE then
    return false
  end
  -- Check if it is a pipe first
  local avail = ffi.new("unsigned long[1]")
  if kernel32.PeekNamedPipe(hStdin, nil, 0, nil, avail, nil) ~= 0 then
    return avail[0] > 0
  end
  -- Otherwise, treat as console handle
  return kernel32.WaitForSingleObject(hStdin, 0) == 0
end

-- Buffer for WSAPollfd arrays
local pollbuf = nil
local pollcap = 0
local function ensure(n)
  if n > pollcap then
    pollcap = math.max(n, pollcap * 2, 8)
    pollbuf = ffi.new("struct pollfd[?]", pollcap)
  end
  return pollbuf
end

-- safe_wspoll wraps WSAPoll in pcall so that LuaJIT's "interrupted!" error
-- (raised when the FFI call is interrupted by a signal/event) is caught and
-- retried instead of crashing the scheduler.
local function safe_wspoll(buf, n, timeout_ms)
  local ok, rc = pcall(ws2_32.WSAPoll, buf, n, timeout_ms)
  if not ok then
    -- rc is the error message string; "interrupted!" means EINTR equivalent
    return 0
  end
  return rc
end

-- poll waits up to timeout_ms for readiness on the given fds.
function M.poll(entries, timeout_ms)
  local has_stdin = false
  local socket_entries = {}

  for _, entry in ipairs(entries) do
    if entry.fd == 0 then
      has_stdin = true
    else
      socket_entries[#socket_entries + 1] = entry
    end
  end

  -- If stdin is not being monitored, we can call WSAPoll directly
  if not has_stdin then
    local n = #socket_entries
    if n == 0 then
      if timeout_ms > 0 then
        kernel32.Sleep(timeout_ms)
      end
      return {}, 0
    end

    local buf = ensure(n)
    for i = 1, n do
      buf[i - 1].fd = socket_entries[i].fd
      buf[i - 1].events = socket_entries[i].events
      buf[i - 1].revents = 0
    end

    -- Cap per-call timeout to avoid long blocking FFI calls that LuaJIT cannot
    -- safely interrupt. Loop until the total requested timeout elapses.
    local MAX_SLICE = 500
    local start = M.monotonic()
    while true do
      local slice = timeout_ms
      if timeout_ms < 0 then
        slice = MAX_SLICE
      elseif timeout_ms > MAX_SLICE then
        local elapsed_ms = (M.monotonic() - start) * 1000
        local remaining = timeout_ms - elapsed_ms
        if remaining <= 0 then break end
        slice = remaining > MAX_SLICE and MAX_SLICE or remaining
      end

      -- Reset revents before each call
      for i = 1, n do buf[i - 1].revents = 0 end

      local rc = safe_wspoll(buf, n, slice)
      local ready = {}
      if rc < 0 then
        local err = ws2_32.WSAGetLastError()
        if err == 10038 or err == 10022 then
          local found_invalid = false
          for i = 1, n do
            local fd = tonumber(buf[i - 1].fd)
            if not is_valid_socket(fd) then
              ready[#ready + 1] = { fd = fd, revents = M.POLLNVAL }
              found_invalid = true
            end
          end
          if found_invalid then
            return ready, #ready
          end
        end
        -- Transient error: brief sleep then retry
        kernel32.Sleep(1)
      elseif rc > 0 then
        for i = 1, n do
          local rev = buf[i - 1].revents
          if rev ~= 0 then
            ready[#ready + 1] = { fd = tonumber(buf[i - 1].fd), revents = rev }
          end
        end
        return ready, rc
      end

      -- rc == 0 (timeout on this slice). If the caller gave a finite timeout,
      -- check if total time has elapsed; if infinite (-1), loop forever.
      if timeout_ms >= 0 then
        local elapsed_ms = (M.monotonic() - start) * 1000
        if elapsed_ms >= timeout_ms then break end
      end
    end
    return {}, 0
  end

  -- Multiplexing: poll sockets with a short timeout and poll stdin
  local start_time = M.monotonic()
  local timeout_sec = timeout_ms / 1000.0
  local ready = {}
  local rc = 0

  while true do
    -- Check if stdin is ready
    if is_stdin_ready() then
      ready[#ready + 1] = { fd = 0, revents = M.POLLIN }
      rc = rc + 1
    end

    -- Poll sockets
    local num_sockets = #socket_entries
    if num_sockets > 0 then
      local buf = ensure(num_sockets)
      for i = 1, num_sockets do
        buf[i - 1].fd = socket_entries[i].fd
        buf[i - 1].events = socket_entries[i].events
        buf[i - 1].revents = 0
      end
      local ws_rc = safe_wspoll(buf, num_sockets, 10)
      if ws_rc > 0 then
        for i = 1, num_sockets do
          local rev = buf[i - 1].revents
          if rev ~= 0 then
            ready[#ready + 1] = { fd = tonumber(buf[i - 1].fd), revents = rev }
            rc = rc + 1
          end
        end
      elseif ws_rc < 0 then
        local err = ws2_32.WSAGetLastError()
        if err == 10038 or err == 10022 then
          for i = 1, num_sockets do
            local fd = tonumber(buf[i - 1].fd)
            if not is_valid_socket(fd) then
              ready[#ready + 1] = { fd = fd, revents = M.POLLNVAL }
              rc = rc + 1
            end
          end
        end
      end
    end

    -- Return if any events occurred or if we timed out
    if rc > 0 then
      break
    end

    local elapsed = M.monotonic() - start_time
    if timeout_ms >= 0 and elapsed >= timeout_sec then
      break
    end

    kernel32.Sleep(10)
  end

  return ready, rc
end

return M
