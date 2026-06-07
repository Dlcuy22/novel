-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/json (JSON encode/decode for Novel)
--
-- Purpose:
--   Convert between JSON text and Lua tables/values, pure Lua (no FFI). Pairs
--   naturally with std/http for APIs. Imported with `import std.json`; calls
--   lower to dot-calls (json.encode(v), json.decode(s)).
--
-- Key Components:
--   - encode(value): serialize a Lua value to a JSON string
--   - decode(text): parse JSON text, returns (value, err)
--   - json.null: sentinel for JSON null (distinct from Lua nil, which a table
--     cannot hold as a value)
--
-- Mapping:
--   JSON object  <-> table with string keys
--   JSON array   <-> table with 1..n integer keys (sequence)
--   JSON string  <-> string
--   JSON number  <-> number
--   JSON true/false <-> boolean
--   JSON null    <-> json.null (encode) ; decoded as json.null
--
-- Note:
--   An empty Lua table encodes as [] (an empty array), since the two are
--   indistinguishable in Lua. Pass an explicit field to force object shape.

local json = {}

-- null is the sentinel JSON null. Tables can't store nil as a value, so a
-- decoded null becomes this unique table; encode maps it back to `null`.
json.null = setmetatable({}, { __tostring = function() return "null" end })

-- ----- encode ---------------------------------------------------------------

local escapes = {
  ['"'] = '\\"', ['\\'] = '\\\\', ['\b'] = '\\b', ['\f'] = '\\f',
  ['\n'] = '\\n', ['\r'] = '\\r', ['\t'] = '\\t',
}

local function escapeStr(s)
  return '"' .. s:gsub('[%z\1-\31\\"]', function(c)
    return escapes[c] or string.format('\\u%04x', string.byte(c))
  end) .. '"'
end

-- isArray reports whether t is a (1..n) sequence, so it encodes as a JSON array
-- rather than an object. An empty table is treated as an array.
local function isArray(t)
  local n = 0
  for k in pairs(t) do
    if type(k) ~= "number" then return false end
    n = n + 1
  end
  for i = 1, n do
    if t[i] == nil then return false end
  end
  return true
end

local encodeValue

