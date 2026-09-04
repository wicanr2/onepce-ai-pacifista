-- Mesen2 headless probe：照 frame 腳本送輸入，在指定 frame 傾印整台機器的狀態，
-- 給 internal/machine 當 M2 的 oracle fixture（docs/spec/vdc-vce.md §10）。
--
-- 環境變數：
--   STATE_PRESS=frame:button:span,…  在該 frame 起按住 button（up/down/left/right/i/ii/select/run）span 個 frame
--   STATE_FRAMES=f1,f2,…             要傾印的 frame（endFrame 計數，與 Mesen 的 frame 計數同義）
--   STATE_STOP_FRAME                 結束 frame（預設最後一個傾印 frame）
--
-- 每個傾印 frame 產生：
--   state-<frame>.tsv       cpu 暫存器、MPR、VDC/VCE state 欄位
--   ram-<frame>.bin         8 KB work RAM（pceWorkRam 或退回 CPU $2000–$3FFF）
--   vram-<frame>.bin        64 KB VRAM（byte-addressed，little-endian word）
--   sat-<frame>.bin         512 bytes sprite RAM（若 API 支援）
--   palette-<frame>.bin     1024 bytes VCE 色盤（little-endian word）
--   screen-<frame>.bin      Mesen2 輸出畫面：一行標頭 "onepce-mesen2-screen-v1\t<w>\t<h>\n"，
--                           之後每像素 3 bytes RGB（列優先）。這是 Mesen2 預設濾鏡的輸出
--                           （PceDefaultVideoFilter：整列同一 clock divider 時寬度為
--                           GetRowWidth(divider)，左緣從 GetLeftOverscan(divider) 個 dot 起，
--                           242 列對應 scanline 14–255；色彩經 Mesen2 的 PCE 色盤表展開）
--   summary.txt
-- 產物是原版執行狀態，屬私人 fixture，不進版控。

local out = emu.getScriptDataFolder()
local frame_count = 0

local plan = {}
for item in string.gmatch(os.getenv("STATE_PRESS") or "", "[^,%s]+") do
  local f, button, span = string.match(item, "^(%d+):(%a+):(%d+)$")
  if f then
    plan[tonumber(f)] = plan[tonumber(f)] or {}
    table.insert(plan[tonumber(f)], { button = string.lower(button), span = tonumber(span) })
  end
end
local dump_frames = {}
local last_dump = 0
for f in string.gmatch(os.getenv("STATE_FRAMES") or "2600", "%d+") do
  dump_frames[tonumber(f)] = true
  if tonumber(f) > last_dump then last_dump = tonumber(f) end
end
local stop_frame = tonumber(os.getenv("STATE_STOP_FRAME") or "") or last_dump

local held = { run = 0, i = 0, ii = 0, select = 0, up = 0, down = 0, left = 0, right = 0 }
local dumped = 0

local function write_bytes(path, memtype, size)
  local file = assert(io.open(path, "wb"))
  local chunk = {}
  for address = 0, size - 1 do
    chunk[#chunk + 1] = string.char(emu.read(address, memtype, false))
    if #chunk == 4096 then
      file:write(table.concat(chunk))
      chunk = {}
    end
  end
  if #chunk > 0 then file:write(table.concat(chunk)) end
  file:close()
end

local function try_memtype(name)
  local ok, mt = pcall(function() return emu.memType[name] end)
  if ok and mt ~= nil then return mt end
  return nil
end

local function dump(frame)
  local st = emu.getState()
  local file = assert(io.open(string.format("%s/state-%d.tsv", out, frame), "w"))
  file:write("schema\tonepce-mesen2-state-v1\n")
  file:write("frame\t" .. frame .. "\n")
  local keys = {}
  for k in pairs(st) do keys[#keys + 1] = k end
  table.sort(keys)
  for _, k in ipairs(keys) do
    local v = st[k]
    if type(v) == "number" then
      file:write(k .. "\t" .. tostring(v) .. "\n")
    elseif type(v) == "boolean" then
      file:write(k .. "\t" .. (v and "1" or "0") .. "\n")
    end
  end
  file:close()

  local work = try_memtype("pceWorkRam")
  if work then
    write_bytes(string.format("%s/ram-%d.bin", out, frame), work, 0x2000)
  else
    local f2 = assert(io.open(string.format("%s/ram-%d.bin", out, frame), "wb"))
    local chunk = {}
    for a = 0x2000, 0x3FFF do chunk[#chunk + 1] = string.char(emu.read(a, emu.memType.pceMemory, false)) end
    f2:write(table.concat(chunk)); f2:close()
  end
  write_bytes(string.format("%s/vram-%d.bin", out, frame), emu.memType.pceVideoRam, 0x10000)
  write_bytes(string.format("%s/palette-%d.bin", out, frame), emu.memType.pcePaletteRam, 0x400)
  local sat = try_memtype("pceSpriteRam")
  if sat then
    write_bytes(string.format("%s/sat-%d.bin", out, frame), sat, 0x200)
  end
  local size = emu.getScreenSize()
  local buf = emu.getScreenBuffer()
  local scr = assert(io.open(string.format("%s/screen-%d.bin", out, frame), "wb"))
  scr:write(string.format("onepce-mesen2-screen-v1\t%d\t%d\n", size.width, size.height))
  local chunk = {}
  for i = 1, size.width * size.height do
    local c = buf[i]
    chunk[#chunk + 1] = string.char((c >> 16) & 0xFF, (c >> 8) & 0xFF, c & 0xFF)
    if #chunk == 4096 then
      scr:write(table.concat(chunk))
      chunk = {}
    end
  end
  if #chunk > 0 then scr:write(table.concat(chunk)) end
  scr:close()
  dumped = dumped + 1
end

emu.addEventCallback(function()
  emu.setInput({
    run = frame_count < held.run, i = frame_count < held.i, ii = frame_count < held.ii,
    select = frame_count < held.select, up = frame_count < held.up, down = frame_count < held.down,
    left = frame_count < held.left, right = frame_count < held.right,
  }, 0)
end, emu.eventType.inputPolled)

emu.addEventCallback(function()
  frame_count = frame_count + 1
  local entries = plan[frame_count]
  if entries then
    for _, e in ipairs(entries) do
      if held[e.button] ~= nil then held[e.button] = frame_count + e.span end
    end
  end
  if dump_frames[frame_count] then
    local ok, err = pcall(dump, frame_count)
    if not ok then
      emu.log("MESEN2_STATE_FAIL frame=" .. frame_count .. " err=" .. tostring(err))
      emu.stop(5)
      return
    end
  end
  if frame_count >= stop_frame then
    local s = assert(io.open(out .. "/summary.txt", "w"))
    s:write(string.format("frames=%d\ndumps=%d\n", frame_count, dumped))
    s:close()
    emu.log("MESEN2_STATE_DONE dumps=" .. dumped)
    emu.stop(0)
  end
end, emu.eventType.endFrame)
