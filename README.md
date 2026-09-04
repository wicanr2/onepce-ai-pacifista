# OnePCE AI Remake

給 AI 代理與 remake 專案對拍用的 PC Engine 模擬器，以 Go 撰寫，headless 優先。

它的價值不在「能玩」，在**狀態可完整觀測、可序列化、可在 `go test` 裡當函式庫呼叫**：
watchpoint 帶完整 MPR、區段快照有標籤、輸入以 frame 腳本化且決定性、畫面以原生像素輸出、
PSG 可記錄成 VGM。第一個客戶是 [`nectaris-cht`](https://github.com/wicanr2/nectaris-cht)。

規劃書：[`docs/PLAN.md`](docs/PLAN.md)；狀態與 worklist：[`CONTEXT.md`](CONTEXT.md)。

## MVP（M0–M4，2026-09-05）

- **CPU 與時序**：HuC6280 全指令、逐存取計時、逐週期中斷取樣；VDC 的 VRAM 存取排隊與 CPU
  stall、逐 word 的 SATB／VRAM DMA。Nectaris 整條 P-100 路線 2,800 frame（7,200 萬條指令）
  每千條指令的暫存器與主時脈都與 Mesen2 相同，逐指令相同的區段見 `docs/spec/huc6280.md` §9。
- **VDC／VCE／匯流排／整機**：scanline 級 VDC，frame 內事件照實測時點排程。REVOLT 路線
  frame 2400／2600／3000 的 VRAM、SAT、色盤與 Mesen2 逐 word 相同（`docs/spec/machine.md`）。
- **觀測**：區間 watch（read／write／exec，CPU 與 VRAM 空間，DMA 可見）、配額與略過計數、
  忽略清單、trace 結構雜湊、區段快照、原生 framebuffer、savestate。
- **介面**：Go library（根套件 `onepce`）、`cmd/onepce`（`run`／`rpc`）、JSON-RPC over stdio。
- **畫面**：REVOLT 戰術畫面（含移動範圍）frame 2450／2600／2800 與 Mesen2 逐像素相同
  （`docs/spec/framebuffer-parity.md`）。
- **PSG**：六聲道波表／DDA／雜訊／LFO，VGM 記錄與 WAV 渲染；PSG 狀態 287 個欄位、VGM
  逐 byte、音訊主頻率與包絡三層都對上 Mesen2／Mednafen（`docs/spec/psg.md` §7）。
- **`oracle` 套件**：SAT／圖塊／BAT 解碼、圖塊像素差異、Mesen2 畫面讀取與比對，純函式，
  給 `go test` 與之後的 GUI 共用（`docs/spec/oracle-helpers.md`）。
- **第一個客戶**：`nectaris-cht/oracle/onepce/` 三條 `go test` 把 re/048、re/175、re/234 的
  實測值改寫成可重跑的測試。

```go
m, _ := onepce.Load(rom)
m.Schedule(onepce.Press{Frame: 1680, Button: onepce.ButtonRun, Span: 15})
w := m.Watch(onepce.Write, onepce.VRAM, 0x5480, 0x7920, func(e onepce.Event) { /* 帶 MPR 的事件 */ })
m.RunToFrame(2800)
snap := m.Snapshot(onepce.SectionVRAM, onepce.SectionSAT)   // 有標籤、有雜湊
img := m.Framebuffer()                                     // 原生 320×240（依 VDC 設定）
fmt.Println(w.Count(), w.Skipped())                        // 略過筆數一定要看
```

```bash
onepce run -rom X.pce -press "1680:run:15,…" -to-frame 2800   -watch write:vram:5480-7920 -screenshot out.png -snapshot-dir snap -save f2800.state -trace-hash
onepce rpc -rom X.pce      # JSON-RPC 2.0，每行一個請求
```

建置與測試都在容器裡（`CLAUDE.md` §3）；oracle 測試以 `ONEPCE_ROM`／`ONEPCE_FIXTURES`／`ONEPCE_SCREEN_FIXTURES`／
`ONEPCE_STATE_FIXTURES` 開關，沒設就 skip。

## 範圍

第一版：HuCard 標準機（HuC6280、HuC6270 VDC、HuC6260 VCE、PSG、兩鍵手把、含 384 KB
這類非 2 的冪 HuCard）。不做 CD-ROM²、SuperGrafx、Arcade Card。GUI 在 headless 全部
通過之後做，定位是對拍輔助（原生畫面、單步、快照、與 remake 畫面並排／疊圖）。

## 授權、致謝與聲明

採 **RRSAL-1.0**（復古重製 source-available 授權條款），全文見 `LICENSE`：非商業用途免費
（含修改與再散布），商業用途請洽 `wicanr2@gmail.com`。

參考來源的邊界：Mesen2（GPL-3.0）只作**行為事實**與執行期 oracle，不翻譯其程式碼；
結構參考 ares（ISC），併入處會在 `NOTICE.md` 保留其版權聲明。細則在 `docs/PLAN.md` 第三節。
本儲存庫不含任何遊戲 ROM、BIOS 或由原版資料重建的素材。
