-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/crypto (hashing for Novel)
--
-- Purpose:
--   Cryptographic and non-cryptographic hashing for .nv code. Imported with
--   `import "std/crypto"`; calls lower to dot-calls (crypto.sha256(s)).
--   Built on LuaJIT's bit library for 32-bit word arithmetic.
--
-- Key Components:
--   - sha256(message): hex-encoded SHA-256 digest of a string
--   - fnv1a(message): 32-bit FNV-1a hash as an integer (fast, non-crypto)
--
-- Note:
--   SHA-256 here is a pure-Lua reference implementation, correct but not fast;
--   it is for integrity and fingerprinting, not high-volume hashing. It is
--   verified against the standard test vectors in runtime/test_std.lua. fnv1a
--   is a non-cryptographic hash for hash tables and checksums only.

local bit = require("bit")
local band, bor, bxor, bnot = bit.band, bit.bor, bit.bxor, bit.bnot
local rshift, lshift, ror = bit.rshift, bit.lshift, bit.ror
local tobit = bit.tobit

local crypto = {}

-- SHA-256 round constants (first 32 bits of the fractional parts of the cube
-- roots of the first 64 primes).
local K = {
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

-- toHex8 renders a 32-bit word (possibly signed from bit ops) as 8 lowercase
-- hex digits.
local function toHex8(word)
  -- Mask to the low 32 bits, then format the unsigned value.
  local u = band(word, 0xffffffff)
  if u < 0 then u = u + 4294967296 end
  return string.format("%08x", u)
end

function crypto.sha256(message)
  -- Initial hash values (fractional parts of the square roots of the first 8
  -- primes).
  local h0, h1, h2, h3 = 0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a
  local h4, h5, h6, h7 = 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19

  -- Pre-processing: append 0x80, pad with zeros, then the 64-bit bit length.
  local len = #message
  local bitLen = len * 8
  local padded = message .. string.char(0x80)
  while (#padded % 64) ~= 56 do
    padded = padded .. string.char(0)
  end
  -- 64-bit big-endian length. Lua numbers handle this range exactly; the high
  -- 32 bits are nonzero only for messages over 512 MB, which we do not target.
  for i = 7, 0, -1 do
    local b = math.floor(bitLen / (2 ^ (8 * i))) % 256
    padded = padded .. string.char(b)
  end

  local w = {}
  for chunk = 1, #padded, 64 do
    -- Build the first 16 words (big-endian) from this 64-byte chunk.
    for i = 0, 15 do
      local o = chunk + i * 4
      local b1, b2, b3, b4 = padded:byte(o, o + 3)
      w[i + 1] = bor(lshift(b1, 24), lshift(b2, 16), lshift(b3, 8), b4)
    end
    -- Extend to 64 words.
    for i = 17, 64 do
      local v15 = w[i - 15]
      local v2 = w[i - 2]
      local s0 = bxor(ror(v15, 7), ror(v15, 18), rshift(v15, 3))
      local s1 = bxor(ror(v2, 17), ror(v2, 19), rshift(v2, 10))
      w[i] = tobit(w[i - 16] + s0 + w[i - 7] + s1)
    end

    local a, b, c, d = h0, h1, h2, h3
    local e, f, g, h = h4, h5, h6, h7

    for i = 1, 64 do
      local S1 = bxor(ror(e, 6), ror(e, 11), ror(e, 25))
      local ch = bxor(band(e, f), band(bnot(e), g))
      local t1 = tobit(h + S1 + ch + K[i] + w[i])
      local S0 = bxor(ror(a, 2), ror(a, 13), ror(a, 22))
      local maj = bxor(band(a, b), band(a, c), band(b, c))
      local t2 = tobit(S0 + maj)
      h = g; g = f; f = e
      e = tobit(d + t1)
      d = c; c = b; b = a
      a = tobit(t1 + t2)
    end

    h0 = tobit(h0 + a); h1 = tobit(h1 + b); h2 = tobit(h2 + c); h3 = tobit(h3 + d)
    h4 = tobit(h4 + e); h5 = tobit(h5 + f); h6 = tobit(h6 + g); h7 = tobit(h7 + h)
  end

  return toHex8(h0) .. toHex8(h1) .. toHex8(h2) .. toHex8(h3)
    .. toHex8(h4) .. toHex8(h5) .. toHex8(h6) .. toHex8(h7)
end

-- mul32 multiplies two values modulo 2^32 without losing precision. A direct
-- a * b can exceed 2^53 (where doubles stop being exact) before truncation, so
-- we split a into 16-bit halves and keep every partial product under 2^53.
local function mul32(a, b)
  local au = band(a, 0xffffffff)
  if au < 0 then au = au + 4294967296 end
  local alo = au % 65536
  local ahi = math.floor(au / 65536)
  local lo = alo * b
  local hi = (ahi * b) % 65536
  return tobit(lo + hi * 65536)
end

-- fnv1a is a fast 32-bit non-cryptographic hash. Use it for hash tables and
-- checksums, never for security.
function crypto.fnv1a(message)
  local hash = tobit(0x811c9dc5)
  for i = 1, #message do
    hash = bxor(hash, message:byte(i))
    hash = mul32(hash, 16777619) -- FNV prime, precision-safe 32-bit multiply
  end
  local u = band(hash, 0xffffffff)
  if u < 0 then u = u + 4294967296 end
  return u
end

return crypto
