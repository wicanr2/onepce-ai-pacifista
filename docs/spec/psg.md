# HuC6280 PSG（M5）

狀態：READY（第一版：六聲道波表／DDA／雜訊／LFO、VGM 記錄、WAV 渲染；重取樣是零階保持）。

參考行為：Mesen2 `PcePsg.cpp`、`PcePsgChannel.cpp` @ b9fa69d（只取行為事實；結構照本 repo 的
裝置慣例）。Mesen2 自己註明雜訊 LFSR 的規格來自 cgfm2 的實測文章、音量表是 −1.5 dB 階；
本版把它們當**強推論**，由 §6 的 oracle 裁決。`nectaris-cht/docs/spec/psg-audio.md` §5 的
音量模型是同一組公式的假說版，本 spec 以 Mesen2 的整數表為準。

## 1. 目標與範圍

- 目標：CPU 寫 `$0800`–`$0809` 後，PSG 的內部狀態與輸出隨主時脈推進，可（a）當快照區段比對、
  （b）記錄成 VGM、（c）渲染成 WAV。
- 不做：band-limited 重取樣（Mesen2 用 blip_buf；本版取樣瞬間直接取當下輸出）、
  每聲道音量設定、HuC6280（非 A）的輸出偏移切換以外的差異。

## 2. 規則

| 編號 | 規則 | 等級 |
|---|---|---|
| P1 | PSG 時脈 = 主時脈 ÷ 6（3,579,545 Hz）；推進以 PSG clock 為單位，一次推進到「最近一個聲道計時器到期」為止再混音（Mesen2 `Run`）。**推進是懶的**：只在每次埠寫入前、frame 邊界那個週期（frame 結束事件之後）、與跨過 frame 邊界的那條指令結束時推進——雜訊計時器到期時重載而不補償超出的 clock，所以 LFSR 相位取決於這個節奏；逐週期推進會得到不同的相位（實測：只有 noiseLfsr／noiseTimer 對不上） | 已證實（讀碼＋實測） |
| P2 | `$0800` 選聲道（低 3 bit；≥6 的寫入不落到任何聲道）；`$0801` 主音量左（高 4 bit）／右（低 4 bit） | 已證實 |
| P3 | `$0802`／`$0803`：12-bit 週期低 8／高 4；週期 0 當 4096 | 已證實 |
| P4 | `$0804`：bit7 啟用（**啟用邊緣**時計時器重載為週期）、bit6 DDA、bit0–4 振幅。開 DDA 時：聲道啟用則輸出立刻變成 DDA 值−偏移，未啟用則波表位址歸 0 | 已證實 |
| P5 | `$0805`：聲道左（高 4 bit）／右（低 4 bit）音量 | 已證實 |
| P6 | `$0806`：DDA 模式寫 DDA 值（5 bit，啟用時輸出立刻更新）；否則寫波表目前格，**只有聲道未啟用時位址才 +1**，非雜訊模式時輸出立刻更新成該格 | 已證實 |
| P7 | `$0807`（只有聲道 4、5 接受）：bit7 雜訊啟用、bit0–4 雜訊頻率；雜訊週期 = (~f & $1F)×64 PSG clock，f=$1F 時 32 | 強推論 |
| P8 | 雜訊 LFSR 18 bit，reset 值 1；每到期右移一位，新 bit17 = bit0⊕bit1⊕bit11⊕bit12⊕bit17（移位前），輸出 = 移位前 bit0 ? 31 : 0；LFSR **一直跑**，不受啟用位元影響 | 強推論 |
| P9 | 聲道推進：啟用且 DDA → 輸出 = DDA 值−偏移；啟用且非雜訊 → 計時器減，歸 0 時重載週期、波表位址 +1（mod 32），輸出 = 波表值−偏移；啟用且雜訊 → 輸出 = 雜訊輸出−偏移；未啟用 → 0。偏移 = 16（HuC6280A，Mesen2 預設） | 已證實 |
| P10 | LFO（`$0808` 頻率、`$0809` 控制）：控制 bit7 清且低 2 bit 非 0 時生效：聲道 0 的週期 += 聲道 1 目前輸出 << ((ctrl&3−1)×2)（12 bit 截斷）；聲道 1 的週期 ×= LFO 頻率（0 當 256） | 已證實（讀碼） |
| P11 | 音量：衰減階數 = (15−主音量)×2 + (31−振幅) + (15−該側聲道音量)×2；≥30 靜音；輸出 = 聲道輸出 × 表[階數]，表 = {255,214,180,151,127,107,90,76,64,53,45,38,32,27,22,19,16,13,11,9,8,6,5,4,4,3,2,2,2,1}（−1.5 dB 一階） | 強推論 |
| P12 | 混音：六聲道左右各自相加（int16）；取樣：每經過 PSG 時脈 ÷ 取樣率 個 PSG clock 取當下混音值（零階保持） | 決定 |
| P13 | 讀 `$0800`–`$0BFF` 回 I/O 匯流排上次的值（`bus` 既有行為） | 已證實 |
| P14 | oracle 的懶推進：Mesen2 只在 PSG 寫入與每個 frame 結束後推進 PSG，所以它在 frame 結束事件時傾印的計時器／LFSR／輸出，是**上一次 PSG 寫入時**的值。本版每次寫入後另存一份「寫入時的視圖」供比對 | 已證實（讀碼） |

