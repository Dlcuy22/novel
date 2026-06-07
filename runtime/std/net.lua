-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/net (asynchronous TCP/UDP sockets for Novel)
--
-- Purpose:
--   Cooperative network I/O over the green-thread scheduler. Sockets are
--   non-blocking (sys/socket); when an operation would block, the calling green
--   thread parks on fd readiness via novel.iowait and the reactor reschedules
--   it, so one connection blocking never stalls the others. Imported with
--   `import std.net`; calls lower to dot-calls (net.listenTCP(...)), and the
--   returned listener/conn/udp handles are objects called with method syntax
--   (conn.read(n) -> conn:read(n)).
--
-- Key Components:
--   - listenTCP(host, port) -> (listener, err): a bound, listening TCP socket
--   - dialTCP(host, port)   -> (conn, err): an established client connection
--   - bindUDP(host, port)   -> (udp, err): a bound UDP socket
--   - listener:accept()     -> (conn, err): next incoming connection (parks)
--   - conn:read(n) / conn:readLine() / conn:write(data) / conn:close()
--   - udp:recvfrom() / udp:sendto(data, ip, port) / udp:close()
--
-- Security:
--   Plaintext only. There is no TLS and no authentication; anything sensitive
--   must be tunneled or guarded by the caller. Bind to 127.0.0.1 rather than
--   0.0.0.0 unless a service is meant to be reachable off-host.
--
-- Method names:
--   Handles deliberately avoid the method names `len` and `append`, which the
--   Novel emitter rewrites to `#x` / table.insert on any receiver.

local novel = require("novel")
local sock = require("sys/socket")

local net = {}

-- Connection object: a connected TCP socket fd with buffered line reads.
local Conn = {}
Conn.__index = Conn

local function newConn(fd)
  return setmetatable({ fd = fd, rbuf = "", closed = false }, Conn)
end

