-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/socket_windows (low-level Winsock2 FFI for the Novel stdlib)
--
-- Purpose:
--   Raw, non-yielding socket primitives for Windows. Wraps the Winsock2 API
--   and handles WSAStartup initialization.

local ffi = require("ffi")

ffi.cdef[[
typedef size_t SOCKET;

struct sockaddr_in {
  uint16_t sin_family;
  uint16_t sin_port;
  uint32_t sin_addr;
  char     sin_zero[8];
};

struct sockaddr { uint16_t sa_family; char sa_data[14]; };

struct addrinfo {
  int ai_flags;
  int ai_family;
  int ai_socktype;
  int ai_protocol;
  size_t ai_addrlen;
  char *ai_canonname;
  struct sockaddr *ai_addr;
  struct addrinfo *ai_next;
};

int WSAStartup(uint16_t wVersionRequested, void *lpWSAData);
int WSACleanup();
int WSAGetLastError();

SOCKET socket(int af, int type, int protocol);
int closesocket(SOCKET s);
int bind(SOCKET s, const struct sockaddr *name, int namelen);
int listen(SOCKET s, int backlog);
SOCKET accept(SOCKET s, struct sockaddr *addr, int *addrlen);
int connect(SOCKET s, const struct sockaddr *name, int namelen);
int setsockopt(SOCKET s, int level, int optname, const void *optval, int optlen);
int ioctlsocket(SOCKET s, unsigned long cmd, unsigned long *argp);

int recv(SOCKET s, void *buf, int len, int flags);
int send(SOCKET s, const void *buf, int len, int flags);
int recvfrom(SOCKET s, void *buf, int len, int flags, struct sockaddr *from, int *fromlen);
int sendto(SOCKET s, const void *buf, int len, int flags, const struct sockaddr *to, int tolen);

uint16_t htons(uint16_t v);
uint16_t ntohs(uint16_t v);
uint32_t htonl(uint32_t v);
int inet_pton(int af, const char *src, void *dst);
const char *inet_ntop(int af, const void *src, char *dst, int size);

int getaddrinfo(const char *node, const char *service, const struct addrinfo *hints, struct addrinfo **res);
void freeaddrinfo(struct addrinfo *res);
int getsockname(SOCKET s, struct sockaddr *name, int *namelen);
]]

local ws2_32 = ffi.load("ws2_32")

-- Initialize Winsock 2.2
local wsaData = ffi.new("char[512]")
if ws2_32.WSAStartup(0x0202, wsaData) ~= 0 then
  error("sys/socket_windows: WSAStartup failed", 2)
end

local M = {}

M.AF_INET      = 2
M.SOCK_STREAM  = 1
M.SOCK_DGRAM   = 2
M.IPPROTO_TCP  = 6
M.IPPROTO_UDP  = 17

local SOL_SOCKET   = 0xffff
local SO_REUSEADDR = 0x0004
local FIONBIO      = 0x8004667e
local INVALID_SOCKET = ffi.cast("SOCKET", ffi.cast("int32_t", -1))

function M.lastError()
  local e = ws2_32.WSAGetLastError()
  return e, "Windows Socket error " .. tostring(e)
end

M.EAGAIN      = 10035 -- WSAEWOULDBLOCK
M.EWOULDBLOCK = 10035
M.EINTR       = 10004 -- WSAEINTR
M.EINPROGRESS = 10036 -- WSAEINPROGRESS

local function makeAddr(ip, port)
  local sa = ffi.new("struct sockaddr_in")
  sa.sin_family = M.AF_INET
  sa.sin_port = ws2_32.htons(port)
  local addrPtr = ffi.cast("char *", sa) + ffi.offsetof("struct sockaddr_in", "sin_addr")
  if ws2_32.inet_pton(M.AF_INET, ip, addrPtr) ~= 1 then
    return nil, "invalid IPv4 address: " .. tostring(ip)
  end
  return sa, ffi.sizeof("struct sockaddr_in")
end
M.makeAddr = makeAddr

local function isDottedIPv4(host)
  return host:match("^%d+%.%d+%.%d+%.%d+$") ~= nil
end

function M.resolve(host)
  if isDottedIPv4(host) then
    return host, nil
  end
  if host == "localhost" then
    return "127.0.0.1", nil
  end
  local hints = ffi.new("struct addrinfo")
  hints.ai_family = M.AF_INET
  hints.ai_socktype = M.SOCK_STREAM
  local res = ffi.new("struct addrinfo *[1]")
  local rc = ws2_32.getaddrinfo(host, nil, hints, res)
  if rc ~= 0 then
    return nil, "resolve " .. host .. ": Windows Socket error " .. tostring(rc)
  end
  local ai = res[0]
  local sin = ffi.cast("struct sockaddr_in *", ai.ai_addr)
  local buf = ffi.new("char[16]")
  local addrPtr = ffi.cast("char *", sin) + ffi.offsetof("struct sockaddr_in", "sin_addr")
  ws2_32.inet_ntop(M.AF_INET, addrPtr, buf, 16)
  local ip = ffi.string(buf)
  ws2_32.freeaddrinfo(ai)
  return ip, nil
end

function M.setNonblocking(s)
  local mode = ffi.new("unsigned long[1]", 1)
  if ws2_32.ioctlsocket(s, FIONBIO, mode) ~= 0 then
    return false, ws2_32.WSAGetLastError()
  end
  return true, nil
end

