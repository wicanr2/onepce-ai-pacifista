# OnePCE AI Remake — 規劃書

日期：2026-09-04。狀態：**草案，待使用者審閱**；動程式碼前先把本檔的未決項收斂。

## 一、這台模擬器是拿來做什麼的

`nectaris-cht`（PC Engine《太空模擬戰》remake）一路下來，原版 oracle 靠兩套外部模擬器拼湊：
Mesen2 2.1.1 的 headless Lua 給 watchpoint 與 CPU／MPR 取樣，Mednafen 1.29 的 F5 state 給
VRAM／SAT／BaseRAM 區段與畫面。它們能用，但每一個問題都要寫一支新的 Lua、解一次 state 格式、
再用 Python 把兩邊的位址空間對齊；AI 每問一個「誰寫這個位址」都是十幾分鐘的來回。

這個專案要做的是**一台以 Go 寫成、headless 優先、狀態可完整觀測與序列化的 PC Engine 模擬器**，
讓 AI 代理與 remake 的 `go test` 直接把原版當成可查詢的函式庫：

```go
m := onepce.Load(rom)                 // 校驗雜湊、選 mapper
m.Press(2470, onepce.Right, 12)       // 在指定 frame 送輸入
m.Watch(onepce.Write, 0x2f00, 0x2fff, func(ev onepce.Event) { ... }) // 帶完整 MPR 的事件
m.RunTo(2800)
snap := m.Snapshot(onepce.VRAM, onepce.SAT, onepce.VCE, onepce.RAM)  // 有 section 標籤
png := m.Framebuffer()                // 原生 256×224（或當前 VDC 模式），不做 4:3 縮放
```

remake 專案裡的對照測試就能寫成 `go test`：載入玩家自己的 ROM，走一段決定性輸入，
把 SAT 位置、BAT、色盤、PSG 暫存器序列與 remake 的狀態逐欄位比。

**不是**要做給玩家用的通用模擬器：沒有 GUI 的優先順序、沒有 CD-ROM²、不追求逐週期
與真機一致（見第七節的準確度定義）。

## 二、從 Nectaris 專案帶過來的需求（已知事實）

下列每一條都對應一次實際踩過的坑，出處在 `nectaris-cht` 的 `docs/pc-engine/` 與
`~/.claude/knowledge-base/retro/pce-*.md`。

| # | 需求 | 為什麼 |
|---|---|---|
| R1 | 每一筆事件、快照、trace 都帶**完整 MPR0–7**，並同時輸出 CPU logical、physical（21-bit）與 ROM file offset 三種位址 | 同一個 logical 位址在不同時刻對到不同 bank；沒有 MPR 的 PC 沒有意義 |
| R2 | write／read／execute watchpoint 可設**區間**，命中時回報 PC（要能扣回該指令起點）、opcode、operand、值、frame、scanline | Mesen2 的單點 watch 看不到指標間接寫入；靜態 xref 結構上抓不到 |
| R3 | 事件配額用完要**回報被略過的筆數**，不能安靜截斷 | 「沒記到」曾被誤讀成「沒有發生」 |
| R4 | 可排除已知的整段填寫者（palette upload、block transfer `TII`／`TAI`…、work RAM 清空）| 否則每次 watch 都被同一批 memset 淹沒 |
| R5 | 區段快照有 section 標籤：`RAM`（8 KB work RAM）、`VRAM`（64 KB word）、`SAT`、`VCE`（512 色）、`VDC regs`、`CPU`（含 MPR、timer）、`PSG`、`IO` | Mednafen 的 state 有標籤才對得上，Mesen2 沒有 VRAM section dump |
| R6 | VRAM→VRAM DMA、SATB DMA 與 `ST0`／`ST1`／`ST2` 埠寫入要能被 watch 看見 | VDC 自己搬的資料對 CPU watchpoint 隱形 |
| R7 | 輸入以 frame 為單位腳本化，且**同輸入必定同結果**（決定性） | 對拍與回歸都建立在「可重跑」上 |
| R8 | 畫面以原生像素輸出（VDC 當前解析度，不做 4:3 修正），另附 BAT／tile／palette 的結構化讀取 | 模擬器截圖量像素會被非整數縮放騙；對拍要從快照解幀 |
| R9 | PSG：暫存器寫入序列可記錄成 VGM，並能離線渲染 WAV | 推廣片配樂與音源比較都走這條 |
| R10 | trace 可輸出「PC＋opcode 序列」的雜湊，讓兩次執行的結構可比而不要求 raw byte 相同 | Nectaris 的 B/C trace raw SHA 不同、結構相同 |
| R11 | 存檔／讀檔（savestate）為版本化的結構化格式（JSON＋二進位區段），可在 `go test` 裡當 fixture | 現行 Mednafen state 要自己解析 |
| R12 | 所有輸出都附 ROM SHA-256、模擬器版本、frame、輸入序列 | 證據契約（`CLAUDE.md` §5／§6 那一套） |
| R13 | 支援 384 KB 這類非 2 的冪 HuCard（Nectaris 就是），mapping 要有證據不猜 | `393,216 bytes` 只證明大小 |

