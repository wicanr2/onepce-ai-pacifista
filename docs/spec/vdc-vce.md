# HuC6270 VDC 與 HuC6260 VCE 規格（`internal/vdc`、`internal/vce`）

狀態：READY（M2 第一版：scanline 級、frame 內事件照實測時點排程；VRAM 存取排隊與 CPU stall 見 §5.1）。

## 來源與等級

| 來源 | 用途 | 等級 |
|---|---|---|
| `nectaris-cht/docs/pc-engine/hardware-reference.md` H-008…H-015、H-029…H-032 | 埠、暫存器群、tile／BAT／SAT 形狀、VRAM 寫入序列 | 已證實／強推論（見原文） |
| 公開文件（HuC6270／HuC6260 暫存器表） | 暫存器編號與欄位 | 已證實 |
| Mesen2 `PceVdc.cpp`、`PceVce.cpp`、`PceConstants.h` @ `b9fa69d` | **行為事實**：主時脈、每列 1365 clock、263 列、垂直狀態機計數、frame 內事件的 dot 位移、sprite 每列 16 個、DMA 速率、狀態旗標與清除、VCE 埠 | 已證實（行為）；表達不引用 |

## 1. 時脈與 frame

| # | 規則 | 等級 |
|---|---|---|
| V1 | 主時脈 21,477,270 Hz；每條掃描線 1365 個主時脈；每 frame 262 或 263 條（VCE `$0400` bit2=1 → 263）。上電 262 | 已證實 |
| V2 | VCE clock divider：`$0400` bit1–0 = 0→4（5.37 MHz，256 px）、1→3（7.16 MHz）、2/3→2（10.7 MHz）。1 dot = divider 個主時脈 | 已證實 |
| V3 | CPU 週期→主時脈：高速 ×3、低速 ×12（`huc6280.md` C6） | 已證實 |

## 2. 暫存器（`$0000` 選號、`$0002/$0003` 資料低／高；`ST0/ST1/ST2` 同義）

| 號 | 名 | 欄位（本專案採用） |
|---|---|---|
| 0 | MAWR | VRAM 寫入位址 |
| 1 | MARR | VRAM 讀取位址；**高位元組寫入後立刻預讀一個 word 進 read buffer** |
| 2 | VWR／VRR | 寫：低位元組暫存，高位元組寫入時把 word 寫進 `VRAM[MAWR]` 並 `MAWR += inc`（`$8000` 以上忽略）；讀 `$0002` 回 buffer 低、`$0003` 回 buffer 高，且**只有在選號 = 2 時**讀高位元組後 `MARR += inc` 並再預讀 |
| 5 | CR | 低：bit0 sprite0 碰撞 IRQ、bit1 overflow IRQ、bit2 RCR IRQ、bit3 VBlank IRQ、bit6 sprite 顯示、bit7 BG 顯示（顯示位元在下一條 Vdw 掃描線的 latch 才生效；burst mode = 兩者皆關）。高：bit3–4 位址增量 1／32／64／128 |
| 6 | RCR | 10 bit；當 `RcrCounter == RCR − 64` 時觸發 |
| 7 | BXR | 10 bit，X 捲動；在每條 Vdw 線的 LatchScrollX 事件 latch |
| 8 | BYR | 9 bit，Y 捲動；Vdw 第 0 線 latch 暫存器值，之後每線 +1，中途寫入在下一線生效 |
| 9 | MWR | bit0–1 VRAM 存取模式、bit2–3 sprite 存取模式、bit4–5 BAT 欄數 32/64/128、bit6 BAT 列數 32/64、bit7 CG 模式 |
| A | HSR | 低 5 bit HSW、高 7 bit HDS |
| B | HDR | 低 7 bit HDW、高 7 bit HDE |
| C | VPR | 低 5 bit VSW、高 8 bit VDS |
| D | VDW | 9 bit |
| E | VCR | 低 8 bit |
| F | DCR | bit0 SATB 完成 IRQ、bit1 VRAM DMA 完成 IRQ、bit2 來源遞減、bit3 目的遞減、bit4 每 frame 自動 SATB |
| 10 | SOUR | VRAM DMA 來源 |
| 11 | DESR | VRAM DMA 目的 |
| 12 | LENR | 長度；**高位元組寫入即啟動** |
| 13 | SATB | SAT 來源；高位元組寫入即設 pending，在下一次 VBlank 觸發時開始搬 |

