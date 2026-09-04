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
| M1 CPU | 完成（2026-09-04）：`internal/huc6280` 全指令、逐存取計時、逐週期中斷取樣；Nectaris 開機 trace 前 600,000 條與 frame 59–94 的 900,000 條與 Mesen2 逐指令相同，整條 P-100 路線 2,800 frame 每千條指令的暫存器與主時脈相同（`docs/spec/huc6280.md` §9） |
| M2 匯流排＋VDC/VCE | 完成第一版（2026-09-05）：`internal/bus`（384 KB 鏡像、timer、I/O 頁）、`internal/vdc`（scanline 級、實測事件時點）、`internal/vce`、`internal/machine`；REVOLT 路線 frame 2400／2600／3000 的 VRAM／SAT／色盤與 Mesen2 逐 word 相同，RAM 除堆疊頁與兩個計時位元組外相同（`docs/spec/machine.md`）；framebuffer 與 Mesen2 畫面在 frame 2450／2600／2800 逐像素相同（`docs/spec/framebuffer-parity.md`） |
| M3 觀測層 | 完成（2026-09-05）：根套件 `onepce.Machine`（Watch 區間／種類／空間、配額與略過計數、忽略清單、Trace 與結構雜湊、區段快照、framebuffer）、savestate（JSON 頭＋gob，S5 決定性測試）；re/203 的外框寫入端以 watch 重現：2,323 個地圖圖塊 word 全為 set-only、寫入端全在 `$A1F5–$A3CF` |
| M4 CLI／RPC／nectaris 客戶端 | 完成（2026-09-05）：`cmd/onepce run`（press／watch／screenshot／snapshot-dir／save／load／trace-hash）與 `rpc`（JSON-RPC 2.0 over stdio，`internal/rpc`）；CLI 走 P-100 路線輸出 10,000 筆事件＋略過 39,379 筆、320×240 截圖目視為戰術畫面含移動範圍；`nectaris-cht/oracle/onepce/` 三條 `go test` 在 `NECTARIS_ROM` 下綠。`oracle/` 套件（SAT／圖塊／BAT 解碼、圖塊像素差異、Mesen2 畫面讀取與比對，`docs/spec/oracle-helpers.md`），nectaris 三條測試改用它後仍綠。**MVP 達成** |
| M5 PSG | 完成（2026-09-05）：`internal/psg`（六聲道、DDA、雜訊、LFO、懶推進）、VGM 記錄、WAV 渲染、快照 PSG 區段、CLI `-wav`／`-vgm`；`docs/spec/psg.md` §7 三層對照全過 |
| M6 對拍 GUI | 完成第一版（2026-09-05）：`internal/gui`（不依賴 Ebiten 的 session、輸入錄製、參考 PNG／序列、差異）＋`cmd/onepce-gui`（Ebiten：並排／疊圖／差異、暫停／單步／逐 frame、截圖／快照、watch 命中）；`docs/spec/gui.md`。G1 由「session 重播 = headless」的單元測試守；容器裡只驗建置與 xvfb 冒煙 |

## Worklist

| ID | 內容 | 完成條件 |
|---|---|---|
| W1 | M0 收尾 | 完成 |
| W2 | M1 CPU | 完成（含中斷取樣順序、P 不存 B） |
| W3 | M2 匯流排＋VDC/VCE | 完成第一版；殘餘：framebuffer 尚未與 P-159 解幀對照（VRAM 相同，繪製正確性待 M4 的截圖工具目視） |
| W4 | M3 觀測層 | 完成；`outline_oracle_test.go` 在 `ONEPCE_ROM` 下綠 |
| W5 | M4 CLI／RPC／nectaris 客戶端 | 完成 |
| W6 | VRAM 存取排隊／CPU stall／逐 word DMA／水平相位（`vdc-vce.md` §5、§5.1） | 完成（2026-09-05）：整條 P-100 路線主時脈零漂移，work RAM 三個 frame 完全相同 |
| W7 | M5 PSG（`docs/spec/psg.md`） | 完成（2026-09-05）：狀態 287 鍵、VGM 逐 byte、音訊主頻率 97.3%／RMS 包絡 0.961 都對上 oracle |
| W8 | M6 對拍 GUI（`docs/spec/gui.md`） | 完成第一版（2026-09-05）；戰術畫面的 remake 畫布幾何未量，`-ref-scale`／`-ref-offset` 手填 |
| W9 | 之後：GUI 的人眼驗收（本機開視窗載 nectaris `-record-dir` 序列）、戰術畫面幾何量測、RPC 的音訊／VGM 方法 | 需要時再做 |

## 已裁定決策

見 `docs/PLAN.md` §十一（授權路線 B、public、GUI 納入 M6、授權不明測試 ROM 只在本機）。