## 三、參考來源與授權邊界

使用者指示（2026-09-04）：**以 Mesen2 的原始碼為主要實作參考**，本機副本在
`~/cht/tmp/Mesen2`（commit `b9fa69ddc6d0a331fb103fdb5eef6904305703c2`，2026-06-04，GPL-3.0），
PCE 核心在 `Core/PCE/`：

| Mesen2 檔案 | 對應本專案模組 | 拿什麼 |
|---|---|---|
| `PceCpu.cpp`／`PceCpu.Instructions.cpp`／`PceTypes.h` | `internal/huc6280` | 256 opcode 的位址模式與週期矩陣、`T` flag、block transfer、MPR、timer、IRQ 優先序 |
| `PceMemoryManager.cpp`、`IPceMapper.h`、`PceSf2RomMapper.cpp` | `internal/bus` | 21-bit 位址分配、HuCard 鏡像規則、SF2 mapper、I/O 區的 MPR=`$FF` 行為 |
| `PceVdc.cpp`／`PceVdc.h` | `internal/vdc` | 暫存器語意、scanline 狀態機、SAT／VRAM DMA 時序、sprite 限制、狀態旗標 |
| `PceVce.cpp`、`PceVpc.cpp` | `internal/vce` | 色盤格式、dot clock、frame 時序（VPC 只看 SuperGrafx 以外的部分） |
| `PcePsg.cpp`／`PcePsgChannel.cpp` | `internal/psg` | 六聲道波形、噪音、LFO、DDA、音量表 |
| `PceTimer.cpp`、`PceControlManager.cpp` | `internal/huc6280`、`internal/bus` | timer 與手把 I/O |
| `Debugger/` | `internal/observe` | 事件種類與 callback 時機的形狀（不抄 UI） |

輔助來源：Mednafen 1.29 `src/pce_fast/huc6280_ops.inc`（交叉驗證特殊指令）、
Charles MacDonald《PC Engine hardware notes》、archaic pixels 的 HuC6270／HuC6260 文件、
`nectaris-cht/docs/pc-engine/hardware-reference.md`（已分級的事實彙整）。
Mesen2 與 Mednafen 同時也是**執行期 oracle**：同 ROM、同輸入比 trace、state、畫面。

### 授權衝突（未決，動程式碼前要裁）

使用者選了 **RRSAL-1.0**（source-available 專有條款，`LICENSE` 已放好），
同時要求以 Mesen2 的碼為參考。兩者有衝突：

- Mesen2 是 GPL-3.0。**逐函式對照翻成 Go**在著作權上是衍生著作，整個 repo 就必須以
  GPL-3.0 散布，不能掛 RRSAL。
- 只從 Mesen2 取**行為事實**（暫存器語意、週期表、時序順序），用自己的結構獨立寫，
  一般認為不構成衍生著作；但界線要靠紀律維持，而且「看過的人重寫」比「沒看過的人重寫」
  在爭議時更難自證。

兩條路，擇一：

| 路線 | 條款 | 參考方式 | 代價 |
|---|---|---|---|
| A | **GPL-3.0** | 可以逐表逐函式對照 Mesen2，最快到達與 oracle 一致 | 這個 repo 與其他 remake 專案不同條款；`nectaris-cht` 只在 `go test` 裡連結它、發行的遊戲不含它，GPL 不會傳染到遊戲本體 |
| B | RRSAL-1.0 | Mesen2 只取行為事實；每個模組檔頭記「參考了哪個檔的哪一段行為」但不翻碼；ares（ISC）可照結構寫 | 慢，而且爭議時舉證困難 |

本檔的里程碑對兩條路都成立；差別只在 §五各模組「能抄多近」。
**預設建議走 A**：這是工具不是遊戲，GPL 沒有商業上的損失，而且與 Mesen2 對拍時
可以直接引用它的碼當證據。

