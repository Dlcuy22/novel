-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/socket_posix (low-level BSD-socket FFI for the Novel stdlib)
--
-- Purpose:
--   Raw, non-yielding socket primitives that std/net layers cooperative async
--   I/O on top of. Wraps the POSIX sockets API.

local ffi = require("ffi")
local posix = require("sys/posix")

local isBSD = (ffi.os == "OSX" or ffi.os == "BSD")

-- struct sockaddr_in differs by platform: macOS/BSD prefix it with a 1-byte
-- sin_len and a 1-byte sin_family; Linux uses a 2-byte sin_family and no
-- sin_len. We declare the matching layout so byte offsets line up.
if isBSD then
  ffi.cdef[[
    struct sockaddr_in {
      uint8_t  sin_len;
      uint8_t  sin_family;
      uint16_t sin_port;
      uint32_t sin_addr;
      char     sin_zero[8];
    };
  ]]
else
  ffi.cdef[[
    struct sockaddr_in {
      uint16_t sin_family;
      uint16_t sin_port;
      uint32_t sin_addr;
      char     sin_zero[8];
    };
  ]]
end

ffi.cdef[[
struct sockaddr { uint16_t sa_family; char sa_data[14]; };

int socket(int domain, int type, int protocol);
int bind(int fd, const struct sockaddr *addr, unsigned int addrlen);
int listen(int fd, int backlog);
int accept(int fd, struct sockaddr *addr, unsigned int *addrlen);
int connect(int fd, const struct sockaddr *addr, unsigned int addrlen);
int setsockopt(int fd, int level, int optname, const void *optval, unsigned int optlen);
long recv(int fd, void *buf, unsigned long len, int flags);
long send(int fd, const void *buf, unsigned long len, int flags);
long recvfrom(int fd, void *buf, unsigned long len, int flags,
              struct sockaddr *src, unsigned int *srclen);
long sendto(int fd, const void *buf, unsigned long len, int flags,
            const struct sockaddr *dst, unsigned int dstlen);

uint16_t htons(uint16_t v);
uint16_t ntohs(uint16_t v);
uint32_t htonl(uint32_t v);
int inet_pton(int af, const char *src, void *dst);
const char *inet_ntop(int af, const void *src, char *dst, unsigned int size);

struct addrinfo {
  int ai_flags; int ai_family; int ai_socktype; int ai_protocol;
  unsigned int ai_addrlen;
  struct sockaddr *ai_addr;   /* order of ai_addr/ai_canonname differs by OS */
  char *ai_canonname;
  struct addrinfo *ai_next;
};
int getaddrinfo(const char *node, const char *service,
                const struct addrinfo *hints, struct addrinfo **res);
void freeaddrinfo(struct addrinfo *res);
const char *gai_strerror(int errcode);
]]

-- ai_addr and ai_canonname are swapped on macOS/BSD relative to Linux; redo the
-- addrinfo declaration in the BSD order so ai_addr points at the right field.
if isBSD then
  ffi.cdef[[
    struct addrinfo {
      int ai_flags; int ai_family; int ai_socktype; int ai_protocol;
      unsigned int ai_addrlen;
      char *ai_canonname;
      struct sockaddr *ai_addr;
      struct addrinfo *ai_next;
    };
  ]]
end

local C = ffi.C

local M = {}

-- Address families / socket types / protocol levels. AF_INET, SOCK_STREAM,
-- SOCK_DGRAM, IPPROTO_* share values across these platforms; SOL_SOCKET and
-- SO_REUSEADDR differ between Linux and BSD.
M.AF_INET      = 2
M.SOCK_STREAM  = 1
M.SOCK_DGRAM   = 2
M.IPPROTO_TCP  = 6
M.IPPROTO_UDP  = 17

local SOL_SOCKET   = 1
local SO_REUSEADDR = 2
if isBSD then
  SOL_SOCKET   = 0xffff
  SO_REUSEADDR = 0x0004
