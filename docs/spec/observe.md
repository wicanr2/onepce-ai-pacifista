# 觀測層規格（`onepce` 根套件：watch、trace、snapshot）

狀態：READY（M3）。需求出處：`docs/PLAN.md` §二 R1–R6、R10、R12。

## 目標與範圍

讓呼叫端（`go test`、CLI、RPC）在不改機器行為的前提下回答：誰在何時寫了哪個位址、
這一拍的機器狀態是什麼、兩次執行的指令序列是否相同。**做**：位址區間 watchpoint（讀／寫／執行、
CPU 空間與 VRAM 空間）、事件配額與略過計數、忽略清單、trace 鉤子、區段快照、framebuffer 輸出。
**不做**：條件式 watch（值比對）、步進除錯 UI——那些由呼叫端用鉤子自己組。

## 規則

| # | 規則 | 等級 |
|---|---|---|
| O1 | 每個事件帶：種類、**指令起點 PC**（不是命中當下的 PC）、opcode、位址（三空間＋MPR，`address-model.md`）、值、frame、scanline、hclock、CPU 週期數、A/X/Y/S/P | 決定（R1／R2） |
| O2 | watch 以區間 `[lo, hi]` 設定；空間有 `cpu`（logical）與 `vram`（word 位址）兩種。`vram` 寫入事件的來源標成 `cpu`／`dma`／`satb`，所以 VDC 自己搬的資料也看得到 | 決定（R6） |
| O3 | 每個 watch 有事件上限（預設 10,000）；超過時**不記錄但計數**，`Skipped()` 回報；呼叫端沒看到略過數不得下「沒有發生」的結論 | 決定（R3） |
| O4 | 忽略清單：以指令起點 PC 為鍵（logical），命中時不記錄也不計入略過（另計 `Ignored()`） | 決定（R4） |
| O5 | exec watch 在指令開始執行前觸發（PC 落在區間） | 決定 |
| O6 | trace 鉤子每條指令呼叫一次，給 `Snapshot`（CPU 暫存器＋MPR）；`TraceHash` 累積「PC＋opcode」的 SHA-256，供結構比對（R10） | 決定 |
| O7 | 區段快照：`RAM`（8 KB）、`VRAM`（32K word）、`SAT`（256 word）、`VCE`（512 word）、`VDCRegs`、`CPU`（含 MPR）、`Timer`、`IO`；每段附 SHA-256；快照另帶 ROM SHA-256、模擬器版本、frame、輸入腳本（R5／R12） | 決定 |
| O8 | framebuffer：VDC 當前顯示區的原生像素（`(HDW+1)·8 × (VDW+1)`），9-bit VCE 色與展開後的 RGBA 兩種；不做 4:3 縮放 | 決定（R8） |
| O9 | 鉤子在機器內部是同步呼叫；鉤子裡不得再驅動機器 | 決定 |

## 介面（根套件 `onepce`）

```go
type Kind uint8            // Read, Write, Exec
type Space uint8           // CPU, VRAM
type Source uint8          // ByCPU, ByDMA, BySATB
type Event struct {
    Kind Kind; Space Space; Source Source
    PC uint16; Opcode uint8; Addr Address; Value uint16
    Frame uint64; Scanline, HClock int; Cycles uint64
    A, X, Y, S, P uint8
}
type Watch struct { ... }  // 由 Machine.Watch 建立
func (m *Machine) Watch(kind Kind, space Space, lo, hi uint32, fn func(Event)) *Watch
func (w *Watch) Limit(n int) *Watch; IgnorePC(pcs ...uint16) *Watch
func (w *Watch) Count() int; Skipped() int; Ignored() int; Remove()
func (m *Machine) Trace(fn func(Snapshot)) func()     // 回傳解除函式
func (m *Machine) Snapshot(sections ...Section) *Snapshot
func (m *Machine) Framebuffer() *image.RGBA
func (m *Machine) FramebufferNative() (w, h int, px []uint16)
```

`Machine`（根套件）包住 `internal/machine`，同時提供 `Load(rom)`、`Press`、`RunFrames`、
`RunToFrame`、`Step`、`Frame()`。呼叫端只需認識 `Machine`、`Event`、`Snapshot`、`Press` 四個概念。

## 驗收

- 單元：區間邊界（lo／hi 皆含）、配額用完後 `Skipped` 遞增、忽略清單不計入略過、exec 事件的 PC
  是指令起點、`vram` 寫入事件的 `Source` 對 CPU 寫入／VRAM DMA／SATB 各一條、trace hash 對同一
  段程式兩次執行相同。
- 對 oracle（`nectaris-cht` `re/203`）：標題 → 戰術 → 選單位 → 「移動」，在移動範圍顯示的那個
  frame 監看 VRAM 寫入；被改寫的圖塊 word 的寫入端 PC 全落在 `$A1F5` 的外框繪製副程式範圍內
  （logical `$A1F5–$A3CF`），且改寫像素的色彩索引全為 15（P-131）。沒有 ROM 就 skip。