沒有找到可信賴的 Go 語言 PC Engine 核心可依賴（待查證；就算有，也不會用外部核心，
因為觀測介面是這個專案的重點，外掛核心做不到 R1–R6）。

## 四、專案名稱

`onepce-ai-remake`：`OnePCE` 唸起來就是 One Piece，PCE 藏在裡面；`ai-remake` 說明它是
給 AI 與 remake 對拍用的。Go module path：`github.com/wicanr2/onepce-ai-remake`，
套件根名 `onepce`。GitHub 上 `onepce-ai-remake` 與 `onepce` 都尚未被使用（2026-09-04 查）。

## 五、架構（深模組、窄介面）

```text
cmd/onepce            CLI：run / trace / watch / snapshot / render-audio / rpc
onepce                 對外唯一入口：Machine、Snapshot、Event、Input（第一節那組 API）
  internal/huc6280     CPU 核心：256 opcode、T flag、block transfer、MPR、timer、IRQ、CSL/CSH
  internal/vdc         HuC6270：VRAM、BAT、sprite、SAT DMA、VRAM DMA、scanline 時序、狀態旗標
  internal/vce         HuC6260：512 色、dot clock、frame 時序
  internal/psg         六聲道波形、噪音、LFO、DDA；暫存器記錄（VGM）與離線渲染
  internal/bus         21-bit 實體位址空間：HuCard mapper（含 384 KB／SF2）、work RAM、I/O 區、backup RAM（先不做）
  internal/observe     watchpoint、trace、事件配額、略過計數；所有 hook 都經這裡
  internal/state       版本化 savestate：JSON 頭 ＋ 區段二進位，每區段有名字與雜湊
  internal/rpc         JSON-RPC over stdio：把 onepce 的方法一比一暴露，不另發明語意
  oracle/              給 go test 用的比對助手：載入 Mesen2 trace／Mednafen state 的 fixture 轉檔結果並逐欄位比
```

介面原則（`rulebook/70`）：呼叫端只需要認識 `Machine`、`Snapshot`、`Event`、`Input` 四個概念；
CPU／VDC 內部的時序狀態機不外露。RPC 與 CLI 是 `onepce` 套件的薄包裝，不各自長出語意。

位址顯示格式固定一種（R1）：`L:$6151 P:$0C151 F:0x0C151 MPR=[FF F8 13 14 01 02 03 00]`。

## 六、驗證策略（先有可重跑的 pass/fail，再寫功能）

| 層 | 方法 | fixture 來源 |
|---|---|---|
| CPU 指令 | 每個 opcode 的單元測試：旗標、週期、位址模式、block transfer 的 7-byte 形狀與 `T` flag 行為 | 公開指令表；找得到就加 homebrew test ROM |
| CPU 對 oracle | 同 ROM、同輸入的 Mesen2 trace（PC／opcode／MPR 序列）逐指令比，允許 raw byte 不同但結構雜湊相同（R10） | Nectaris ROM（私人，只存雜湊與轉檔後的摘要，不進 Git） |
| VDC／VCE | Mednafen state 的 `VDC/VRAM`／`SAT`／`VCE` 區段逐 word 比；畫面用 P-159 的方法（VRAM base／stride 對齊）比 | 同上 |
| 時序 | frame 數、scanline 中斷點與 oracle 對齊；先到 frame 級，scanline 級列為里程碑 | 同上 |
| PSG | 暫存器序列與 Mesen2 的 PSG probe 對照；渲染出的 WAV 與 Mednafen `-soundrecord` 比頻譜 | 同上 |
| 決定性 | 同輸入跑兩次，全部 section 雜湊相同（R7） | 內建 |
| 第一個客戶 | `nectaris-cht` 以 `NECTARIS_ROM` 開關的 `go test`：REVOLT 開局 9 個 SAT 位置（`re/048`）、移動範圍外框 87 個像素（`re/203`）、單位落點 +4（`re/234`） | 已有結論的實測值 |

**措辭邊界**照 `nectaris-cht/CLAUDE.md` §3：沒有同 ROM、同輸入、同 frame 的比對，
不得寫「完全一致」；模擬器單元測試全綠只代表內部自洽。

## 七、準確度定義

第一版目標是「**指令級與 oracle 一致、frame 級畫面一致**」：