狀態暫存器（讀 `$0000`）：bit6 busy（§5.1 Q9）、bit5 VBlank、bit4 VRAM DMA 完成、bit3 SATB 完成、bit2 RCR、bit1 sprite overflow、bit0 sprite0 碰撞。**讀取後全部清除並放掉 IRQ1**。

## 3. 垂直狀態機（照實測計數）

四個模式循環 `Vsw → Vds → Vdw → Vde`，計數器在每次 RCR 計數（每條線一次，見 §4）時減 1，歸零就換下一個：
`Vsw = VSW+1` 線（進入時 latch VSW/VDS/VDW/VCR、存取模式、BAT 尺寸）；`Vds = VDS+2`；
`Vdw = VDW+1`（進入時 `RcrCounter = 0`、禁止 DMA）；`Vde = VCR`。
掃描線號到 `lines−3` 時**強制**進 `Vsw`（VCE 每 frame 末尾拉 VBLANK 三線）；到 `lines−2` 時若這 frame 還沒發過 VBlank 就補發。
在 `Vde` 且 `RcrCounter == VDW+1` 的那次計數 → 排定 VBlank（在下一個 HDS 觸發點發出）。

## 4. 每條掃描線內的事件（單位：主時脈，`d` = divider）

| 時點 | 事件 |
|---|---|
| 0 | 進 HSW；`hswEnd = 24·d`（d=3 時 32·d） |
| `displayStart = hswEnd + (HDS+1)·8·d` | 進 HDW（顯示區）；BG／sprite 從這裡開始輸出 |
| Vdw 線：`displayStart − 34·d` | LatchScrollY（Y latch／+1、套用顯示開關）；+2·d LatchScrollX；再 +6·d **HdsIrqTrigger** |
| 非 Vdw 線：`displayStart − 25·d` | HdsIrqTrigger |
| `displayStart + ((HDW−1)·8 + 2)·d` | **RCR 計數**：`RcrCounter++`、時脈垂直計數器、比對 RCR、Vde 判 VBlank |
| `displayStart + (HDW+1)·8·d` | 進 HDE，再 `(HDE+1)·8·d` 後進 HSW（`(HSW+1)·8·d` 長）；若線已滿 1365 則直接由 1365 重置 |
| 1365 | 線結束：若 RCR 事件還沒發就在此發；scanline++（到 lines 回 0）；**若此時不在 HSW 就強制進 HSW 並記下同步起點**，已在 HSW 則同步起點不更新（§5.1 Q7 的 8 dot 視窗從同步起點量） |

HdsIrqTrigger：若有排定的 VBlank → 設狀態 bit5（若 CR bit3）＋IRQ1；允許 DMA；pending 或自動的 SATB 開始搬。
若上一線 sprite overflow 且 CR bit1 → 狀態 bit1＋IRQ1。
若事件時點 ≤ 目前 HClock（HDS 很小）就在線開始立即發生。

## 5. DMA

兩種傳送都在 VDC tick（3 主時脈）上逐 word 進行，只在允許 DMA（VBlank／burst）且不在
HSW 前 8 dot（§5.1 Q7，用 `≤`）時推進；SATB 進行中 VRAM DMA 不動。

- SATB：從 VBlank 觸發起每 4 dot 搬一個 word，256 個 word；搬完若 DCR bit0 → 狀態 bit3＋IRQ1。
- VRAM→VRAM：寫 LENR 高位元組即啟動（不允許時原地等），每個 word 讀一拍寫一拍、各 `2·d` 主時脈；
  SOUR／DESR／LENR 隨傳送更新（LENR 減到 `$FFFF` 結束）；目的 `≥$8000` 忽略；完成若 DCR bit1 →
  狀態 bit4＋IRQ1。
