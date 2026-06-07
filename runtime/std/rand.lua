-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/rand (random number generation for Novel)
--
-- Purpose:
--   Pseudo-random numbers for general use, plus a secure-bytes helper backed by
--   the OS entropy source. Imported with `import "std/rand"`; calls lower to
--   dot-calls (rand.intn(6)).
--
-- Key Components:
--   - seed(n): seed the PRNG deterministically (for reproducible runs)
--   - float(): a float in [0, 1)
--   - intn(n): an integer in [0, n-1]
--   - between(lo, hi): an integer in [lo, hi] inclusive
--   - bytes(n): n cryptographically secure bytes from /dev/urandom (or nil)
--
-- Note:
--   float/intn/between use Lua's math.random (a PRNG): fine for games,
--   sampling, and jitter, NOT for keys or tokens. Use bytes() when you need
--   unpredictable, security-grade randomness.

local r = {}

-- seed makes the PRNG sequence reproducible. Without a call, the sequence is
-- whatever LuaJIT starts with.
function r.seed(n)
  math.randomseed(n)
end

-- float returns a pseudo-random float in [0, 1).
function r.float()
  return math.random()
end

-- intn returns a pseudo-random integer in [0, n-1]. n must be >= 1.
function r.intn(n)
  if n < 1 then return 0 end
  return math.random(0, n - 1)
end

-- between returns a pseudo-random integer in [lo, hi] inclusive.
function r.between(lo, hi)
  if hi < lo then return lo end
  return math.random(lo, hi)
end

local ffi = require("ffi")
local advapi32
if ffi.os == "Windows" then
  ffi.cdef[[
    int CryptAcquireContextA(void **phProv, const char *szContainer, const char *szProvider, unsigned long dwProvType, unsigned long dwFlags);
    int CryptGenRandom(void *hProv, unsigned long dwLen, unsigned char *pbBuffer);
    int CryptReleaseContext(void *hProv, unsigned long dwFlags);
  ]]
  advapi32 = ffi.load("advapi32")
end

local prov = nil

-- bytes returns n cryptographically secure bytes as a string by reading
-- /dev/urandom on POSIX or using CryptGenRandom on Windows.
function r.bytes(n)
  if n <= 0 then return "" end
  if ffi.os == "Windows" then
    if prov == nil then
      local p = ffi.new("void *[1]")
      -- PROV_RSA_FULL = 1, CRYPT_VERIFYCONTEXT = 0xF0000000
      if advapi32.CryptAcquireContextA(p, nil, nil, 1, 0xF0000000) ~= 0 then
        prov = p[0]
      else
        return nil
      end
    end
    local buf = ffi.new("unsigned char[?]", n)
    if advapi32.CryptGenRandom(prov, n, buf) ~= 0 then
      return ffi.string(buf, n)
    end
    return nil
  else
    local f = io.open("/dev/urandom", "rb")
    if f == nil then return nil end
    local b = f:read(n)
    f:close()
    return b
  end
end

return r
