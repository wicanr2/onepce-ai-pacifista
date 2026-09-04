# 最近交接

最後更新：2026-09-05

## Repository

- branch `main`，public，remote `github.com/wicanr2/onepce-ai-remake`。
- 參考副本：`~/cht/tmp/Mesen2`（`b9fa69d`，只取行為事實）、`~/cht/tmp/ares`（`7b51c8a`，結構）。
- 建置 image 暫借 `nectaris-ebiten-test:20260816-v3`。

## 本輪已完成（2026-09-04／05）

- M0：gate、公開樹測試、位址模型。
- M1：`docs/spec/huc6280.md`、`hucard-mapper.md` → `internal/huc6280`（256 opcode 表、
  逐存取計時、T flag、block transfer、TAM/TMA、逐週期中斷取樣）、`internal/bus`。
  `tools/oracle/mesen2_trace_probe.lua` 錄 Nectaris 開機 200,000 條指令；前 160,000 條
  （實際到 165,454）逐指令與 Mesen2 相同。分歧原因是 VRAM 存取 stall 未模擬造成 timer
  相位差，記在 spec §9，測試以 `ONEPCE_TRACE_LIMIT` 為界。
- M2：`docs/spec/vdc-vce.md`、`machine.md` → `internal/vdc`（scanline 級、frame 內事件照實測
  dot 位移、SATB／VRAM DMA、BG＋sprite 繪製）、`internal/vce`、`internal/machine`（逐週期
  推進 VDC、frame 邊界在掃描線 256、`Press` 輸入腳本、`FrameHook`）。
  `tools/oracle/mesen2_state_probe.lua` 在 RUN×6 路線的 frame 2400／2600／3000 傾印；
  VRAM／SAT／色盤逐 word 相同，RAM 除堆疊頁與 `$2E3F–$2E40` 外相同。
- 修過的錯：CPU 中斷在 CLI 後立刻進（應延一條）；timer 以 CPU 週期計（應以 3 主時脈為一步）；
  VDC 上電狀態（應停在 VDS、VDW=239、HDW=$1F）；VDC 只在指令結束推進（應逐週期）。

## 私人輸入（本機，不進 Git）

- ROM：`/tmp/nec-rom/rom.pce`（SHA-256 `7986c694…eab9`）。
- fixture：`dist-all/fixtures/trace/current/`（trace.tsv、samples.tsv）、
  `dist-all/fixtures/state/current/`（state／ram／vram／sat／palette ×3 frame）。
- Mesen2 設定目錄：`/tmp/nectaris-ai-mesen-home`。
- oracle 測試命令（gate 不含）：
  ```bash
  docker run --rm --network none -v "$PWD":/work -w /work -v /tmp/nec-rom:/rom:ro \
    -u "$(id -u):$(id -g)" -e HOME=/tmp -e ONEPCE_ROM=/rom/rom.pce \
    -e ONEPCE_FIXTURES=/work/dist-all/fixtures/trace/current \
    -e ONEPCE_STATE_FIXTURES=/work/dist-all/fixtures/state/current \
    -e ONEPCE_STATE_PRESS="1680:run:15,1815:run:15,1950:run:15,2085:run:15,2220:run:15,2355:run:15" \
    -e ONEPCE_STATE_IGNORE="0E3F,0E40" \
    nectaris-ebiten-test:20260816-v3 go test -count=1 -v ./internal/machine/
  ```

- M3（2026-09-05）：`docs/spec/observe.md`、`state.md` → 根套件 `onepce`（`Load`、`Schedule`、
  `Watch`、`Trace`／`NewTraceHash`、`Snapshot`、`Framebuffer`、`SaveState`／`LoadState`）；
  位址型別搬到 `internal/addr`（根套件別名），bus／VDC／CPU／machine 加觀測鉤子。
  `outline_oracle_test.go`：RUN×6 → right×7 → down×4 → I → I，frame 2500–2800 監看
  `$5480–$7920` 的 VRAM 寫入，2,323 個改寫 word 全部只設位元、寫入端全在 `$A1F5–$A3CF`。

- M4（2026-09-05）：`docs/spec/cli-rpc.md` → `plan.go`（`ParsePresses`／`ButtonByName`）、
  `internal/rpc`（JSON-RPC 2.0，14 個方法，事件佇列上限 100,000）、`cmd/onepce`。
  CLI 實跑：RUN×6 → right×7 → down×4 → I → I 到 frame 2800，`-watch write:vram:5480-7920`
  記 10,000 筆、略過 39,379 筆（預設上限），截圖 320×240 目視為 REVOLT 戰術畫面含白色
  移動範圍格線，trace 雜湊 72,299,794 條指令。
  nectaris 端 `oracle/onepce/`（獨立子模組）三條測試綠：re/048 九個 sprite 全在、re/234 的
  +4 規則對全部單位成立、P-131 的 169 個圖塊 1,215 像素全為 15。
- **MVP（M0–M4）達成。**

- M2 殘餘＋M4 第三項（2026-09-05）：`docs/spec/framebuffer-parity.md`、`oracle-helpers.md` →
  `oracle/`（`Sprites`／`TilePixel`／`SpritePixel`／`BAT`／`ChangedTilePixels`／
  `ReadMesen2Screen`／`MatchScreen`／`SearchScreen`）、`Machine.DisplayWindow`／`ClockDivider`、
  `screen_oracle_test.go`。P-100 路線 frame 2450／2600／2800 的畫面與 Mesen2 在規則算出的位置
  (32, 3) 逐像素相同（各 76,480 像素、43–44 色）。過程中發現 Mesen2 全速模式會略過繪製，
  畫面傾印隨牆鐘拿到舊 frame——harness 固定加 `--pcEngine.disableFrameSkipping=true`，
  兩次執行畫面逐 byte 相同才採用（spec §6.1）。nectaris 端三條測試改用 `oracle` 套件後仍綠。
  畫面 fixture：`dist-all/fixtures/screen/current/`（screen／vram／sat／palette ×3 frame），命令：
  ```bash
  docker run --rm --network none -v "$PWD":/work -w /work -v /tmp/nec-rom:/rom:ro \
    -u "$(id -u):$(id -g)" -e HOME=/tmp -e ONEPCE_ROM=/rom/rom.pce \
    -e ONEPCE_SCREEN_FIXTURES=/work/dist-all/fixtures/screen/current \
    -e ONEPCE_SCREEN_PRESS="1680:run:15,1815:run:15,1950:run:15,2085:run:15,2220:run:15,2355:run:15,2500:right:8,2520:right:8,2540:right:8,2560:right:8,2580:right:8,2600:right:8,2620:right:8,2640:down:8,2660:down:8,2680:down:8,2700:down:8,2720:i:8,2740:i:8" \
    nectaris-ebiten-test:20260816-v3 go test -count=1 -run TestFramebufferMatchesMesen2Picture -v .
  ```

## 下一個最小動作（MVP 之後，各自先寫 spec）

1. VRAM 存取 stall 與 VDC 忙碌旗標（`vdc-vce.md` §8 第一列），目標是 trace 對照穿過 timer 相位。
2. M5 PSG、M6 對拍 GUI（`docs/PLAN.md` §八）。
