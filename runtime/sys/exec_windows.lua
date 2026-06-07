-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/exec_windows (low-level Windows process execution for std/exec)
--
-- Purpose:
--   Spawns subprocesses, redirects stdout/stderr via Windows anonymous pipes,
--   and waits for completion using the Win32 API.
--
-- Key Components:
--   - execute(path, args, capture_stdout, capture_stderr): spawns process,
--     reads pipes asynchronously without blocking using PeekNamedPipe, and reaps handles.
--
-- Dependencies:
--   - kernel32.dll: standard Win32 process execution functions
--   - novel: scheduler integration for thread yielding
--
-- Error Types:
--   - novel.error on failure to CreateProcessA or construct pipes.
--

local ffi = require("ffi")
local novel = require("novel")

ffi.cdef[[
  typedef struct _SECURITY_ATTRIBUTES {
      unsigned long nLength;
      void *lpSecurityDescriptor;
      int bInheritHandle;
  } SECURITY_ATTRIBUTES;

  typedef struct _STARTUPINFOA {
      unsigned long cb;
      char *lpReserved;
      char *lpDesktop;
      char *lpTitle;
      unsigned long dwX;
      unsigned long dwY;
      unsigned long dwXSize;
      unsigned long dwYSize;
      unsigned long dwXCountChars;
      unsigned long dwYCountChars;
      unsigned long dwFillAttribute;
      unsigned long dwFlags;
      unsigned short wShowWindow;
      unsigned short cbReserved2;
      void *lpReserved2;
      void *hStdInput;
      void *hStdOutput;
      void *hStdError;
  } STARTUPINFOA;

  typedef struct _PROCESS_INFORMATION {
      void *hProcess;
      void *hThread;
      unsigned long dwProcessId;
      unsigned long dwThreadId;
  } PROCESS_INFORMATION;

  void *GetStdHandle(unsigned long nStdHandle);
  int CreatePipe(void **lpReadPipe, void **lpWritePipe, SECURITY_ATTRIBUTES *lpPipeAttributes, unsigned long nSize);
  int SetHandleInformation(void *hObject, unsigned long dwMask, unsigned long dwFlags);
  int CreateProcessA(const char *lpApplicationName, char *lpCommandLine, SECURITY_ATTRIBUTES *lpProcessAttributes, SECURITY_ATTRIBUTES *lpThreadAttributes, int bInheritHandles, unsigned long dwCreationFlags, void *lpEnvironment, const char *lpCurrentDirectory, STARTUPINFOA *lpStartupInfo, PROCESS_INFORMATION *lpProcessInformation);
  unsigned long WaitForSingleObject(void *hHandle, unsigned long dwMilliseconds);
  int GetExitCodeProcess(void *hProcess, unsigned long *lpExitCode);
  int CloseHandle(void *hObject);
  int ReadFile(void *hFile, void *lpBuffer, unsigned long nNumberOfBytesToRead, unsigned long *lpNumberOfBytesRead, void *lpOverlapped);
  int PeekNamedPipe(void *hNamedPipe, void *lpBuffer, unsigned long nBufferSize, unsigned long *lpBytesRead, unsigned long *lpTotalBytesAvail, unsigned long *lpBytesLeftThisMessage);
  unsigned long GetLastError();
]]

local kernel32 = ffi.load("kernel32")
local M = {}

local STARTF_USESTDHANDLES = 0x00000100
local HANDLE_FLAG_INHERIT = 0x00000001
local WAIT_TIMEOUT = 258

local function lastError()
  return "Windows error code " .. tostring(kernel32.GetLastError())
end

local function escapeArg(arg)
  if arg:find('[%s"]') then
    return '"' .. arg:gsub('"', '\\"') .. '"'
  end
  return arg
end

local function buildCommandLine(path, args)
  local parts = { escapeArg(path) }
  for _, arg in ipairs(args) do
    table.insert(parts, escapeArg(arg))
  end
  return table.concat(parts, " ")
end