local function newSocket(socktype, proto)
  local fd = ws2_32.socket(M.AF_INET, socktype, proto)
  if fd == INVALID_SOCKET then
    local _, msg = M.lastError()
    return nil, "socket: " .. msg
  end
  local ok, err = M.setNonblocking(fd)
  if not ok then
    ws2_32.closesocket(fd)
    return nil, "setNonblocking: Windows Socket error " .. tostring(err)
  end
  return tonumber(fd), nil
end

function M.newTCP() return newSocket(M.SOCK_STREAM, M.IPPROTO_TCP) end
function M.newUDP() return newSocket(M.SOCK_DGRAM, M.IPPROTO_UDP) end

function M.setReuseAddr(fd)
  local one = ffi.new("int[1]", 1)
  ws2_32.setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, one, ffi.sizeof("int"))
end

function M.bindListen(fd, ip, port, backlog)
  local sa, salen = makeAddr(ip, port)
  if sa == nil then return nil, salen end
  if ws2_32.bind(fd, ffi.cast("struct sockaddr *", sa), salen) ~= 0 then
    local _, msg = M.lastError()
    return nil, "bind: " .. msg
  end
  if ws2_32.listen(fd, backlog or 128) ~= 0 then
    local _, msg = M.lastError()
    return nil, "listen: " .. msg
  end
  return true
end

function M.bindUDP(fd, ip, port)
  local sa, salen = makeAddr(ip, port)
  if sa == nil then return nil, salen end
  if ws2_32.bind(fd, ffi.cast("struct sockaddr *", sa), salen) ~= 0 then
    local _, msg = M.lastError()
    return nil, "bind: " .. msg
  end
  return true
end

function M.accept(fd)
  local connfd = ws2_32.accept(fd, nil, nil)
  if connfd == INVALID_SOCKET then
    local e = ws2_32.WSAGetLastError()
    if e == M.EAGAIN or e == M.EWOULDBLOCK or e == M.EINTR then
      return nil, "EAGAIN"
    end
    return nil, "accept: Windows Socket error " .. tostring(e)
  end
  M.setNonblocking(connfd)
  return tonumber(connfd), nil
end

function M.connectStart(fd, ip, port)
  local sa, salen = makeAddr(ip, port)
  if sa == nil then return nil, salen end
  if ws2_32.connect(fd, ffi.cast("struct sockaddr *", sa), salen) == 0 then
    return true, nil
  end
  local e = ws2_32.WSAGetLastError()
  if e == M.EINPROGRESS or e == M.EAGAIN or e == M.EWOULDBLOCK or e == M.EINTR then
    return false, nil
  end
  return nil, "connect: Windows Socket error " .. tostring(e)
end

local iobuf = ffi.new("char[?]", 65536)

function M.recv(fd, n)
  n = n or 4096
  if n > 65536 then n = 65536 end
  local got = ws2_32.recv(fd, iobuf, n, 0)
  if got < 0 then
    local e = ws2_32.WSAGetLastError()
    if e == M.EAGAIN or e == M.EWOULDBLOCK or e == M.EINTR then
      return nil, "EAGAIN"
    end
    return nil, "recv: Windows Socket error " .. tostring(e)
  end
  return ffi.string(iobuf, got), nil
end

function M.send(fd, data)
  local n = ws2_32.send(fd, data, #data, 0)
  if n < 0 then
    local e = ws2_32.WSAGetLastError()
    if e == M.EAGAIN or e == M.EWOULDBLOCK or e == M.EINTR then
      return nil, "EAGAIN"
    end
    return nil, "send: Windows Socket error " .. tostring(e)
  end
  return tonumber(n), nil
end

function M.recvfrom(fd, n)
  n = n or 65536
  if n > 65536 then n = 65536 end
  local src = ffi.new("struct sockaddr_in")
  local srclen = ffi.new("int[1]", ffi.sizeof("struct sockaddr_in"))
  local got = ws2_32.recvfrom(fd, iobuf, n, 0, ffi.cast("struct sockaddr *", src), srclen)
  if got < 0 then
    local e = ws2_32.WSAGetLastError()
    if e == M.EAGAIN or e == M.EWOULDBLOCK or e == M.EINTR then
      return nil, "EAGAIN", nil
    end
    return nil, "recvfrom: Windows Socket error " .. tostring(e), nil
  end
  local buf = ffi.new("char[16]")
  local addrPtr = ffi.cast("char *", src) + ffi.offsetof("struct sockaddr_in", "sin_addr")
  ws2_32.inet_ntop(M.AF_INET, addrPtr, buf, 16)
  return ffi.string(iobuf, got), ffi.string(buf), ws2_32.ntohs(src.sin_port)
end

function M.sendto(fd, data, ip, port)
  local sa, salen = makeAddr(ip, port)
  if sa == nil then return nil, salen end
  local n = ws2_32.sendto(fd, data, #data, 0, ffi.cast("struct sockaddr *", sa), salen)
  if n < 0 then
    local e = ws2_32.WSAGetLastError()
    if e == M.EAGAIN or e == M.EWOULDBLOCK or e == M.EINTR then
      return nil, "EAGAIN"
    end
    return nil, "sendto: Windows Socket error " .. tostring(e)
  end
  return tonumber(n), nil
end

function M.close(fd)
  return ws2_32.closesocket(fd) == 0
end

function M.getPort(fd)
  local sa = ffi.new("struct sockaddr_in")
  local salen = ffi.new("int[1]", ffi.sizeof("struct sockaddr_in"))
  if ws2_32.getsockname(fd, ffi.cast("struct sockaddr *", sa), salen) == 0 then
    return ws2_32.ntohs(sa.sin_port)
  end
  return nil
end

return M
