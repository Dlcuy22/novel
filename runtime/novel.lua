-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: novel (runtime shim for LuaJIT)
--
-- Purpose:
--   The runtime that emitted Lua loads via `local novel = require("novel")`.
--   Provides Novel's green-thread scheduler and channels by lowering its
--   concurrency primitives onto LuaJIT coroutines (language-spec.md §13.2):
--
--     spawn fn(){...}()  -> novel.spawn(function() ... end)
--     chan<T>()          -> novel.chan()        -- unbuffered
--     chan<T>(n)         -> novel.chan(n)        -- buffered, capacity n
--     send(ch, v)        -> novel.send(ch, v)
--     recv(ch)           -> novel.recv(ch)       -- returns value, ok
--     close(ch)          -> novel.close(ch)
--
-- Key Components:
--   - novel.spawn(fn): schedule fn as a green thread
--   - novel.run():     drain the run queue until nothing is runnable
--   - novel.chan(cap): create an (un)buffered channel
--   - novel.send/recv/close: blocking channel ops via coroutine yield/resume
--   - novel.error(msg): construct a Novel error value { msg = ... } (§3.6)
--
-- Async I/O:
--   The scheduler is also a reactor. A green thread can park on file-descriptor
--   readiness via novel.iowait(fd, events) or on a deadline via
--   novel.sleep(seconds); when the run queue empties, novel.run() blocks in
--   poll(2) (see sys/poll) until an fd is ready or the nearest timer fires,
--   then re-enqueues the woken threads. This lets blocking I/O (sockets,
--   stdin, sleeps) suspend just one green thread instead of stalling the whole
--   VM. run() returns only when the run queue, the I/O waiters, AND the timers
--   are all empty.
--
-- Note:
--   The scheduler is cooperative: green threads run until they block on a
--   channel, an fd, or a timer (or finish), then yield back. novel.run() must
--   be called once at the end of `main`; the emitter appends it.

local poll = require("sys/poll")

local novel = {}

-- Run queue of ready coroutines, as a FIFO. We track head/tail explicitly
-- rather than using `#ready`: once we set freed slots to nil the table has
-- holes, and Lua's length operator is undefined on tables with holes.
local ready = {}      -- map of index -> coroutine
local head = 1        -- index of next coroutine to run
local tail = 0        -- index of last enqueued coroutine

-- Reactor state for async I/O.
--   waiters: fd -> { co = coroutine, events = POLL* mask, revents = ... }
--            one green thread parked per fd waiting for readiness.
--   timers:  list of { co = coroutine, deadline = monotonic seconds }
--            green threads parked on novel.sleep.
local waiters = {}
local timers = {}

local function enqueue(co)
  tail = tail + 1
  ready[tail] = co
end

local function dequeue()
  if head > tail then return nil end
  local co = ready[head]
  ready[head] = nil
  head = head + 1
  return co
end

-- The coroutine currently being resumed by the scheduler.
local current = nil

-- Re-export the poll(2) event flags so stdlib I/O modules (net, io) can pass
-- them to novel.iowait without each requiring sys/poll directly.
novel.POLLIN   = poll.POLLIN
novel.POLLOUT  = poll.POLLOUT
novel.POLLERR  = poll.POLLERR
novel.POLLHUP  = poll.POLLHUP
novel.POLLNVAL = poll.POLLNVAL

-- monotonic exposes the reactor's clock (seconds, float) for stdlib timing.
function novel.monotonic() return poll.monotonic() end

-- spawn schedules fn to run as a green thread.
function novel.spawn(fn)
  local co = coroutine.create(fn)
  enqueue(co)
  return co
end

-- iowait parks the current green thread until fd is ready for the given POLL*
-- events (POLLIN/POLLOUT). Returns the revents mask poll(2) reported, which may
-- include POLLHUP/POLLERR/POLLNVAL even when not requested, so callers can
-- detect a closed or broken fd. One waiter per fd: a second iowait on the same
-- fd replaces the first (sockets are owned by a single green thread).
function novel.iowait(fd, events)
  local self = { co = current, events = events }
  waiters[fd] = self
  coroutine.yield()
  return self.revents or 0