-- execute runs a command with the given arguments, captures output, and reaps the process.
--
-- params:
--   path: absolute or relative path to the binary
--   args: array of command arguments
--   capture_stdout: boolean indicating if stdout should be returned
--   capture_stderr: boolean indicating if stderr should be returned
-- returns:
--   stdout_data: captured stdout string (or nil)
--   stderr_data: captured stderr string (or nil)
--   exit_code: process exit code
--   err: error object if the command failed to execute
function M.execute(path, args, capture_stdout, capture_stderr)
  local sa = ffi.new("SECURITY_ATTRIBUTES")
  sa.nLength = ffi.sizeof(sa)
  sa.lpSecurityDescriptor = nil
  sa.bInheritHandle = 1

  local stdout_read = ffi.new("void *[1]")
  local stdout_write = ffi.new("void *[1]")
  local stderr_read = ffi.new("void *[1]")
  local stderr_write = ffi.new("void *[1]")

  if capture_stdout then
    if kernel32.CreatePipe(stdout_read, stdout_write, sa, 0) == 0 then
      return nil, nil, -1, novel.error("CreatePipe failed: " .. lastError())
    end
    kernel32.SetHandleInformation(stdout_read[0], HANDLE_FLAG_INHERIT, 0)
  end

  if capture_stderr then
    if kernel32.CreatePipe(stderr_read, stderr_write, sa, 0) == 0 then
      if capture_stdout then
        kernel32.CloseHandle(stdout_read[0])
        kernel32.CloseHandle(stdout_write[0])
      end
      return nil, nil, -1, novel.error("CreatePipe failed: " .. lastError())
    end
    kernel32.SetHandleInformation(stderr_read[0], HANDLE_FLAG_INHERIT, 0)
  end

  local si = ffi.new("STARTUPINFOA")
  si.cb = ffi.sizeof(si)
  si.dwFlags = STARTF_USESTDHANDLES
  si.hStdInput = kernel32.GetStdHandle(ffi.cast("unsigned long", -10))
  si.hStdOutput = capture_stdout and stdout_write[0] or kernel32.GetStdHandle(ffi.cast("unsigned long", -11))
  si.hStdError = capture_stderr and stderr_write[0] or kernel32.GetStdHandle(ffi.cast("unsigned long", -12))

  local pi = ffi.new("PROCESS_INFORMATION")
  local cmdLine = buildCommandLine(path, args)

  if kernel32.CreateProcessA(nil, ffi.cast("char *", cmdLine), nil, nil, 1, 0, nil, nil, si, pi) == 0 then
    if capture_stdout then
      kernel32.CloseHandle(stdout_read[0])
      kernel32.CloseHandle(stdout_write[0])
    end
    if capture_stderr then
      kernel32.CloseHandle(stderr_read[0])
      kernel32.CloseHandle(stderr_write[0])
    end
    return nil, nil, -1, novel.error("CreateProcessA failed: " .. lastError())
  end

  -- Close the write ends in the parent so it does not block on reading
  if capture_stdout then kernel32.CloseHandle(stdout_write[0]) end
  if capture_stderr then kernel32.CloseHandle(stderr_write[0]) end

  local stdout_buf = {}
  local stderr_buf = {}
  local stdout_eof = not capture_stdout
  local stderr_eof = not capture_stderr

  local temp_buf = ffi.new("char[4096]")
  local bytes_read = ffi.new("unsigned long[1]")
  local avail = ffi.new("unsigned long[1]")

  -- Read from the pipes using non-blocking Win32 PeekNamedPipe checks
  while not stdout_eof or not stderr_eof do
    local did_work = false

    if not stdout_eof then
      if kernel32.PeekNamedPipe(stdout_read[0], nil, 0, nil, avail, nil) == 0 then
        stdout_eof = true
      elseif avail[0] > 0 then
        local to_read = math.min(avail[0], 4096)
        if kernel32.ReadFile(stdout_read[0], temp_buf, to_read, bytes_read, nil) == 0 then
          stdout_eof = true
        else
          table.insert(stdout_buf, ffi.string(temp_buf, bytes_read[0]))
          did_work = true
        end
      else
        -- Check if process exited to determine if we hit EOF
        if kernel32.WaitForSingleObject(pi.hProcess, 0) ~= WAIT_TIMEOUT then
          stdout_eof = true
        end
      end
    end

    if not stderr_eof then
      if kernel32.PeekNamedPipe(stderr_read[0], nil, 0, nil, avail, nil) == 0 then
        stderr_eof = true
      elseif avail[0] > 0 then
        local to_read = math.min(avail[0], 4096)
        if kernel32.ReadFile(stderr_read[0], temp_buf, to_read, bytes_read, nil) == 0 then
          stderr_eof = true
        else
          table.insert(stderr_buf, ffi.string(temp_buf, bytes_read[0]))
          did_work = true
        end
      else
        -- Check if process exited to determine if we hit EOF
        if kernel32.WaitForSingleObject(pi.hProcess, 0) ~= WAIT_TIMEOUT then
          stderr_eof = true
        end
      end
    end

    if not did_work and (not stdout_eof or not stderr_eof) then
      -- Yield execution to other green threads
      novel.sleep(0.01)
    end
  end

  if capture_stdout then kernel32.CloseHandle(stdout_read[0]) end
  if capture_stderr then kernel32.CloseHandle(stderr_read[0]) end

  kernel32.WaitForSingleObject(pi.hProcess, 0xFFFFFFFF) -- wait for final cleanup

  local exit_code = ffi.new("unsigned long[1]")
  if kernel32.GetExitCodeProcess(pi.hProcess, exit_code) == 0 then
    kernel32.CloseHandle(pi.hProcess)
    kernel32.CloseHandle(pi.hThread)
    return nil, nil, -1, novel.error("GetExitCodeProcess failed: " .. lastError())
  end

  kernel32.CloseHandle(pi.hProcess)
  kernel32.CloseHandle(pi.hThread)

  return table.concat(stdout_buf), table.concat(stderr_buf), tonumber(exit_code[0]), nil
end

return M
