-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/encoding (base64 and hex codecs for Novel)
--
-- Purpose:
--   Binary-to-text encodings used by HTTP, tokens, and binary protocols. Pure
--   Lua, no FFI. Imported with `import std.encoding`; calls lower to dot-calls
--   (encoding.base64Encode(s)).
--
-- Key Components:
--   - base64Encode(data) / base64Decode(text): standard base64 (RFC 4648, with
--     '+' '/' and '=' padding). Decode returns (data, err).
--   - hexEncode(data) / hexDecode(text): lowercase hex. Decode returns
--     (data, err) and rejects odd-length or non-hex input.

local encoding = {}

local B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

-- Reverse lookup for decode, built once.
local B64R = {}
for i = 1, #B64 do
  B64R[B64:sub(i, i)] = i - 1
end

-- base64Encode encodes a byte string to standard base64 with '=' padding.
function encoding.base64Encode(data)
  local out = {}
  local n = #data
  local i = 1
  while i <= n do
    local b1 = data:byte(i)
    local b2 = data:byte(i + 1)
    local b3 = data:byte(i + 2)
    local n1 = math.floor(b1 / 4)
    local n2 = (b1 % 4) * 16 + (b2 and math.floor(b2 / 16) or 0)
    local n3 = b2 and ((b2 % 16) * 4 + (b3 and math.floor(b3 / 64) or 0)) or nil
    local n4 = b3 and (b3 % 64) or nil
    out[#out + 1] = B64:sub(n1 + 1, n1 + 1)
    out[#out + 1] = B64:sub(n2 + 1, n2 + 1)
    out[#out + 1] = n3 and B64:sub(n3 + 1, n3 + 1) or "="
    out[#out + 1] = n4 and B64:sub(n4 + 1, n4 + 1) or "="
    i = i + 3
  end
  return table.concat(out)
end

-- base64Decode decodes standard base64. Whitespace is ignored. Returns
-- (data, nil) or (nil, error) on an invalid character or length.
function encoding.base64Decode(text)
  local novel = require("novel")
  text = text:gsub("[ \t\r\n]", "")
  if #text % 4 ~= 0 then
    return nil, novel.error("base64: input length not a multiple of 4")
  end
  local out = {}
  for i = 1, #text, 4 do
    local c1, c2, c3, c4 = text:sub(i, i), text:sub(i + 1, i + 1),
                            text:sub(i + 2, i + 2), text:sub(i + 3, i + 3)
    local v1, v2 = B64R[c1], B64R[c2]
    if v1 == nil or v2 == nil then
      return nil, novel.error("base64: invalid character")
    end
    out[#out + 1] = string.char(v1 * 4 + math.floor(v2 / 16))
    if c3 ~= "=" then
      local v3 = B64R[c3]
      if v3 == nil then return nil, novel.error("base64: invalid character") end
      out[#out + 1] = string.char((v2 % 16) * 16 + math.floor(v3 / 4))
      if c4 ~= "=" then
        local v4 = B64R[c4]
        if v4 == nil then return nil, novel.error("base64: invalid character") end
        out[#out + 1] = string.char((v3 % 4) * 64 + v4)
      end
    end
  end
  return table.concat(out), nil
end

-- hexEncode encodes bytes to lowercase hex (two chars per byte).
function encoding.hexEncode(data)
  return (data:gsub(".", function(c)
    return string.format("%02x", string.byte(c))
  end))
end

-- hexDecode decodes a lowercase/uppercase hex string. Returns (data, nil) or
-- (nil, error) on odd length or a non-hex digit.
function encoding.hexDecode(text)
  local novel = require("novel")
  if #text % 2 ~= 0 then
    return nil, novel.error("hex: odd-length input")
  end
  if text:match("[^0-9a-fA-F]") then
    return nil, novel.error("hex: invalid digit")
  end
  return (text:gsub("..", function(h)
    return string.char(tonumber(h, 16))
  end)), nil
end

return encoding
