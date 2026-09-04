-- Mesen2 headless probe：錄開機起前 N 條指令的 PC＋opcode（每 SAMPLE_EVERY 條附完整暫存器與 MPR），
-- 給 internal/huc6280 當 oracle fixture（docs/spec/huc6280.md §8）。
--
-- 輸出（LuaScriptData 目錄）：
--   trace.tsv    每行 "PC\topcode"（十六進位），共 N 行
--   samples.tsv  "index\tpc\ta\tx\ty\tsp\tps\tmpr0..7\tframe\tmasterClock\tcpuCycles"
--   summary.txt  N、被略過筆數、結束 frame
--
-- 環境變數：TRACE_LIMIT（預設 20000，絕對指令序號的上限）、SAMPLE_EVERY（預設 1000）、
--   TRACE_START（預設 0：從第幾條指令起才寫 trace，之前的只計數；summary.txt 記 start=）、
--   TRACE_START_FRAME、TRACE_LINES、TRACE_PRESS、SAMPLE_KEYS（見下方註解）。
-- 產物含原版執行流程，屬私人 fixture，不進版控。

local LIMIT = tonumber(os.getenv("TRACE_LIMIT") or "20000")
local SAMPLE_EVERY = tonumber(os.getenv("SAMPLE_EVERY") or "1000")
local START = tonumber(os.getenv("TRACE_START") or "0")
-- TRACE_START_FRAME：改成從該 frame 結束後的第一條指令起記錄（summary.txt 記 start= 與 start_frame=）
local START_FRAME = tonumber(os.getenv("TRACE_START_FRAME") or "-1")
-- TRACE_LINES=0：不寫 trace.tsv 的逐條記錄，只留 samples.tsv（找長距離漂移用）
local WRITE_LINES = (os.getenv("TRACE_LINES") or "1") ~= "0"
-- TRACE_PRESS=frame:button:span,…：與 mesen2_state_probe.lua 的 STATE_PRESS 同義
local plan = {}
for item in string.gmatch(os.getenv("TRACE_PRESS") or "", "[^,%s]+") do
  local f, button, span = string.match(item, "^(%d+):(%a+):(%d+)$")
  if f then
    plan[tonumber(f)] = plan[tonumber(f)] or {}
    table.insert(plan[tonumber(f)], { button = string.lower(button), span = tonumber(span) })
  end
end
local held = { run = 0, i = 0, ii = 0, select = 0, up = 0, down = 0, left = 0, right = 0 }
-- TRACE_END_FRAME：該 frame 結束就收工（預設不限；LIMIT 仍然有效）
local END_FRAME = tonumber(os.getenv("TRACE_END_FRAME") or "-1")
local recording = START_FRAME <= 0
local start_count = -1
-- SAMPLE_KEYS：逗號分隔的額外 emu.getState() 鍵，每個取樣多寫一欄（例如 vdc.hclock,vdc.scanline）
local EXTRA = {}
for key in string.gmatch(os.getenv("SAMPLE_KEYS") or "", "[^,%s]+") do EXTRA[#EXTRA + 1] = key end
local out_dir = emu.getScriptDataFolder()

local trace = assert(io.open(out_dir .. "/trace.tsv", "w"))
local samples = assert(io.open(out_dir .. "/samples.tsv", "w"))
samples:write("index\tpc\ta\tx\ty\tsp\tps\tmpr\tframe\tmasterClock\tcpuCycles" .. (#EXTRA > 0 and ("\t" .. table.concat(EXTRA, "\t")) or "") .. "\n")

local count = 0
local frame = 0
local finished = false

local function finish(code)
  if finished then return end
  finished = true
  trace:close()
  samples:close()
  local summary = assert(io.open(out_dir .. "/summary.txt", "w"))
  summary:write(string.format("instructions=%d\nend_frame=%d\nstart=%d\nstart_frame=%d\n", count, frame,
    start_count >= 0 and start_count or START, START_FRAME))
  summary:close()
  emu.log("MESEN2_TRACE_DONE instructions=" .. count)
  emu.stop(code)
end

emu.addEventCallback(function()
  emu.setInput({
    run = frame < held.run, i = frame < held.i, ii = frame < held.ii,
    select = frame < held.select, up = frame < held.up, down = frame < held.down,
    left = frame < held.left, right = frame < held.right,
  }, 0)
end, emu.eventType.inputPolled)

emu.addEventCallback(function()
  frame = frame + 1
  local entries = plan[frame]
  if entries then
    for _, e in ipairs(entries) do
      if held[e.button] ~= nil then held[e.button] = frame + e.span end
    end
  end
  if frame == START_FRAME then recording = true end
  if END_FRAME >= 0 and frame >= END_FRAME then finish(0) end
end, emu.eventType.endFrame)

emu.addMemoryCallback(function(address, value)
  if count >= LIMIT then
    finish(0)
    return
  end
  if count < START or not recording then
    count = count + 1
    return
  end
  if start_count < 0 then start_count = count end
  if WRITE_LINES then
    local opcode = emu.read(address, emu.memType.pceMemory, false)
    trace:write(string.format("%04X\t%02X\n", address, opcode))
  end
  if count % SAMPLE_EVERY == 0 then
    local st = emu.getState()
    local mpr = {}
    for i = 0, 7 do mpr[#mpr + 1] = string.format("%02X", st["memoryManager.mpr[" .. i .. "]"]) end
    samples:write(string.format("%d\t%04X\t%02X\t%02X\t%02X\t%02X\t%02X\t%s\t%d\t%s\t%s\n",
      count, st["cpu.pc"], st["cpu.a"], st["cpu.x"], st["cpu.y"], st["cpu.sp"], st["cpu.ps"],
      table.concat(mpr, " "), frame, tostring(st["masterClock"]), tostring(st["cpu.cycleCount"])))
    for _, key in ipairs(EXTRA) do
      local v = st[key]
      if type(v) == "boolean" then v = v and 1 or 0 end
      samples:seek("end")
    end
    if #EXTRA > 0 then
      -- 補在同一行尾：先移除剛寫的換行再接上
      samples:seek("cur", -1)
      local cols = {}
      for _, key in ipairs(EXTRA) do
        local v = st[key]
        if type(v) == "boolean" then v = v and 1 or 0 end
        cols[#cols + 1] = tostring(v)
      end
      samples:write("\t" .. table.concat(cols, "\t") .. "\n")
    end
  end
  count = count + 1
end, emu.callbackType.exec, 0x0000, 0xFFFF, emu.cpuType.pce, emu.memType.pceMemory)
