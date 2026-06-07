-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/exec_posix (low-level POSIX process execution for std/exec)
--
-- Purpose:
--   Spawns subprocesses, redirects stdout/stderr via pipes, and waits for
--   completion cooperatively using the runtime's green-thread scheduler.
--
-- Key Components:
--   - execute(path, args, capture_stdout, capture_stderr): spawns process,
--     reads pipes asynchronously, and reaps the child process.
--
-- Dependencies:
--   - sys/posix: raw read and non-blocking fd utilities
--   - novel: scheduler integration for thread parking
--
-- Error Types:
--   - novel.error on failure to fork, construct pipes, or wait for process exit.
--

local ffi = require("ffi")
local posix = require("sys/posix")
local novel = require("novel")

ffi.cdef[[
  int pipe(int pipefd[2]);
  int fork(void);
  int dup2(int oldfd, int newfd);
  int execvp(const char *file, char *const argv[]);
  int waitpid(int pid, int *wstatus, int options);
  void _exit(int status);
]]

local M = {}

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
  local stdout_pipe = ffi.new("int[2]")
  local stderr_pipe = ffi.new("int[2]")

  if capture_stdout then
    if ffi.C.pipe(stdout_pipe) < 0 then
      return nil, nil, -1, novel.error("pipe failed: " .. posix.strerror())
    end
  end

  if capture_stderr then
    if ffi.C.pipe(stderr_pipe) < 0 then
      if capture_stdout then
        ffi.C.close(stdout_pipe[0])
        ffi.C.close(stdout_pipe[1])
      end
      return nil, nil, -1, novel.error("pipe failed: " .. posix.strerror())
    end
  end

  local pid = ffi.C.fork()
  if pid < 0 then
    if capture_stdout then
      ffi.C.close(stdout_pipe[0])
      ffi.C.close(stdout_pipe[1])
    end
    if capture_stderr then
      ffi.C.close(stderr_pipe[0])
      ffi.C.close(stderr_pipe[1])
    end
    return nil, nil, -1, novel.error("fork failed: " .. posix.strerror())
  end

  if pid == 0 then
    -- Child process
    if capture_stdout then
      ffi.C.dup2(stdout_pipe[1], 1)
      ffi.C.close(stdout_pipe[0])
      ffi.C.close(stdout_pipe[1])
    end

    if capture_stderr then
      ffi.C.dup2(stderr_pipe[1], 2)
      ffi.C.close(stderr_pipe[0])
      ffi.C.close(stderr_pipe[1])
    end

    -- Construct null-terminated argv array
    local argv = ffi.new("char *[?]", #args + 2)
    argv[0] = ffi.cast("char *", path)
    for i = 1, #args do
      argv[i] = ffi.cast("char *", args[i])
    end
    argv[#args + 1] = nil

    ffi.C.execvp(path, argv)
    -- If execvp returns, it failed to execute
    ffi.C._exit(127)
  end

  -- Parent process
  if capture_stdout then ffi.C.close(stdout_pipe[1]) end
  if capture_stderr then ffi.C.close(stderr_pipe[1]) end

  if capture_stdout then posix.setNonblocking(stdout_pipe[0]) end
  if capture_stderr then posix.setNonblocking(stderr_pipe[0]) end

  local stdout_buf = {}
  local stderr_buf = {}
  local stdout_eof = not capture_stdout
  local stderr_eof = not capture_stderr

  -- Read from the pipes cooperatively
  while not stdout_eof or not stderr_eof do
    local did_work = false

    if not stdout_eof then
      local chunk, err = posix.read(stdout_pipe[0], 4096)
      if chunk == nil then
        if err ~= posix.EAGAIN and err ~= posix.EWOULDBLOCK then
          stdout_eof = true
        end
      elseif chunk == "" then
        stdout_eof = true
      else
        table.insert(stdout_buf, chunk)
        did_work = true
      end
    end

    if not stderr_eof then
      local chunk, err = posix.read(stderr_pipe[0], 4096)
      if chunk == nil then
        if err ~= posix.EAGAIN and err ~= posix.EWOULDBLOCK then
          stderr_eof = true
        end
      elseif chunk == "" then
        stderr_eof = true
      else
        table.insert(stderr_buf, chunk)
        did_work = true
      end
    end

    if not did_work and (not stdout_eof or not stderr_eof) then
      -- Neither pipe has data: yield the thread and wait for readiness events
      if not stdout_eof then
        novel.iowait(stdout_pipe[0], novel.POLLIN)
      elseif not stderr_eof then
        novel.iowait(stderr_pipe[0], novel.POLLIN)
      end
    end
  end

  if capture_stdout then ffi.C.close(stdout_pipe[0]) end
  if capture_stderr then ffi.C.close(stderr_pipe[0]) end

  local status = ffi.new("int[1]")
  local ret_pid = ffi.C.waitpid(pid, status, 0)
  if ret_pid < 0 then
    return nil, nil, -1, novel.error("waitpid failed: " .. posix.strerror())
  end

  -- Extract exit status code using standard POSIX macros
  local termsig = bit.band(status[0], 0x7f)
  if termsig ~= 0 then
    return nil, nil, -1, novel.error("process terminated by signal " .. tostring(termsig))
  end

  local exit_code = bit.band(bit.rshift(status[0], 8), 0xff)
  return table.concat(stdout_buf), table.concat(stderr_buf), exit_code, nil
end

return M
