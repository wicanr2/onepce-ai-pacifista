# 存檔／讀檔（savestate）規格（`internal/state`）

狀態：READY（M3）。需求出處：`docs/PLAN.md` R11。

## 目標與範圍

把整台機器的狀態存成一個檔，之後讀回來從同一拍繼續，結果與不存檔連續跑相同。用途：
測試 fixture（跳過開機的 2,400 個 frame）、對拍時反覆從同一狀態出發。**不是** PC Engine
原版存檔格式，也不與 Mesen2／Mednafen 的 state 互通。

## 規則

| # | 規則 | 等級 |
|---|---|---|
| S1 | 格式：JSON 頭（版本、ROM SHA-256、模擬器版本、frame、輸入腳本、各區段名稱／長度／SHA-256／偏移）＋ 緊接的二進位區段。頭以一個 `\n` 結束後就是二進位 | 決定 |
| S2 | 版本號整數（目前 2：VDC 存取佇列、逐 word 傳送與水平相位欄位加入後升版）；讀到不同版本直接拒絕，不做轉換 | 決定 |
| S3 | ROM SHA-256 不符直接拒絕 | 決定 |
| S4 | 區段：`cpu`（暫存器、週期數、中斷取樣）、`bus`（MPR、速度、IRQ 遮罩／線、I/O buffer、主時脈、timer）、`ram`、`vdc`（暫存器、VRAM、SAT、所有時序與 latch 狀態、DMA 狀態、framebuffer 尺寸）、`vce`（色盤、位址、控制）、`pad`、`machine`（輸入腳本與釋放時點、lastMaster） | 決定 |
| S5 | 決定性驗收：存檔 → 讀檔 → 跑 N frame 的快照，與不存檔連續跑 N frame 的快照逐 byte 相同 | 決定 |
| S6 | 各元件以匯出的 `State` 結構表示自己的狀態（`Save() State`／`Restore(State)`），編碼用 `encoding/gob`；結構欄位增減即版本升級 | 決定 |

## 驗收

- 單元：S5 對合成 ROM（小程式寫 RAM／VRAM／改 MPR／開 timer）成立；S2／S3 的拒絕；區段雜湊與內容一致。
- 對 oracle：不適用（存檔是 remake 自己的機制）。