- 兩種傳送都會讓 CPU 的 VRAM 存取佇列等待（§5.1 Q4）。

## 5.1 VRAM 存取排隊與 CPU stall

參考行為：Mesen2 `PceVdc.cpp` @ b9fa69d（`QueueMemoryRead/Write`、`ProcessVramAccesses`、
`WaitForVramAccess`、`LoadBackgroundTiles`）。Mesen2 自己標注這組數字是「依測試 ROM 結果的
推測」，所以等級是**強推論**；驗收看 Nectaris 開機 trace 能不能穿過 timer 相位（§10）。

| 規則 | 內容 |
|---|---|
| Q1 排隊 | CPU 對 VRAM 的讀寫不立即發生：寫 `VWR` 高位元組時把 word 排入佇列；寫 `MARR` 高位元組、或選到 `VRR` 時讀 `$0003`，排入一筆讀取。同時最多一筆 |
| Q2 延遲 | 排入後至少等：寫 21／18／12 主時脈、讀 24／24／15 主時脈（分頻 4／3／2）；之後在第一個空槽執行 |
| Q3 tick | 佇列每 3 主時脈檢查一次（VDC tick），檢查用該 tick 開始時的 hclock |
| Q4 空槽（VBlank、burst、或這列沒有 sprite 要抓且不在 BG 取用視窗內） | 偶數 dot 才是空槽；SATB／VRAM DMA 進行中全擋 |
| Q5 空槽（Vdw 且在 BG 取用視窗內） | 視窗 = [displayStart − 16 dot, displayStart + (HDW+1)×8 + 16 dot)，只在 scanline 14–255。取 k = ⌊(hclock − 視窗起點) ÷ 分頻⌋ − 1：k < 0 擋；VRAM 模式 0 → k 偶數才開；模式 1／2 → k mod 8 ∈ {2, 3} 才開；模式 3 全擋 |
| Q6 空槽（Vdw、視窗外、這列有 sprite） | 上一個 BG 視窗結束起算（跨線累計），下一列的 sprite cell 每個占 4／4／8／16 dot（sprite 存取模式 0–3）；抓完之前全擋，之後同 Q4 |
| Q7 HSW | 每線前 8 dot 不服務佇列：以 tick 起點算，`hclock + 3 < 8 dot` 時擋（oracle 量的是「主時脈 − 同步起點」，tick 起點時等於 hclock + 3）；DMA 傳送則是 `hclock + 3 ≤ 8 dot` 時暫停 |
| Q8 stall | 佇列非空時，CPU 再寫 `MAWR`／`MARR`／`VWR` 任一位元組、或在讀取排隊中讀 `$0002`／`$0003`，就以 3 主時脈為單位停住直到佇列清空；stall 期間 timer 與 VDC 照走，CPU 不走 |
| Q9 狀態 | `$0000` bit6 = 佇列非空 |
| Q10 觀測 | watch 事件在排入時發出（帶排入指令的 PC），VRAM 內容在服務時才變；兩者之間 `MAWR` 不可能被改（Q8） |
| Q11 cell 數 | Q6 的 cell 數 = 該列 SAT 掃描到的 16 px cell 數（上限 16），在前一線的 displayStart − 16 dot 起評估，評估的列 = (RcrCounter + 1) mod 總線數（frame 最後一線評估的是第 0 列）；非 Vdw 線（frame 最後一線除外）評估不排程，數為 0。評估開始前，數仍是正在畫的這一列的 |

差異（記在 §8）：Mesen2 在 HDE 提前結束而再次進入 HSW 時也擋 8 dot，本版只擋線首；
Mesen2 在佇列非空時逐 tick 補畫該線，本版仍在 displayStart 一次畫整線。

## 6. 繪製（每條 Vdw 線一次，輸出列號 = `RcrCounter`）