-- read returns up to n bytes from the connection (default 4096). It parks the
-- green thread until data arrives. Returns:
--   (data, nil)  bytes ("" means the peer closed the connection / EOF)
--   (nil, err)   a real socket error
-- Any bytes previously buffered by readLine are returned first.
function Conn:read(n)
  n = n or 4096
  if #self.rbuf > 0 then
    local take = self.rbuf:sub(1, n)
    self.rbuf = self.rbuf:sub(#take + 1)
    return take, nil
  end
  while true do
    local data, err = sock.recv(self.fd, n)
    if data ~= nil then
      return data, nil
    end
    if err == "EAGAIN" then
      novel.iowait(self.fd, novel.POLLIN)
    else
      return nil, novel.error(err)
    end
  end
end

-- readLine returns the next CRLF/LF-terminated line (delimiter stripped), or
-- (nil, err). At a clean EOF before any newline it returns whatever trailing
-- bytes remain, then (nil, nil) on the next call (peer closed, buffer empty).
function Conn:readLine()
  while true do
    local nl = self.rbuf:find("\n", 1, true)
    if nl then
      local line = self.rbuf:sub(1, nl - 1)
      self.rbuf = self.rbuf:sub(nl + 1)
      if line:sub(-1) == "\r" then line = line:sub(1, -2) end
      return line, nil
    end
    local data, err = sock.recv(self.fd, 4096)
    if data == nil then
      if err == "EAGAIN" then
        novel.iowait(self.fd, novel.POLLIN)
      else
        return nil, novel.error(err)
      end
    elseif data == "" then
      -- EOF: flush any trailing partial line, then signal end.
      if #self.rbuf > 0 then
        local line = self.rbuf
        self.rbuf = ""
        return line, nil
      end
      return nil, nil
    else
      self.rbuf = self.rbuf .. data
    end
  end
end

-- write sends all of data, looping over partial writes and parking on POLLOUT
-- when the send buffer is full. Returns (true, nil) or (nil, err).
function Conn:write(data)
  local off = 1
  while off <= #data do
    local n, err = sock.send(self.fd, data:sub(off))
    if n ~= nil then
      off = off + n
    elseif err == "EAGAIN" then
      novel.iowait(self.fd, novel.POLLOUT)
    else
      return nil, novel.error(err)
    end
  end
  return true, nil
end

-- close shuts the connection. Safe to call more than once.
function Conn:close()
  if not self.closed then
    self.closed = true
    novel.closefd(self.fd)
    sock.close(self.fd)
  end
end

-- Listener object: a bound, listening TCP socket.
local Listener = {}
Listener.__index = Listener

-- accept returns the next incoming connection, parking the green thread until
-- one arrives. Returns (conn, nil) or (nil, err).
function Listener:accept()
  while true do
    local connfd, err = sock.accept(self.fd)
    if connfd ~= nil then
      return newConn(connfd), nil
    end
    if err == "EAGAIN" then
      novel.iowait(self.fd, novel.POLLIN)
    else
      return nil, novel.error(err)
    end
  end
end

-- close stops listening. Safe to call more than once.
function Listener:close()
  if not self.closed then
    self.closed = true
    novel.closefd(self.fd)
    sock.close(self.fd)
  end
end

-- listenTCP binds and listens on host:port. Use "127.0.0.1" for local-only or
-- "0.0.0.0" to accept from any interface (see the security note). Returns
-- (listener, nil) or (nil, err).
function net.listenTCP(host, port)
  local ip, rerr = sock.resolve(host)
  if ip == nil then return nil, novel.error(rerr) end
  local fd, serr = sock.newTCP()
  if fd == nil then return nil, novel.error(serr) end
  sock.setReuseAddr(fd)
  local ok, berr = sock.bindListen(fd, ip, port, 128)
  if not ok then
    sock.close(fd)
    return nil, novel.error(berr)
  end
  return setmetatable({ fd = fd, closed = false }, Listener), nil
end

-- dialTCP opens a client connection to host:port, parking until the connect
-- completes. Returns (conn, nil) or (nil, err).
function net.dialTCP(host, port)
  local ip, rerr = sock.resolve(host)
  if ip == nil then return nil, novel.error(rerr) end
  local fd, serr = sock.newTCP()
  if fd == nil then return nil, novel.error(serr) end
  local done, cerr = sock.connectStart(fd, ip, port)
  if done == nil then
    sock.close(fd)
    return nil, novel.error(cerr)
  end
  if not done then
    -- Non-blocking connect in progress: writable means it resolved.
    novel.iowait(fd, novel.POLLOUT)
  end
  return newConn(fd), nil
end

-- UDP object: a bound datagram socket.
local UDP = {}
UDP.__index = UDP

-- recvfrom returns the next datagram and its sender. Returns:
--   (data, ip, port)  on success
--   (nil, err, nil)   on a real error
-- It parks the green thread until a datagram arrives.
function UDP:recvfrom()
  while true do
    local data, ipOrErr, port = sock.recvfrom(self.fd, 65536)
    if data ~= nil then
      return data, ipOrErr, port
    end
    if ipOrErr == "EAGAIN" then
      novel.iowait(self.fd, novel.POLLIN)
    else
      return nil, novel.error(ipOrErr), nil
    end
  end
end

-- sendto sends one datagram to ip:port. Returns (true, nil) or (nil, err).
function UDP:sendto(data, ip, port)
  while true do
    local n, err = sock.sendto(self.fd, data, ip, port)
    if n ~= nil then
      return true, nil
    end
    if err == "EAGAIN" then
      novel.iowait(self.fd, novel.POLLOUT)
    else
      return nil, novel.error(err)
    end
  end
end

-- close releases the UDP socket. Safe to call more than once.
function UDP:close()
  if not self.closed then
    self.closed = true
    novel.closefd(self.fd)
    sock.close(self.fd)
  end
end

-- bindUDP binds a UDP socket to host:port. Returns (udp, nil) or (nil, err).
function net.bindUDP(host, port)
  local ip, rerr = sock.resolve(host)
  if ip == nil then return nil, novel.error(rerr) end
  local fd, serr = sock.newUDP()
  if fd == nil then return nil, novel.error(serr) end
  local ok, berr = sock.bindUDP(fd, ip, port)
  if not ok then
    sock.close(fd)
    return nil, novel.error(berr)
  end
  return setmetatable({ fd = fd, closed = false }, UDP), nil
end

return net
