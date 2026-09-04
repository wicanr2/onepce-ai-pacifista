# 對拍 GUI（M6）

狀態：READY（第一版）。使用者 2026-09-04 裁定納入：GUI 配合對拍需求。

## 1. 目標與範圍

- 目標：一個 Ebiten 視窗，左格是 onepce 的原生 framebuffer（整數倍縮放），右格是 remake 的
  截圖或錄影幀序列（`nectaris -record-dir` 的 PNG），可並排、疊圖或差異高亮；能暫停、單步、
  逐 frame，熱鍵存快照與截圖；watch 命中即時列出；鍵盤輸入錄成 frame 腳本，headless 可重播。
- 不做：聲音輸出（headless 優先，容器沒有音訊裝置）、除錯器（反組譯、中斷點）、存讀檔 UI。

## 2. 規則

| 編號 | 規則 | 等級 |
|---|---|---|
| G1 | GUI 只呼叫根套件 `onepce` 的公開介面；它自己沒有機器狀態（PLAN M6 驗收） | 決定 |
| G2 | 一個 GUI frame 推進一個機器 frame（暫停時不推進）；單步一條指令用 `Step`；逐 frame 用 `RunFrames(1)` | 決定 |
| G3 | 鍵盤 → 手把：方向鍵、Z＝I、X＝II、Enter＝RUN、右 Shift＝SELECT。按下的鍵在**下一個** frame 邊界生效（`Schedule(Press{Frame: 現在+1, Span: 1})`），連續按住合併成一筆 `Press{Frame, Span}`；退出時可寫成 `frame:button:span,…`，餵回 `-press` 或 Lua probe 得到同一段執行 | 決定 |
| G4 | 右格來源：一個 PNG，或一個目錄裡依檔名排序的 PNG 序列；序列的索引跟著機器 frame 走（`索引 = (frame − 起始 frame) ÷ 每幀間隔`，兩個都是參數） | 決定 |
| G5 | 疊圖／差異：remake 畫布 → 原生座標用 `(scale, offsetX, offsetY)`，預設 (3, 96, 0)（`nectaris-cht/tools/frame_parity.py` 的戰鬥畫面幾何）；取每個原生像素在畫布上的中心點比 RGB，任一通道差 > 8 算不同（兩邊 9-bit→8-bit 的展開取整方式不同） | 決定 |
| G6 | 熱鍵：P 暫停／繼續、N 單步指令、F 逐 frame、C 截圖 PNG、S 快照目錄、1／2／3 = 並排／疊圖／差異、`[`／`]` 手動移動參考序列索引 | 決定 |
| G7 | HUD：frame、scanline、hclock、PC（三空間）、暫停狀態、watch 命中數與最近幾筆 | 決定 |

## 3. 介面

```go
// internal/gui：不依賴 Ebiten 的核心，go test 直接測
type Session struct{ M *onepce.Machine; Paused bool; … }
func New(m *onepce.Machine) *Session
func (s *Session) Tick(held uint8)           // 一個 GUI frame：記錄輸入、視情況推進一個機器 frame
func (s *Session) StepInstruction(); func (s *Session) StepFrame()
func (s *Session) Plan() []onepce.Press       // 排程＋錄到的輸入，合併後
func (s *Session) Watch(spec string) error    // 與 CLI 同語法 kind:space:lo-hi[:limit]
func (s *Session) Hits() []onepce.Event       // 最近 N 筆
type Reference struct{ … }                    // PNG 或序列
func LoadReference(path string, startFrame uint64, every int) (*Reference, error)
func (r *Reference) At(frame uint64) image.Image
func Diff(native *image.RGBA, ref image.Image, scale, ox, oy int) (out *image.RGBA, differing int)

// cmd/onepce-gui：Ebiten 外殼
onepce-gui -rom X [-press …] [-watch …]… [-ref PNG|DIR] [-ref-start N] [-ref-every K]
           [-ref-scale 3] [-ref-offset 96,0] [-scale 2] [-record-plan out.txt]
```

## 4. 驗收

- 內部自洽：`Session` 用合成 ROM：暫停時 `Tick` 不推進；按鍵合併成 `Press`；`Diff` 對合成影像
  算出正確的差異數；序列索引換算。
- **G1 的機械驗收**：同一份腳本，`Session.Tick` 逐 frame 推進到 frame N 的快照雜湊 =
  headless `RunToFrame(N)` 的雜湊（單元測試，合成 ROM）。
- 對 nectaris：載入 `nectaris -record-dir` 的一段 PNG 序列並排顯示——人眼驗收，本版不自動化。
- GUI 外殼本身只在本機開視窗看；容器裡用 `xvfb-run` 只驗證能建置。

## 5. 未知與暫停條件

- 戰術畫面的 remake 畫布幾何（不是戰鬥畫面）沒有量：`-ref-scale`／`-ref-offset` 先手填，
  量到再改預設值。