end

-- sleep parks the current green thread for at least `seconds` (a float). A
-- non-positive duration is a cooperative yield: the thread goes to the back of
-- the run queue so other ready threads run, then resumes.
function novel.sleep(seconds)
  if seconds == nil or seconds <= 0 then
    enqueue(current)
    coroutine.yield()
    return
  end
  timers[#timers + 1] = { co = current, deadline = poll.monotonic() + seconds }
  coroutine.yield()
end

-- reactor_poll blocks in poll(2) until an awaited fd is ready or the nearest
-- timer fires, then re-enqueues every woken green thread. Called by run() only
-- when the ready queue is empty but threads are parked on I/O or timers.
local function reactor_poll()
  -- Assemble the poll set from fd waiters.
  local entries = {}
  local byfd = {}
  for fd, w in pairs(waiters) do
    entries[#entries + 1] = { fd = fd, events = w.events }
    byfd[fd] = w
  end

  -- Timeout is the time until the nearest timer, or -1 (block) if none.
  local timeout = -1
  if #timers > 0 then
    local soonest = math.huge
    for _, t in ipairs(timers) do
      if t.deadline < soonest then soonest = t.deadline end
    end
    local ms = math.ceil((soonest - poll.monotonic()) * 1000)
    timeout = ms > 0 and ms or 0
  end

  local readyFds = poll.poll(entries, timeout)

  -- Wake fds that fired, handing each its revents.
  for _, rf in ipairs(readyFds) do
    local w = byfd[rf.fd]
    if w then
      w.revents = rf.revents
      waiters[rf.fd] = nil
      enqueue(w.co)
    end
  end

  -- Wake every timer whose deadline has passed; keep the rest.
  if #timers > 0 then
    local now = poll.monotonic()
    local pending = {}
    for _, t in ipairs(timers) do
      if t.deadline <= now then
        enqueue(t.co)
      else
        pending[#pending + 1] = t
      end
    end
    timers = pending
  end
end

-- run drives the scheduler until nothing is runnable: it resumes ready
-- coroutines, and when the run queue empties it blocks in the reactor for any
-- I/O- or timer-parked threads. It returns only when the run queue, the I/O
-- waiters, and the timers are all empty. Threads blocked on a channel are
-- re-enqueued by channel ops when they become runnable again.
function novel.run()
  while true do
    local co = dequeue()
    if co then
      if coroutine.status(co) ~= "dead" then
        current = co
        local ok, err = coroutine.resume(co)
        current = nil
        if not ok then
          error("novel: green thread crashed: " .. tostring(err), 0)
        end
      end
    elseif next(waiters) == nil and #timers == 0 then
      return
    else
      reactor_poll()
    end
  end
end

-- yield_blocked parks the current coroutine; the resumer must re-enqueue it.
local function block()
  return coroutine.yield()
end

-- chan creates a channel. capacity == nil or 0 means unbuffered.
function novel.chan(capacity)
  return {
    __novel_chan = true,
    cap = capacity or 0,
    buf = {},            -- queued values (FIFO via bcount/btail)
    bhead = 1,
    btail = 0,
    closed = false,
    recvq = {},          -- coroutines waiting to receive
    sendq = {},          -- {co=..., val=...} waiting to send
  }
end

local function buf_len(ch) return ch.btail - ch.bhead + 1 end

local function buf_push(ch, v)
  ch.btail = ch.btail + 1
  ch.buf[ch.btail] = v
end

local function buf_shift(ch)
  local v = ch.buf[ch.bhead]
  ch.buf[ch.bhead] = nil
  ch.bhead = ch.bhead + 1
  return v
end

-- send delivers v on ch. Blocks (yields) if the channel is full and no receiver
-- is waiting. Sending on a closed channel is a runtime error.
function novel.send(ch, v)
  if ch.closed then
    error("novel: send on closed channel", 0)
  end

  -- Hand directly to a waiting receiver if there is one.
  local rco = table.remove(ch.recvq, 1)
  if rco then
    -- Stash the value for the receiver to pick up on resume.
    rco.__novel_recv_val = v
    rco.__novel_recv_ok = true
    enqueue(rco.co)
    return
  end

  -- Room in the buffer: enqueue and continue.
  if buf_len(ch) < ch.cap then
    buf_push(ch, v)
    return
  end

  -- Otherwise block until a receiver takes the value.
  ch.sendq[#ch.sendq + 1] = { co = current, val = v }
  block()
end

-- recv returns (value, ok). ok is false when the channel is closed and drained.
function novel.recv(ch)
  -- Value already buffered.
  if buf_len(ch) > 0 then
    local v = buf_shift(ch)
    -- Wake one blocked sender into the freed slot.
    local s = table.remove(ch.sendq, 1)
    if s then
      buf_push(ch, s.val)
      enqueue(s.co)
    end
    return v, true
  end

  -- A sender is blocked (unbuffered rendezvous).
  local s = table.remove(ch.sendq, 1)
  if s then
    enqueue(s.co)
    return s.val, true
  end

  if ch.closed then
    return nil, false
  end

  -- Block until a sender hands us a value (or close wakes us).
  local self = { co = current }
  ch.recvq[#ch.recvq + 1] = self
  block()
  return self.__novel_recv_val, self.__novel_recv_ok == true
end

-- close marks ch closed and wakes all blocked receivers with (nil, false).
function novel.close(ch)
  if ch.closed then
    error("novel: close of closed channel", 0)
  end
  ch.closed = true
  for _, r in ipairs(ch.recvq) do
    r.__novel_recv_val = nil
    r.__novel_recv_ok = false
    enqueue(r.co)
  end
  ch.recvq = {}
end

-- error constructs a Novel error value: { msg = "..." } (language-spec.md §3.6).
function novel.error(msg)
  return { msg = msg }
end

-- closefd removes fd from the reactor's waiters, waking the parked thread (if any)
-- with POLLNVAL. This avoids polling closed sockets on platforms like Windows where
-- WSAPoll fails completely on invalid descriptors.
function novel.closefd(fd)
  if waiters[fd] then
    local w = waiters[fd]
    waiters[fd] = nil
    w.revents = novel.POLLNVAL
    enqueue(w.co)
  end
end

-- reset clears the scheduler's run queue and reactor state. Used by the REPL
-- driver to recover a clean scheduler after a green thread crashes mid-chunk.
function novel.reset()
  ready = {}
  head = 1
  tail = 0
  current = nil
  waiters = {}
  timers = {}
end

-- repr renders a value the way the REPL echoes it: strings are quoted so they
-- are distinguishable from bare identifiers, error tables show as error("..."),
-- everything else uses tostring.
function novel.repr(v)
  local t = type(v)
  if t == "string" then
    return string.format("%q", v)
  elseif t == "table" and type(v.msg) == "string" and next(v, "msg") == nil then
    return 'error(' .. string.format("%q", v.msg) .. ')'
  else
    return tostring(v)
  end
end

-- replprint echoes the result(s) of a bare REPL expression, one line, values
-- comma-separated. A lone nil (e.g. a call that returns nothing) prints nothing,
-- matching Python's handling of None-valued statements.
function novel.replprint(...)
  local n = select("#", ...)
  if n == 0 then return end
  if n == 1 and (select(1, ...)) == nil then return end
  local parts = {}
  for i = 1, n do
    parts[i] = novel.repr((select(i, ...)))
  end
  io.write(table.concat(parts, ", "), "\n")
end

return novel
