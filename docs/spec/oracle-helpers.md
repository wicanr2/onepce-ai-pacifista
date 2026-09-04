# oracle 助手套件（M4 第三項）

`docs/PLAN.md` M4 列了 `oracle/` 測試助手。第一個客戶 `nectaris-cht/oracle/onepce/` 先把
SAT 解碼與圖塊像素解碼寫在測試檔裡；本 spec 把它們收成公開套件
`github.com/wicanr2/onepce-ai-remake/oracle`，讓每個 `go test` 不必各抄一份 H-030／SAT 公式。

## 1. 目標與範圍

- 目標：只依賴 `Snapshot` 裡的 word 陣列（VRAM／SAT／VCE／VDC regs）就能做的**純解碼**與
  **比對**函式，沒有機器狀態、不跑模擬。
- 不做：畫面渲染（那是 VDC 的事，`Machine.Framebuffer()`）；任何遊戲語意（格子、單位）。

## 2. 證據與等級

| 事實 | 來源 | 等級 |
|---|---|---|
| 圖塊 16 word：word[row] 低位元組 plane0、高位元組 plane1；word[row+8] 低 plane2、高 plane3；bit7 是最左像素 | `vdc-vce.md` §6（H-030） | 已證實 |
| SAT 每 sprite 4 word：Y（10 bit，螢幕 y = 值 − 64）、X（10 bit，螢幕 x = 值 − 32）、pattern（bit0 選 2bpp 平面，>>1 為 cell 編號）、attr（bit0–3 色盤、bit7 前景、bit8 寬 32、bit11 水平翻轉、bit12–13 高 16／32／64、bit15 垂直翻轉） | `vdc-vce.md` §6 | 已證實 |
| sprite cell 64 word：plane0 在 +0、plane1 +16、plane2 +32、plane3 +48，bit15 是最左像素；寬 32 時 cell 編號清 bit0，高 32 清 bit1，高 64 清 bit1–2 | 同上 | 已證實 |
| BAT 每格一個 word：bit0–11 圖塊編號、bit12–15 色盤；寬 32／64／128、高 32／64 由 MWR bit4–6 決定 | `vdc-vce.md` §6 | 已證實 |
| Mesen2 畫面傾印格式與座標 | `framebuffer-parity.md` §2 | 已證實 |

## 3. 介面契約

```go
package oracle

type Sprite struct {
    Index, X, Y, Width, Height int
    Cell                        uint16 // 已套用尺寸遮罩的 cell 編號
    Palette                     uint8
    HFlip, VFlip, Front, SP23   bool
}
func Sprites(sat []uint16) []Sprite                  // 64 筆，依 SAT 順序
func (s Sprite) Visible(w, h int) bool               // 與 w×h 的顯示視窗有交集

func TilePixel(vram []uint16, tile, x, y int) uint8  // 4bpp 圖塊像素（0–15）
func SpritePixel(vram []uint16, s Sprite, x, y int) uint8 // sprite 內 (x,y) 的像素，已含翻轉

type BATEntry struct{ Tile uint16; Palette uint8 }
func BATSize(mwr uint16) (cols, rows int)
func BAT(vram []uint16, mwr uint16) (cols, rows int, entries []BATEntry)

func RGB(c uint16) (r, g, b uint8)                   // 9-bit GRB → 8-bit，線性 ×255/7

type PixelChange struct{ Tile, X, Y int; Before, After uint8 }
func ChangedTilePixels(before, after []uint16, loWord, hiWord int) []PixelChange

type Screen struct{ W, H int; RGB []uint32 }         // 0xRRGGBB
func ReadMesen2Screen(r io.Reader) (*Screen, error)
func Mesen2LeftOverscan(clockDivider int) int        // 84／48／30

type ScreenMatch struct {
    X0, Y0     int // 本機 (0,0) 在 ref 裡的位置
    Compared   int // 兩邊都有的像素數
    Mismatch   int // 不同的像素數（含色彩對照表衝突）
    Colours    int // 對照表大小
}
func MatchScreen(w, h int, native []uint16, ref *Screen, x0, y0 int) ScreenMatch
func SearchScreen(w, h int, native []uint16, ref *Screen, x0, y0, radius int) ScreenMatch // 差異最少者
```

- 所有函式對長度不足的輸入回傳零值／空切片，不 panic；`ReadMesen2Screen` 回 error。
- 這個套件是純函式，`go test` 與 GUI（M6）都能用。

## 4. 驗收條件

- 內部自洽：合成 VRAM／SAT 的單元測試（含翻轉、尺寸遮罩、BAT 尺寸四種、對照表衝突）。
- 對原版核對：`nectaris-cht/oracle/onepce/` 三條測試改用本套件後仍綠；
  `framebuffer-parity.md` §5 的畫面測試用 `MatchScreen` 過。

## 5. 未知與暫停條件

- 2bpp sprite 模式（MWR bit2–3）與 CG 模式的圖塊解碼不在本版；呼叫端要先看 `VDCRegs`。
