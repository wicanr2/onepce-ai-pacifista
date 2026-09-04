# OnePCE AI Remake — 目前狀態

本檔是狀態與 worklist 的單一真相；方法在 `CLAUDE.md`，規劃在 `docs/PLAN.md`。

## 目標

MVP：`docs/PLAN.md` 的 M0–M4——`nectaris-cht` 能用 `onepce` 在 `go test` 裡載入玩家自己的
ROM、走決定性輸入、拿到帶 MPR 的事件與有標籤的快照，並與 remake 對拍。PSG（M5）與對拍
GUI（M6）在 MVP 之後。

## 現況

| 里程碑 | 狀態 |
|---|---|
| M0 骨架 | 進行中：`CLAUDE.md`、gate、公開樹測試、位址模型（spec＋程式＋測試）已建 |
| M1 CPU | 未開始 |
| M2 匯流排＋VDC/VCE | 未開始 |
| M3 觀測層 | 未開始 |
| M4 CLI／RPC／oracle 助手／nectaris 客戶端 | 未開始 |

## Worklist

| ID | 內容 | 完成條件 |
|---|---|---|
| W1 | M0 收尾：gate 綠、`CONTEXT`／`HANDOFF` 就位、`docs/spec/hucard-mapper.md` 的規則表 | gate `GATE-OK` |
| W2 | M1：`docs/spec/huc6280.md`（opcode 表、旗標、週期、MPR、timer、IRQ）→ `internal/huc6280` → Mesen2 trace fixture 比對 | 單元測試全綠；Nectaris 開機前 N 萬條指令與 Mesen2 trace 結構雜湊相同 |
| W3 | M2：`docs/spec/bus.md`、`vdc.md`、`vce.md` → 實作 → Mednafen／Mesen2 VRAM 逐 word 比對 | REVOLT 戰術畫面 VRAM／SAT／VCE 相同 |
| W4 | M3：`docs/spec/observe.md`、`state.md` → watchpoint／快照／savestate | 重做 `re/203` 的外框寫入端定位，與 Mesen2 相同 |
| W5 | M4：CLI、JSON-RPC、`oracle/`、`nectaris-cht` 三條 `go test` | 三條測試在 `NECTARIS_ROM` 下綠 |

## 已裁定決策

見 `docs/PLAN.md` §十一（授權路線 B、public、GUI 納入 M6、授權不明測試 ROM 只在本機）。