local function encodeTable(t, out)
  if t == json.null then
    out[#out + 1] = "null"
    return
  end
  if isArray(t) then
    out[#out + 1] = "["
    for i = 1, #t do
      if i > 1 then out[#out + 1] = "," end
      encodeValue(t[i], out)
    end
    out[#out + 1] = "]"
  else
    out[#out + 1] = "{"
    local first = true
    for k, v in pairs(t) do
      if not first then out[#out + 1] = "," end
      first = false
      out[#out + 1] = escapeStr(tostring(k))
      out[#out + 1] = ":"
      encodeValue(v, out)
    end
    out[#out + 1] = "}"
  end
end

function encodeValue(v, out)
  local tv = type(v)
  if v == nil or v == json.null then
    out[#out + 1] = "null"
  elseif tv == "boolean" then
    out[#out + 1] = v and "true" or "false"
  elseif tv == "number" then
    -- %.14g round-trips a double without trailing noise; integers stay integer.
    if v == math.floor(v) and v == v and v ~= math.huge and v ~= -math.huge then
      out[#out + 1] = string.format("%d", v)
    else
      out[#out + 1] = string.format("%.14g", v)
    end
  elseif tv == "string" then
    out[#out + 1] = escapeStr(v)
  elseif tv == "table" then
    encodeTable(v, out)
  else
    error("json.encode: cannot encode a " .. tv, 2)
  end
end

-- encode serializes a Lua value to a JSON string.
function json.encode(value)
  local out = {}
  encodeValue(value, out)
  return table.concat(out)
end

-- ----- decode ---------------------------------------------------------------

-- A tiny recursive-descent parser over the input string with a cursor `pos`.
-- Each parse function advances pos past what it consumed. Errors are raised
-- and caught by json.decode via pcall so callers get (nil, err).

local unescapes = {
  ['"'] = '"', ['\\'] = '\\', ['/'] = '/', ['b'] = '\b',
  ['f'] = '\f', ['n'] = '\n', ['r'] = '\r', ['t'] = '\t',
}

local parseValue

local function skipWS(s, pos)
  local _, e = s:find("^[ \t\r\n]*", pos)
  return (e or pos - 1) + 1
end

local function parseString(s, pos)
  -- pos is at the opening quote.
  local buf = {}
  local i = pos + 1
  while i <= #s do
    local c = s:sub(i, i)
    if c == '"' then
      return table.concat(buf), i + 1
    elseif c == "\\" then
      local nxt = s:sub(i + 1, i + 1)
      if nxt == "u" then
        local hex = s:sub(i + 2, i + 5)
        local code = tonumber(hex, 16)
        if code == nil then error("json: bad \\u escape at " .. i) end
        -- Encode the codepoint as UTF-8 (BMP only; surrogate pairs not joined).
        if code < 0x80 then
          buf[#buf + 1] = string.char(code)
        elseif code < 0x800 then
          buf[#buf + 1] = string.char(0xC0 + math.floor(code / 0x40),
                                      0x80 + (code % 0x40))
        else
          buf[#buf + 1] = string.char(0xE0 + math.floor(code / 0x1000),
                                      0x80 + (math.floor(code / 0x40) % 0x40),
                                      0x80 + (code % 0x40))
        end
        i = i + 6
      else
        local u = unescapes[nxt]
        if u == nil then error("json: bad escape \\" .. nxt .. " at " .. i) end
        buf[#buf + 1] = u
        i = i + 2
      end
    else
      buf[#buf + 1] = c
      i = i + 1
    end
  end
  error("json: unterminated string")
end

local function parseNumber(s, pos)
  local numstr = s:match("^%-?%d+%.?%d*[eE]?[%+%-]?%d*", pos)
  if numstr == nil or numstr == "" then
    error("json: invalid number at " .. pos)
  end
  return tonumber(numstr), pos + #numstr
end

local function parseArray(s, pos)
  local arr = {}
  pos = skipWS(s, pos + 1)
  if s:sub(pos, pos) == "]" then return arr, pos + 1 end
  while true do
    local val
    val, pos = parseValue(s, pos)
    arr[#arr + 1] = val
    pos = skipWS(s, pos)
    local c = s:sub(pos, pos)
    if c == "," then
      pos = skipWS(s, pos + 1)
    elseif c == "]" then
      return arr, pos + 1
    else
      error("json: expected ',' or ']' at " .. pos)
    end
  end
end

local function parseObject(s, pos)
  local obj = {}
  pos = skipWS(s, pos + 1)
  if s:sub(pos, pos) == "}" then return obj, pos + 1 end
  while true do
    pos = skipWS(s, pos)
    if s:sub(pos, pos) ~= '"' then error("json: expected string key at " .. pos) end
    local key
    key, pos = parseString(s, pos)
    pos = skipWS(s, pos)
    if s:sub(pos, pos) ~= ":" then error("json: expected ':' at " .. pos) end
    local val
    val, pos = parseValue(s, skipWS(s, pos + 1))
    obj[key] = val
    pos = skipWS(s, pos)
    local c = s:sub(pos, pos)
    if c == "," then
      pos = pos + 1
    elseif c == "}" then
      return obj, pos + 1
    else
      error("json: expected ',' or '}' at " .. pos)
    end
  end
end

function parseValue(s, pos)
  pos = skipWS(s, pos)
  local c = s:sub(pos, pos)
  if c == "{" then
    return parseObject(s, pos)
  elseif c == "[" then
    return parseArray(s, pos)
  elseif c == '"' then
    return parseString(s, pos)
  elseif c == "t" and s:sub(pos, pos + 3) == "true" then
    return true, pos + 4
  elseif c == "f" and s:sub(pos, pos + 4) == "false" then
    return false, pos + 5
  elseif c == "n" and s:sub(pos, pos + 3) == "null" then
    return json.null, pos + 4
  elseif c == "-" or (c >= "0" and c <= "9") then
    return parseNumber(s, pos)
  end
  error("json: unexpected character '" .. c .. "' at " .. pos)
end

-- decode parses JSON text. Returns (value, nil) on success or (nil, error) on
-- malformed input. Trailing non-whitespace after the value is an error.
function json.decode(text)
  local novel = require("novel")
  local ok, val, pos = pcall(function()
    local v, p = parseValue(text, 1)
    return v, p
  end)
  if not ok then
    return nil, novel.error(tostring(val))
  end
  local rest = skipWS(text, pos)
  if rest <= #text then
    return nil, novel.error("json: trailing data at " .. rest)
  end
  return val, nil
end

return json
