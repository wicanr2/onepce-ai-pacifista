-- Mesen2 headless probe：錄開機起前 N 條指令的 PC＋opcode（每 SAMPLE_EVERY 條附完整暫存器與 MPR），
-- 給 internal/huc6280 當 oracle fixture（docs/spec/huc6280.md §8）。
--
-- 輸出（LuaScriptData 目錄）：
--   trace.tsv    每行 "PC\topcode"（十六進位），共 N 行
--   samples.tsv  "index\tpc\ta\tx\ty\tsp\tps\tmpr0..7\tframe"
--   summary.txt  N、被略過筆數、結束 frame
--
-- 環境變數：TRACE_LIMIT（預設 20000）、SAMPLE_EVERY（預設 1000）。
-- 產物含原版執行流程，屬私人 fixture，不進版控。

local LIMIT = tonumber(os.getenv("TRACE_LIMIT") or "20000")
local SAMPLE_EVERY = tonumber(os.getenv("SAMPLE_EVERY") or "1000")
local out_dir = emu.getScriptDataFolder()

local trace = assert(io.open(out_dir .. "/trace.tsv", "w"))
local samples = assert(io.open(out_dir .. "/samples.tsv", "w"))
samples:write("index\tpc\ta\tx\ty\tsp\tps\tmpr\tframe\n")

local count = 0
local frame = 0
local finished = false

local function finish(code)
  if finished then return end
  finished = true
  trace:close()
  samples:close()
  local summary = assert(io.open(out_dir .. "/summary.txt", "w"))
  summary:write(string.format("instructions=%d\nend_frame=%d\n", count, frame))
  summary:close()
  emu.log("MESEN2_TRACE_DONE instructions=" .. count)
  emu.stop(code)
end

emu.addEventCallback(function() frame = frame + 1 end, emu.eventType.endFrame)

emu.addMemoryCallback(function(address, value)
  if count >= LIMIT then
    finish(0)
    return
  end
  local opcode = emu.read(address, emu.memType.pceMemory, false)
  trace:write(string.format("%04X\t%02X\n", address, opcode))
  if count % SAMPLE_EVERY == 0 then
    local st = emu.getState()
    local mpr = {}
    for i = 0, 7 do mpr[#mpr + 1] = string.format("%02X", st["memoryManager.mpr[" .. i .. "]"]) end
    samples:write(string.format("%d\t%04X\t%02X\t%02X\t%02X\t%02X\t%02X\t%s\t%d\n",
      count, st["cpu.pc"], st["cpu.a"], st["cpu.x"], st["cpu.y"], st["cpu.sp"], st["cpu.ps"],
      table.concat(mpr, " "), frame))
  end
  count = count + 1
end, emu.callbackType.exec, 0x0000, 0xFFFF, emu.cpuType.pce, emu.memType.pceMemory)
