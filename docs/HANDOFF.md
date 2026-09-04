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

## 下一個最小動作

1. M3：`docs/spec/observe.md` → watchpoint（區間、read/write/exec、DMA 可見）、事件配額與
   略過計數、區段快照、savestate。
2. M4：CLI（run／screenshot／snapshot／trace／watch）、JSON-RPC、`oracle/` 助手，
   在 `nectaris-cht` 接第一批 `go test`。