end

-- lastError returns the current errno and its message as a pair.
function M.lastError()
  local e = ffi.errno()
  return e, posix.strerror(e)
end

M.EAGAIN      = posix.EAGAIN
M.EWOULDBLOCK = posix.EWOULDBLOCK
M.EINTR       = posix.EINTR
M.EINPROGRESS = posix.EINPROGRESS

-- makeAddr builds a sockaddr_in for an IPv4 dotted address and port. Returns
-- (sockaddr_in cdata, byte length) or (nil, errmsg) if the address is invalid.
local function makeAddr(ip, port)
  local sa = ffi.new("struct sockaddr_in")
  if isBSD then
    sa.sin_len = ffi.sizeof("struct sockaddr_in")
  end
  sa.sin_family = M.AF_INET
  sa.sin_port = C.htons(port)
  if C.inet_pton(M.AF_INET, ip, ffi.cast("void *", ffi.cast("char *", sa) + ffi.offsetof("struct sockaddr_in", "sin_addr"))) ~= 1 then
    return nil, "invalid IPv4 address: " .. tostring(ip)
  end
  return sa, ffi.sizeof("struct sockaddr_in")
end
M.makeAddr = makeAddr

-- isDottedIPv4 reports whether host already looks like a numeric IPv4 address,
-- so we can skip the (blocking) name resolver.
local function isDottedIPv4(host)
  return host:match("^%d+%.%d+%.%d+%.%d+$") ~= nil
end

-- resolve turns a hostname into a dotted IPv4 string.
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
  local rc = C.getaddrinfo(host, nil, hints, res)
  if rc ~= 0 then
    return nil, "resolve " .. host .. ": " .. ffi.string(C.gai_strerror(rc))
  end
  local ai = res[0]
  local sin = ffi.cast("struct sockaddr_in *", ai.ai_addr)
  local buf = ffi.new("char[16]")
  local addrPtr = ffi.cast("char *", sin) + ffi.offsetof("struct sockaddr_in", "sin_addr")
  C.inet_ntop(M.AF_INET, addrPtr, buf, 16)
  local ip = ffi.string(buf)
  C.freeaddrinfo(ai)
  return ip, nil
end

-- newTCP / newUDP create a non-blocking socket fd, or (nil, errmsg).
local function newSocket(socktype, proto)
  local fd = C.socket(M.AF_INET, socktype, proto)
  if fd < 0 then
    local _, msg = M.lastError()
    return nil, "socket: " .. msg
  end
  local ok, err = posix.setNonblocking(fd)
  if not ok then
    posix.close(fd)
    return nil, "setNonblocking: " .. posix.strerror(err)
  end
  return fd, nil
end

function M.newTCP() return newSocket(M.SOCK_STREAM, M.IPPROTO_TCP) end
function M.newUDP() return newSocket(M.SOCK_DGRAM, M.IPPROTO_UDP) end

-- setReuseAddr lets a listener rebind a recently-closed port.
function M.setReuseAddr(fd)
  local one = ffi.new("int[1]", 1)
  C.setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, one, ffi.sizeof("int"))
end

-- bindListen binds fd to ip:port and starts listening. Returns (true) or
-- (nil, errmsg).
function M.bindListen(fd, ip, port, backlog)
  local sa, salen = makeAddr(ip, port)
  if sa == nil then return nil, salen end
  if C.bind(fd, ffi.cast("struct sockaddr *", sa), salen) ~= 0 then
    local _, msg = M.lastError()
    return nil, "bind: " .. msg
  end
  if C.listen(fd, backlog or 128) ~= 0 then
    local _, msg = M.lastError()
    return nil, "listen: " .. msg
  end
  return true
end

