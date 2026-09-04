# 整機組裝與時序（`internal/machine`）

狀態：READY（M2）。

## 規則

| # | 規則 | 等級 |
|---|---|---|
| M1 | 一台機器 = HuC6280 ＋ bus（ROM／RAM／I/O／timer）＋ VDC ＋ VCE ＋ 手把。匯流排每 tick（每個 CPU 週期）呼叫 `Clock` 鉤子，VDC 依主時脈差推進；所以指令**中途**讀 VDC 狀態看到的是那一拍的狀態 | 決定（對照證實必要：`huc6280.md` §9） |
| M2 | frame 邊界 = VDC 掃描線到 256 的那一拍（frame 計數器在此 +1）。這與 oracle 的 end-of-frame 回呼、輸入取樣時點相同 | 已證實（Mesen2 行為） |
| M3 | 輸入以 `Press{Frame, Button, Span}` 排程：在 frame 邊界（跨過 scanline 256 的那個 CPU 週期內，與 Mesen2 在 SendFrame 輪詢輸入同點）套用，按住 `Span` 個 frame。同一份腳本可餵 oracle 的 Lua probe（`tools/oracle/mesen2_state_probe.lua` 的 `STATE_PRESS`） | 決定 |
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
| work RAM 8,192 bytes | 三個 frame 全部相同（含堆疊頁與 `$2E3F–$2E40` 這個等待迴圈計數器；`ONEPCE_STATE_IGNORE` 留空即可） |

### 整條路線的指令級對照（同日稍後）

`tools/oracle/mesen2_trace_probe.lua` 以 `TRACE_PRESS`＋`TRACE_LINES=0` 錄整條 P-100 路線
（RUN×6 → right×7 → down×4 → I → I，frame 0–2800），每 1,000 條指令取樣一次
PC／A／X／Y／S／P／MPR／主時脈：

| 範圍 | 取樣 | 結果 |
|---|---|---|
| 第 0–72,178,865 條指令，frame 0–2800 | 72,179 筆 | 全部相同，主時脈零漂移 |

能走到這裡靠：VRAM 存取排隊與 CPU stall、逐 word 的 SATB／VRAM DMA、HSW 同步起點量法、
frame 最後一線的 sprite 評估列（`vdc-vce.md` §5／§5.1）、中斷取樣順序與 P 不存 B
（`huc6280.md` C4／C4a）、按鍵在 frame 邊界週期內套用（M3）。

措辭邊界：這是「同 ROM、同輸入腳本、每千條指令的暫存器與主時脈相同」，逐指令相同只在
另外錄了逐條 trace 的區段（0–600,000、1,700,000–2,600,000）證實。
