-- banner.lua: a global Lua module, installed at $NVLPATH/modules/lua/banner.lua.
-- Imported by bare name ("banner") and resolved at runtime via the store's
-- LUA_PATH. Shows that plain Lua modules drop straight into the store.

local banner = {}

function banner.line(title)
  local bar = string.rep("=", #title + 4)
  return bar .. "\n| " .. title .. " |\n" .. bar
end

return banner