- CPU：每條指令的結果與旗標一致；週期數對齊到 HuC6280 公開表，用於 timer 與 VDC 時序。
- VDC：以 scanline 為單位更新，同一 frame 結束時 VRAM／SAT／畫面與 oracle 一致；
  mid-scanline 的暫存器變更效果先不追。
- 音訊：暫存器級一致；波形渲染標為 hardware-spec approximation。
- 不追求：真機逐週期、類比輸出特性、CD 系統。

## 八、里程碑

| 里程碑 | 內容 | 驗收 |
|---|---|---|
| M0 骨架 | repo、`LICENSE`、`CLAUDE.md`、Docker image（Go 1.25 鎖版）、gate 命令、位址格式、`onepce` 介面型別（無實作） | gate 綠；`go vet` 過；介面文件與本檔一致 |
| M1 CPU | HuC6280 全指令、MPR、timer、IRQ；trace 輸出 | 單元測試全綠；對 Nectaris 開機前 N 萬條指令與 Mesen2 trace 結構雜湊相同 |
| M2 匯流排＋VDC/VCE | HuCard mapper（含 384 KB）、VRAM、BAT、sprite、DMA、frame 時序；PNG 輸出 | REVOLT 戰術畫面 VRAM／SAT／VCE 與 Mednafen state 逐 word 相同；畫面與 P-159 方法對齊 |
| M3 觀測層 | watchpoint（區間、三種類型、DMA 可見）、事件配額與略過計數、區段快照、savestate | 用它重做 `re/203` 的外框寫入端定位，結果與 Mesen2 相同 |
| M4 介面 | CLI、JSON-RPC over stdio、`oracle/` 測試助手；`nectaris-cht` 接上第一批 `go test` | Nectaris 三條對照測試在 `NECTARIS_ROM` 下綠 |
| M5 PSG | 六聲道、VGM 記錄、WAV 渲染 | 曲號 `$26`／`$29` 的暫存器序列與 Mesen2 probe 相同；WAV 與 Mednafen 頻譜相關係數達門檻（門檻在 spec 定） |
| M6（選）GUI | Ebiten 視窗、即時輸入 | 只在 headless 全過之後 |

每個里程碑先寫 `docs/spec/*.md` 再實作（與 `nectaris-cht` 同一套 spec 先行流程）。

## 九、已知風險與未知

| 項目 | 現況 | 處置 |
|---|---|---|
| 384 KB HuCard 的 bank 鏡像 | Nectaris 專案有實測的 MPR 快照與 ROM offset 換算，但沒有寫成 mapper 規則 | M2 先從 ROM 大小與 oracle 的 MPR 行為推 mapping，記證據等級 |
| HuC6280 未文件化行為（`T` flag、decimal、未定義 opcode）| 公開文件不完整 | 用 oracle trace 裁決，逐條記勘誤 |
| VDC 時序細節（`BXR`／`BYR` latch、sprite overflow、DMA 週期）| 文件間有出入 | 先 frame 級，差異點列成 spec 的未知表 |
| 測試 ROM 的可得性 | 未查 | M1 前查一次；沒有就全靠 oracle fixture |
| oracle fixture 是原版衍生資料 | 不能進 Git | 只存雜湊與轉檔腳本，fixture 放本機 `dist-all/`／`/tmp`，測試無 ROM 時 skip 並明示 |
| ares 版本鎖定 | 未定 | M0 記下參考的 ares commit 與檔案雜湊 |

## 十、與 `nectaris-cht` 的關係

- `nectaris-cht` 是第一個客戶，不是相依：模擬器 repo 不含任何 Nectaris 專屬語意。
- 現行 `pce-mesen-debugger` 子代理的 Mesen2 Lua 後端，在 M3／M4 之後逐步換成 `onepce` 的
  CLI／RPC；Lua probe 保留當交叉驗證，不刪。
- Docker：初期沿用 `nectaris-ebiten-test:20260816-v3`（Go 1.25.12、Xvfb），M0 之後建
  `onepce-dev:<日期>` 自己的 image。

## 十一、待使用者決定

1. **授權路線 A（GPL-3.0）或 B（RRSAL-1.0）**——見第三節。這一項不裁，M1 不能開工。
2. GitHub repo 先建 **private**（與 `nectaris-cht` 相同）；公開時機另議。
3. M6 GUI 要不要進第一版範圍——本檔預設不進。
4. 測試 ROM 若只找到授權不明的，是否寧可不用。
