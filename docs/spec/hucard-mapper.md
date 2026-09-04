# 匯流排與 HuCard 映射規格（`internal/bus`）

狀態：READY（M1 最小版：ROM＋work RAM＋I/O stub；M2 接 VDC／VCE）。

## 來源

公開文件（bank 配置、`$F8` work RAM、`$FF` I/O）；Mesen2 `PceMemoryManager.cpp` @ `b9fa69d`
只作行為事實核對（非 2 的冪 ROM 的鏡像規則、未映射 bank 讀值）；Nectaris 實測 MPR 快照
（`nectaris-cht/docs/nectaris-rom/`）作動態裁決。

## 規則

| # | 規則 | 等級 |
|---|---|---|
| B1 | physical 21-bit → bank = `physical >> 13`（0–255）；bank `$00–$7F` 是 HuCard ROM 窗、`$F8–$FB` 是 8 KB work RAM 的四次鏡像、`$FF` 是 I/O 頁、其餘未映射讀 `$FF`、寫忽略 | 已證實 |
| B2 | ROM 大小 = 8 KB × N 個 bank。N 為 2 的冪（≤128）時 bank `i` 讀 ROM bank `i mod N` | 已證實 |
| B3 | **384 KB（N=48）**：bank 以 16 個為一組，八組的 ROM 起始 bank 依序為 `0,16,0,16,32,32,32,32`；即 `$00–$1F` 前 256 KB、`$20–$3F` 再一次前 256 KB、`$40–$7F` 後 128 KB 重複四次 | 強推論（公開慣例＋Mesen2 行為）；**待 Nectaris MPR 實測裁決**（其 MPR 值只用到 `$00–$2F`？要查 `nectaris-rom/rom-map.md`） |
| B4 | 512 KB（N=64）：組起始 `0,16,32,48,32,48,32,48`；768 KB（N=96）：`0,16,32,48,64,80,64,80` | 強推論（同上），非 Nectaris 需求，先實作但標未驗 |
| B5 | ROM file offset = `ROMbank × 0x2000 + (physical & 0x1FFF)`，ROMbank 由 B2–B4 算出；I/O 與 RAM 為 `unknown` | 已證實 |
| B6 | 上電：`MPR7=0`，其餘 MPR 本專案設 0（remake 決定；硬體不定）；CPU 低速；`$1402`=0 | 已證實／決定 |
| B7 | I/O 頁分派見 `docs/spec/huc6280.md` C10／C11 | 已證實 |
| B8 | 沒有 backup RAM（`$F7`）、CD-ROM、SF2 mapper（M2 以後若需要再加，先寫 spec） | 決定 |

## 介面

```go
type Bus struct { ROM []byte; RAM [0x2000]byte; mpr onepce.MPR; ... }
func New(rom []byte, devices Devices) (*Bus, error)   // 校驗大小是 8 KB 倍數；記 SHA-256
func (b *Bus) FileOffset(physical uint32) int64       // B5
```

`Devices` 是 VDC／VCE／PSG／timer／pad 的介面集合；M1 先給 stub（讀 `$FF`、寫忽略），
M2 換成真的。

## 驗收

- 單元：B2 與 B3 的映射表（用 48 個 bank 的假 ROM，每 bank 第一個 byte 寫 bank 號，驗證 128 個
  window 讀到的值）；未映射讀 `$FF`；work RAM 鏡像；`FileOffset` 對 I/O 回 `unknown`。
- 對 oracle：M1 的 trace 比對本身就會驗到 B3（Nectaris 開機會切 MPR）。