## 3. VGM 記錄（R9）

與 `nectaris-cht/tools/mesen2_pce_psg_vgm_probe.lua` 同一種檔：命令 `0xB9 port data`，每個
frame 邊界一個 `0x62`（等 735 sample），結尾 `0x66`；header：版本 `0x171`、EOF 偏移、
總 sample 數 = frame 數 × 735、rate 60、資料偏移、`0xA4` = 3,579,545。
frame 邊界取 Mesen2 的 `startFrame` 事件時點（掃描線計數歸 0 那一刻，不是 frame 計數器所在的 scanline 256），記錄視窗 `[start, stop)` 的
語意與 probe 相同（寫入在 frame 計數 ∈ [start, stop) 時收，計數進入 (start, stop] 時吐）。
目的是**與 probe 的輸出逐 byte 相同**（§6）。

## 4. WAV

16-bit 立體聲 PCM，取樣率由呼叫端給（預設 44,100）。

## 5. 介面

```go
// internal/psg
type PSG struct{ … }
func New() *PSG
func (p *PSG) Write(port uint8, value uint8)
func (p *PSG) Advance(master uint64)              // 推進到主時脈 master
func (p *PSG) SetSampleRate(rate int)
func (p *PSG) Drain() []int16                     // 交錯立體聲，取走後清空
func (p *PSG) State() State                       // 含 6 個 Channel 的全部欄位
func (p *PSG) LastWriteState() State              // P14
type Recorder struct{ … }                         // VGM
func (r *Recorder) Write(port, value uint8); func (r *Recorder) StartFrame(); func (r *Recorder) Bytes() []byte
func WriteWAV(w io.Writer, rate int, samples []int16) error

// 根套件
func (m *Machine) SetAudioRate(rate int); func (m *Machine) DrainAudio() []int16
func (m *Machine) RecordVGM(start, stop uint64); func (m *Machine) VGM() (data []byte, done bool)
Snapshot.PSG / SectionPSG
// CLI：onepce run -wav out.wav [-audio-rate 44100] -vgm start-stop -vgm-out out.vgm
```

## 6. 驗收

- 內部自洽：LFSR 前 32 個輸出位元對照公式；音量表與階數；週期 0；DDA 立即更新；波表位址只在
  未啟用時前進；LFO 兩種效果；VGM header 欄位；WAV header。
- 對 oracle：
  1. **狀態**：既有 state fixture（frame 2400／2600／3000）裡的 `psg.*` 鍵——暫存器類逐欄相同；
     計時器／LFSR／輸出以 P14 的「寫入時視圖」逐欄相同。
  2. **VGM**：同一路線用 nectaris 的 VGM probe 在 Mesen2 錄 frame 240–2000，與本版 `RecordVGM(240, 2000)`
     逐 byte 相同。
  3. **音訊**：Mednafen `-soundrecord` 的 WAV 與本版 WAV 比對（音高軌跡、RMS 包絡）——門檻：
     逐 1,024-sample 視窗的主頻率一致率 ≥ 95%，RMS 包絡相關係數 ≥ 0.9。**第一版未做**，
     在 §7 記為未完成，不假裝有。

## 7. 結果（2026-09-05）

| 層 | 方法 | 結果 |
|---|---|---|
| 狀態 | 既有 state fixture 的 `psg.*` 287 個鍵（frame 2400／2600／3000），對本版「寫入時視圖」 | **全部相同**（含 6 聲道的 timer、waveAddr、noiseLfsr、noiseTimer、currentOutput、32 格波表） |
| VGM | nectaris 的 VGM probe 在 Mesen2 錄 frame 240–2000（RUN 路線），對 `RecordVGM(240, 2000)` | **逐 byte 相同**：865,543 bytes、287,842 筆埠寫入、1,760 個 frame 區塊 |
| 音訊 | Mednafen 1.29.0 `-sounddriver dummy -soundrecord` 錄開機 22 秒，對 `onepce run -to-frame 1320 -wav`，`tools/oracle/compare_wav.py` | 主頻率一致率 **97.3%**（門檻 95%）、RMS 包絡相關 **0.961**（門檻 0.9）；整體響度比 0.716（Mednafen 增益不同，不列入門檻） |

發現並寫回 spec 的事實：P1 的懶推進（否則只有 noiseLfsr／noiseTimer 對不上）；§3 的 frame
邊界是掃描線歸零而不是 scanline 256；VGM probe 的按鍵計數以 startFrame 為準，等於本機
`Press.Frame` 的 endFrame 計數 **+1**（測試時把路線平移一個 frame）。

未做：每聲道音量設定、HuC6280（非 A）偏移切換；VGM 只有 frame 同步的 `0x62`，沒有
sample 級的 `0x61`。