- BG：BAT 索引 `((BgScrollY_latch) >> 3 & (rows−1)) · cols + ((BgScrollX_latch>>3 + 欄) & (cols−1))`；entry 高 4 bit 色盤組、低 12 bit 圖塊號；圖塊 16 word：`[row]` 是 plane0(低)/plane1(高)，`[row+8]` 是 plane2/plane3；像素從 bit7 往右。色 0 透明。
- sprite：SAT 64 筆 × 4 word：Y（10 bit，−64）、X（10 bit，−32）、pattern（`(word & $7FF) >> 1` 為 16×16 cell 號，cell 資料 64 word：4 個 plane 各 16 word）、旗標（bit0–3 色盤、bit7 前景、bit8 寬 32、bit12–13 高 16/32/64、bit11 水平翻轉、bit15 垂直翻轉）。寬 32 的 cell 號清 bit0，高 32 清 bit1，高 64 清 bit1–2。每線最多 16 個 cell，超過設 overflow。
- 合成：BG 非零取 BG；sprite 依 SAT 順序第一個不透明像素，若 BG 為 0 或該 sprite 有前景位元則蓋上去；sprite 0 與其他 sprite 的不透明像素重疊時設 sprite0 碰撞（CR bit0 時發 IRQ）。都透明用 BG 色盤 0 色 0。
- 顯示區外（HDS 前／HDE 後）本版不輸出；framebuffer 尺寸 = `(HDW+1)·8 × (VDW+1)`。

## 7. VCE

`$0400` 控制（V1／V2、bit7 灰階）；`$0402/$0403` 色盤位址（9 bit）；`$0404` 資料低 8 bit；`$0405` 資料 bit8，**寫入後位址 +1**；讀 `$0404/$0405` 同樣自動遞增（讀高位元組時）。色彩 9 bit：bit8–6 G、bit5–3 R、bit2–0 B。色盤 0–255 給 BG、256–511 給 sprite；每組 16 色。

## 8. 刻意差異（本版）

| 差異 | 影響 | 何時補 |
|---|---|---|
| 佇列非空時不逐 tick 補畫；線中再次進入 HSW 時不重新 latch 水平暫存器、不重排事件 | 只影響 HDE 很短、或線中改水平暫存器的畫面設定 | 遇到再補 |
| 顯示區外不輸出、不做 overscan 色 | framebuffer 只有可見區 | 對拍不需要 |
| 中途改 CR 顯示位元／BXR 的同拍 latch 例外 | 極少數遊戲的閃爍 | 不補 |

## 9. 介面

```go
// vdc
func New(vce *vce.VCE, irq IRQLine) *VDC
func (v *VDC) Read(port uint8) uint8 / Write(port uint8, value uint8)   // 給 bus.Device
func (v *VDC) Advance(masterCycles uint64)                              // 由 machine 每條指令後呼叫
func (v *VDC) Frame() uint64; FrameReady() bool                        // 每 frame 一次的旗標
func (v *VDC) VRAM() []uint16; SAT() []uint16; Registers() Registers   // 快照
func (v *VDC) Framebuffer() (w, h int, pixels []uint16 /* VCE 9-bit */)
// vce
func (c *VCE) Palette() []uint16; ClockDivider() int; Lines() int
```

## 10. 驗收

- 單元：垂直狀態機計數（VSW/VDS/VDW/VCR 給定值 → 每線模式序列）、RCR 觸發線、VBlank 觸發時點（主時脈）、VRAM 讀寫序列（H-029）、位址增量四種、BAT 四種尺寸索引、sprite 尺寸／翻轉、每線 16 上限、狀態讀清除。
- 對 oracle：（M1 延伸）Nectaris 開機 trace 穿過 VBlank 等待仍逐指令相同——**已達成**（到第 165,454 條，`huc6280.md` §9）；（M2）REVOLT 戰術畫面 frame N 的 VRAM／SAT／VCE 與 Mesen2 傾印逐 word 相同——**已達成**（frame 2400／2600／3000，`machine.md`）；framebuffer 與 Mesen2 同 frame 的畫面逐像素相同——**已達成**（frame 2450／2600／2800 各 76,480 像素零差異，`framebuffer-parity.md` §6）；（§5.1）Nectaris 整條 P-100 路線 2,800 frame 每千條指令的暫存器與主時脈相同——**已達成**（`machine.md`）。
