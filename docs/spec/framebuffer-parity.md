# framebuffer 對原版逐像素（M2 殘餘驗收）

`docs/PLAN.md` M2 的驗收有兩半：VRAM／SAT／VCE 逐 word（`vdc-vce.md` §10 已過）與
「畫面與 P-159 方法對齊」。本 spec 把後者定成可重跑的測試：`Machine.Framebuffer()` 的
每一個像素都要與 Mesen2 在同一 frame 輸出的畫面相同。

## 1. 目標與範圍

- 目標：REVOLT 戰術畫面（含移動範圍外框）在 P-100 路線的 frame 2450／2600／2800，
  本機 framebuffer 與 Mesen2 輸出畫面在顯示視窗內逐像素相同。
- 不做：overscan 區（顯示視窗外）的比對；Mesen2 色彩展開表的重製；4:3 修正。

## 2. 證據與等級

| 事實 | 來源 | 等級 |
|---|---|---|
| Mesen2 的 PCE 輸出緩衝每列 682 個 word，第 r 列是 scanline 14+r，共 242 列；像素 x = hclock ÷ VCE 分頻 | Mesen2 `PceVpc.cpp`、`PceConstants.h` @ b9fa69d | 已證實（讀碼） |
| 預設濾鏡整張畫面同一分頻時，輸出寬 = GetRowWidth(分頻)，左緣從 GetLeftOverscan(分頻) 個 dot 起：/2 → 84、/3 → 48、/4 → 30 | `PceDefaultVideoFilter.h`、`PceConstants.h` | 已證實（讀碼） |
| 輸出像素的 RGB 由 Mesen2 的 512 色表（設定檔）展開；表不在本 repo 重製 | `PceDefaultVideoFilter::InitLookupTable` | 已證實 |
| 顯示視窗的第一個像素在 hclock = displayStart（HSW 結束 + (HDS+1)×8 dot）；第一列是 VDW 的第 0 條 raster | `vdc-vce.md` §4 | 已證實 |

## 3. 介面契約

- `Machine.DisplayWindow() (dot0, line0 int)`：上一張 framebuffer 的左上角在 Mesen2 座標
  系裡的位置——`dot0` = displayStart ÷ 分頻，`line0` = 第一條 VDW raster 的 scanline。
- `Machine.ClockDivider() int`：VCE 目前的分頻（4／3／2）。
- oracle fixture：`tools/oracle/mesen2_state_probe.lua` 多傾印 `screen-<frame>.bin`
  （標頭一行 `onepce-mesen2-screen-v1\t<w>\t<h>`，之後每像素 3 bytes RGB）。
- `oracle.ReadMesen2Screen`、`oracle.MatchScreen`、`oracle.Mesen2LeftOverscan`
  （`oracle-helpers.md`）。

## 4. 比對規則

1. 對齊：本機像素 (x, y) 對應 Mesen2 畫面的 (x + dot0 − LeftOverscan(分頻), y + line0 − 14)。
2. 色彩：本機像素是 9-bit VCE 色；Mesen2 給 RGB。比對時學一張「9-bit → RGB」對照表：
   同一個 9-bit 色在整張畫面裡必須對到同一個 RGB，且不同 9-bit 色不得對到同一個 RGB。
   違反任一條的像素算不相同。這樣比的是畫面內容，不是 Mesen2 的顯示曲線
   （與 `nectaris-cht/tools/frame_parity.py` 同一個理由）。
3. 測試先在規則 1 的位置比，再在 ±4 像素內搜尋；**驗收要求規則 1 的位置就是零差異**，
   搜尋只用來在失敗時報告「差了幾個像素的位移」。

## 5. 驗收條件

- 內部自洽：`oracle` 套件的單元測試（合成畫面、位移、色彩衝突）。
- 對原版核對（`ONEPCE_SCREEN_FIXTURES`／`ONEPCE_SCREEN_PRESS` 開關，沒設就 skip）：
  frame 2450／2600／2800 三張畫面在規則 1 的位置差異像素 = 0，且同一批傾印的
  VRAM／SAT／色盤與本機快照逐 word 相同。
- 結果記在 §6。

## 6. 結果（2026-09-05）

| frame | 本機 | Mesen2 畫面 | 對齊位置 | 比對像素 | 差異 | 對照表 |
|---|---|---|---|---|---|---|
| 2450 | 320×240，分頻 3 | 389×242 | (32, 3)，即規則 1 算出的位置 | 76,480 | 0 | 43 色 |
| 2600 | 同上 | 同上 | 同上 | 76,480 | 0 | 43 色 |
| 2800（移動範圍在畫面上） | 同上 | 同上 | 同上 | 76,480 | 0 | 44 色 |

- 320 個像素（最後一列）在 Mesen2 畫面之外：Nectaris 的顯示視窗從 scanline 17 起 240 列，
  最後一列是 scanline 256，而 Mesen2 只輸出 scanline 14–255。
- 同一批傾印的 VRAM／SAT／色盤與本機快照逐 word 相同。
- 測試：`screen_oracle_test.go`；命令見 `docs/HANDOFF.md`。

## 6.1 取 oracle 畫面時的固定規則

Mesen2 在全速模式（`--testRunner`）下，距上次繪製不到 10 ms 就略過該 frame 的繪製
（`PceVpc::ProcessStartFrame`），`emu.getScreenBuffer()`／`takeScreenshot()` 拿到的是舊 frame
的圖，而且看牆鐘、每次不同。`tools/oracle/mesen2_headless.sh` 因此固定帶
`--pcEngine.disableFrameSkipping=true`；驗收方法是同一腳本跑兩次、畫面逐 byte 相同。
沒有這個開關時記憶體傾印仍正確，只有畫面會錯——「記憶體對、畫面不對」先懷疑這一條。

## 7. 未知與暫停條件

- Mesen2 的 sprite 每列上限與 overflow 行為若與本機不同，會在 sprite 密集的畫面出現差異；
  三張 REVOLT 畫面 sprite 不到 16 cell／列，不會觸發。出現差異先看是否落在 sprite 上。
- 若規則 1 的位置不對而 ±4 內找得到零差異，是 displayStart 或 line0 的定義錯，停下來改
  `vdc-vce.md` §4，不改測試的期望。
