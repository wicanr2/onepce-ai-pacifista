# 整機組裝與時序（`internal/machine`）

狀態：READY（M2）。

## 規則

| # | 規則 | 等級 |
|---|---|---|
| M1 | 一台機器 = HuC6280 ＋ bus（ROM／RAM／I/O／timer）＋ VDC ＋ VCE ＋ 手把。匯流排每 tick（每個 CPU 週期）呼叫 `Clock` 鉤子，VDC 依主時脈差推進；所以指令**中途**讀 VDC 狀態看到的是那一拍的狀態 | 決定（對照證實必要：`huc6280.md` §9） |
| M2 | frame 邊界 = VDC 掃描線到 256 的那一拍（frame 計數器在此 +1）。這與 oracle 的 end-of-frame 回呼、輸入取樣時點相同 | 已證實（Mesen2 行為） |
| M3 | 輸入以 `Press{Frame, Button, Span}` 排程：在 frame 邊界套用，按住 `Span` 個 frame。同一份腳本可餵 oracle 的 Lua probe（`tools/oracle/mesen2_state_probe.lua` 的 `STATE_PRESS`） | 決定 |
| M4 | `FrameHook` 在 frame 計數器前進的那個 CPU 週期內被呼叫，供在同一拍抓快照 | 決定 |
| M5 | 手把：SEL（埠 bit0）=1 讀方向、=0 讀 I/II/SELECT/RUN；CLR（bit1）=1 讀 0；按下為 0 | 已證實（Mesen2 行為） |
| M6 | 上電：CPU `Reset`（A/X/Y/S=0，remake 決定）、MPR 全 0、低速、VDC 上電幾何（`vdc-vce.md`）、VCE 262 線／divider 4 | 已證實／決定 |

## 介面

```go
func New(rom []byte) (*Machine, error)
func (m *Machine) Step() int            // 一條指令
func (m *Machine) RunFrame()            // 跑到下一個 frame 邊界並套用輸入腳本
func (m *Machine) RunToFrame(f uint64)
func (m *Machine) Schedule(presses ...Press)
m.FrameHook = func(frame uint64) { ... }
m.CPU / m.Bus / m.VDC / m.VCE / m.Pad   // 快照用
```

## 對照結果（2026-09-05，Nectaris，路線：RUN×6 從標題進戰術畫面）

`internal/machine/state_test.go` 對 Mesen2 在 frame 2400／2600／3000 的傾印：

| 區段 | 結果 |
|---|---|
| VRAM 32,768 words | 三個 frame 全部相同 |
| SAT 256 words | 相同 |
| VCE 色盤 512 words | 相同 |
| work RAM 8,192 bytes | 除堆疊頁（`$2100–$21FF`）與 `$2E3F–$2E40` 外相同；差異 14／19／17 bytes 全在這兩處 |

堆疊頁與 `$2E3F–$2E40` 是**時序**差異：中斷落在不同指令上（VRAM 存取 stall 未模擬，
`vdc-vce.md` §8），前者是舊的返回位址、後者是一個隨等待迴圈次數變動的計數器
（語意未證，只記位址）。frame 邊界時的 PC 與 oracle 相差 0–2 條指令。

措辭邊界：這是「同 ROM、同輸入腳本、同 frame 的記憶體相同」，不是逐週期相同。
