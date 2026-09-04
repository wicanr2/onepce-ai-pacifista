# OnePCE AI Remake — 目前狀態

本檔是狀態與 worklist 的單一真相；方法在 `CLAUDE.md`，規劃在 `docs/PLAN.md`。

## 目標

MVP：`docs/PLAN.md` 的 M0–M4——`nectaris-cht` 能用 `onepce` 在 `go test` 裡載入玩家自己的
ROM、走決定性輸入、拿到帶 MPR 的事件與有標籤的快照，並與 remake 對拍。PSG（M5）與對拍
GUI（M6）在 MVP 之後。

## 現況

| 里程碑 | 狀態 |
|---|---|
| M0 骨架 | 完成：`CLAUDE.md`、gate、公開樹測試、位址模型 |
| M1 CPU | 完成（2026-09-04）：`internal/huc6280` 全指令、逐存取計時、逐週期中斷取樣；Nectaris 開機 trace 前 160,000 條與 Mesen2 逐指令相同（`docs/spec/huc6280.md` §9） |
| M2 匯流排＋VDC/VCE | 完成第一版（2026-09-05）：`internal/bus`（384 KB 鏡像、timer、I/O 頁）、`internal/vdc`（scanline 級、實測事件時點）、`internal/vce`、`internal/machine`；REVOLT 路線 frame 2400／2600／3000 的 VRAM／SAT／色盤與 Mesen2 逐 word 相同，RAM 除堆疊頁與兩個計時位元組外相同（`docs/spec/machine.md`） |
| M3 觀測層 | 未開始 |
| M4 CLI／RPC／oracle 助手／nectaris 客戶端 | 未開始 |

## Worklist

| ID | 內容 | 完成條件 |
|---|---|---|
| W1 | M0 收尾 | 完成 |
| W2 | M1 CPU | 完成；殘餘：VRAM 存取 stall 未模擬，trace 對照以 160,000 條為界（`huc6280.md` §9） |
| W3 | M2 匯流排＋VDC/VCE | 完成第一版；殘餘：framebuffer 尚未與 P-159 解幀對照（VRAM 相同，繪製正確性待 M4 的截圖工具目視） |
| W4 | M3：`docs/spec/observe.md`、`state.md` → watchpoint／快照／savestate | 重做 `re/203` 的外框寫入端定位，與 Mesen2 相同 |
| W5 | M4：CLI、JSON-RPC、`oracle/`、`nectaris-cht` 三條 `go test` | 三條測試在 `NECTARIS_ROM` 下綠 |

## 已裁定決策

見 `docs/PLAN.md` §十一（授權路線 B、public、GUI 納入 M6、授權不明測試 ROM 只在本機）。
