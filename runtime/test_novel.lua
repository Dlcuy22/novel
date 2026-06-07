-- Smoke test for the Novel runtime shim. Run with: luajit runtime/test_novel.lua
-- Exits non-zero on failure so `make test-runtime` can gate CI.

package.path = (arg[0]:match("(.*/)") or "./") .. "?.lua;" .. package.path
local novel = require("novel")
local poll = require("sys/poll")
local function poll_now() return poll.monotonic() end

local failures = 0
local function check(name, cond)
  if cond then
    print("ok   - " .. name)
  else
    print("FAIL - " .. name)
    failures = failures + 1
  end
end

-- Buffered fan-out (spec §10.5): 0+2+4+6 = 12.
do
  local ch = novel.chan(4)
  for i = 0, 3 do
    novel.spawn(function() novel.send(ch, i * 2) end)
  end
  local sum = 0
  novel.spawn(function()
    for _ = 1, 4 do sum = sum + novel.recv(ch) end
  end)
  novel.run()
  check("buffered fan-out sums to 12", sum == 12)
end

-- Unbuffered rendezvous.
do
  local ch = novel.chan()
  local got
  novel.spawn(function() novel.send(ch, "hi") end)
  novel.spawn(function() got = novel.recv(ch) end)
  novel.run()
  check("unbuffered rendezvous delivers value", got == "hi")
end

-- Close semantics: drained closed channel yields ok=false.
do
  local ch = novel.chan()
  local okFlag = true
  novel.spawn(function() novel.close(ch) end)
  novel.spawn(function() local _, ok = novel.recv(ch); okFlag = ok end)
  novel.run()
  check("recv on closed channel returns ok=false", okFlag == false)
end

-- error() constructor (spec §3.6).
do
  local e = novel.error("boom")
  check("error() builds { msg }", e.msg == "boom")
end

-- REPL helpers (language-spec.md §12). repr quotes strings, shows errors as
-- error("..."), and uses tostring otherwise.
do
  check("repr quotes a string", novel.repr("hi") == '"hi"')
  check("repr renders a number", novel.repr(42) == "42")
  check("repr renders a bool", novel.repr(true) == "true")
  check("repr renders an error value", novel.repr(novel.error("x")) == 'error("x")')
end

-- replprint echoes values; a lone nil prints nothing, and reset clears the
-- scheduler so a fresh run starts clean.
do
  novel.spawn(function() end)
  novel.reset()
  local ran = false
  novel.spawn(function() ran = true end)
  novel.run()
  check("reset clears the run queue then run drains fresh work", ran == true)
end

-- Async reactor (Phase 0): iowait, sleep, and run() staying alive while threads
-- are parked on I/O or timers. These exercise the poll(2)-backed event loop.
local ffi = require("ffi")
ffi.cdef[[
int pipe(int fildes[2]);
long write(int fd, const void *buf, unsigned long count);
long read(int fd, void *buf, unsigned long count);
int close(int fd);
]]

-- iowait parks a thread until an fd is readable; a second thread writes to the
-- pipe/socket, the reactor wakes the first. run() must NOT exit while the reader is
-- parked with nothing in the ready queue.
do
  local got = nil
  if ffi.os == "Windows" then
    local sock = require("sys/socket")
    local server_fd = sock.newTCP()
    sock.setReuseAddr(server_fd)
    assert(sock.bindListen(server_fd, "127.0.0.1", 0), "bindListen failed")
    local port = sock.getPort(server_fd)
    assert(port, "getPort failed")
    
    local client_fd = sock.newTCP()
    sock.connectStart(client_fd, "127.0.0.1", port)
    
    local conn_fd
    
    novel.spawn(function()
      conn_fd = sock.accept(server_fd)
      if not conn_fd then
        novel.iowait(server_fd, novel.POLLIN)
        conn_fd = sock.accept(server_fd)
      end
      assert(conn_fd, "accept failed")
      
      local data, err = sock.recv(conn_fd, 8)
      if not data then
        novel.iowait(conn_fd, novel.POLLIN)
        data, err = sock.recv(conn_fd, 8)
      end
      got = data
    end)
    
    novel.spawn(function()
      -- Yield first so the reader starts waiting.
      novel.sleep(0.01)
      local n, err = sock.send(client_fd, "Q")
      if not n then
        novel.iowait(client_fd, novel.POLLOUT)
        sock.send(client_fd, "Q")
      end
    end)
    
    novel.run()
    
    sock.close(client_fd)
    if conn_fd then sock.close(conn_fd) end
    sock.close(server_fd)
    check("iowait wakes a parked thread when the socket becomes readable (Windows)", got == "Q")
  else
    local fds = ffi.new("int[2]")
    assert(ffi.C.pipe(fds) == 0, "pipe() failed")
    local r, w = fds[0], fds[1]
    novel.spawn(function()
      local rev = novel.iowait(r, novel.POLLIN or 0x001)
      local buf = ffi.new("char[8]")
      local n = ffi.C.read(r, buf, 8)
      got = ffi.string(buf, n)
    end)
    novel.spawn(function()
      -- Yield first so the reader parks on the fd before we write.
      novel.sleep(0)
      ffi.C.write(w, "Q", 1)
    end)
    novel.run()
    ffi.C.close(r); ffi.C.close(w)
    check("iowait wakes a parked thread when the fd becomes readable", got == "Q")
  end
end

-- sleep parks on a timer; run() blocks in poll() until the deadline rather than
-- spinning or exiting early.
do
  local woke = false
  local before = poll_now()
  novel.spawn(function()
    novel.sleep(0.03)
    woke = true
  end)
  novel.run()
  local elapsed = poll_now() - before
  check("sleep parks the thread until its deadline", woke == true)
  check("sleep waited at least ~30ms", elapsed >= 0.02)
end

-- Timer ordering: shorter sleeps must wake before longer ones regardless of
-- spawn order.
do
  local order = {}
  novel.spawn(function() novel.sleep(0.04); order[#order + 1] = "slow" end)
  novel.spawn(function() novel.sleep(0.01); order[#order + 1] = "fast" end)
  novel.run()
  check("timers fire in deadline order", order[1] == "fast" and order[2] == "slow")
end

-- A channel-only program (no I/O, no timers) must still terminate exactly as
-- before: run() returns once the ready queue drains, never entering poll().
do
  local sum = 0
  local ch = novel.chan(2)
  novel.spawn(function() novel.send(ch, 10); novel.send(ch, 32) end)
  novel.spawn(function() sum = novel.recv(ch) + novel.recv(ch) end)
  novel.run()
  check("channel-only program still terminates without the reactor", sum == 42)
end

if failures > 0 then
  print(failures .. " failure(s)")
  os.exit(1)
end
print("all runtime tests passed")