-- bindUDP binds a UDP fd to ip:port. Returns (true) or (nil, errmsg).
function M.bindUDP(fd, ip, port)
  local sa, salen = makeAddr(ip, port)
  if sa == nil then return nil, salen end
  if C.bind(fd, ffi.cast("struct sockaddr *", sa), salen) ~= 0 then
    local _, msg = M.lastError()
    return nil, "bind: " .. msg
  end
  return true
end

-- accept pulls one pending connection off fd's queue.
function M.accept(fd)
  local connfd = C.accept(fd, nil, nil)
  if connfd < 0 then
    local e = ffi.errno()
    if e == M.EAGAIN or e == M.EWOULDBLOCK or e == M.EINTR then
      return nil, "EAGAIN"
    end
    return nil, "accept: " .. posix.strerror(e)
  end
  posix.setNonblocking(connfd)
  return connfd, nil
end

-- connectStart begins a non-blocking connect to ip:port.
function M.connectStart(fd, ip, port)
  local sa, salen = makeAddr(ip, port)
  if sa == nil then return nil, salen end
  if C.connect(fd, ffi.cast("struct sockaddr *", sa), salen) == 0 then
    return true, nil
  end
  local e = ffi.errno()
  if e == M.EINPROGRESS or e == M.EAGAIN or e == M.EINTR then
    return false, nil
  end
  return nil, "connect: " .. posix.strerror(e)
end

local iobuf = ffi.new("char[?]", 65536)

-- recv reads up to n bytes from a connected socket.
function M.recv(fd, n)
  n = n or 4096
  if n > 65536 then n = 65536 end
  local got = C.recv(fd, iobuf, n, 0)
  if got < 0 then
    local e = ffi.errno()
    if e == M.EAGAIN or e == M.EWOULDBLOCK or e == M.EINTR then
      return nil, "EAGAIN"
    end
    return nil, "recv: " .. posix.strerror(e)
  end
  return ffi.string(iobuf, got), nil
end

-- send writes bytes to a connected socket.
function M.send(fd, data)
  local n = C.send(fd, data, #data, 0)
  if n < 0 then
    local e = ffi.errno()
    if e == M.EAGAIN or e == M.EWOULDBLOCK or e == M.EINTR then
      return nil, "EAGAIN"
    end
    return nil, "send: " .. posix.strerror(e)
  end
  return tonumber(n), nil
end

-- recvfrom reads one UDP datagram.
function M.recvfrom(fd, n)
  n = n or 65536
  if n > 65536 then n = 65536 end
  local src = ffi.new("struct sockaddr_in")
  local srclen = ffi.new("unsigned int[1]", ffi.sizeof("struct sockaddr_in"))
  local got = C.recvfrom(fd, iobuf, n, 0, ffi.cast("struct sockaddr *", src), srclen)
  if got < 0 then
    local e = ffi.errno()
    if e == M.EAGAIN or e == M.EWOULDBLOCK or e == M.EINTR then
      return nil, "EAGAIN", nil
    end
    return nil, "recvfrom: " .. posix.strerror(e), nil
  end
  local buf = ffi.new("char[16]")
  local addrPtr = ffi.cast("char *", src) + ffi.offsetof("struct sockaddr_in", "sin_addr")
  C.inet_ntop(M.AF_INET, addrPtr, buf, 16)
  return ffi.string(iobuf, got), ffi.string(buf), C.ntohs(src.sin_port)
end

-- sendto writes one UDP datagram to ip:port.
function M.sendto(fd, data, ip, port)
  local sa, salen = makeAddr(ip, port)
  if sa == nil then return nil, salen end
  local n = C.sendto(fd, data, #data, 0, ffi.cast("struct sockaddr *", sa), salen)
  if n < 0 then
    local e = ffi.errno()
    if e == M.EAGAIN or e == M.EWOULDBLOCK or e == M.EINTR then
      return nil, "EAGAIN"
    end
    return nil, "sendto: " .. posix.strerror(e)
  end
  return tonumber(n), nil
end

-- close closes a socket fd.
function M.close(fd) return posix.close(fd) end

return M
