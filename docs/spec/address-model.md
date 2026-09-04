# 位址模型與顯示格式

狀態：READY（M0）。來源：`nectaris-cht/docs/pc-engine/hardware-reference.md` H-002／H-003／H-004
（已證實）；`~/.claude/knowledge-base/retro/pce-huc6280-re-toolkit.md`。

## 規則

| # | 規則 | 等級 |
|---|---|---|
| A1 | CPU logical 位址 16-bit；`page = logical >> 13`（0–7）、`offset = logical & 0x1FFF` | 已證實 |
| A2 | `physical = (MPR[page] << 13) \| offset`，21-bit（`$000000–$1FFFFF`） | 已證實 |
| A3 | MPR 可在執行中改變（`TAMi`／`TMAi`），所以任何位址輸出都必須帶當下的 MPR0–7 | 已證實 |
| A4 | ROM file offset 由 mapper 決定：physical bank（`physical >> 13`）落在 ROM 映射範圍才有值，否則 `unknown`；384 KB 這類非 2 的冪 HuCard 的鏡像規則在 `docs/spec/hucard-mapper.md` 另證 | 已證實（規則）／mapper 細節見該 spec |
| A5 | `MPR=$FF` 的 page 是硬體 I/O，不是 ROM；file offset 為 `unknown` | 已證實 |
| A6 | direct page（zero page）位址 `$xx` 實際是 logical `$2000+xx`（MPR1 backing）；stack `$2100–$21FF` | 已證實 |

## 顯示格式（固定一種，所有輸出共用）

```text
L:$6151 P:$28151 F:0x28151 MPR=[FF F8 13 14 01 02 03 00]
```

- `L:` logical，4 位十六進位大寫，`$` 前綴。
- `P:` physical，5 位，`$` 前綴。
- `F:` ROM file offset，`0x` 前綴、5 位以上；無映射時寫 `F:unknown`。
- `MPR=[…]` 八個 2 位十六進位，MPR0 在最左。

## 介面

```go
type MPR [8]uint8
func (m MPR) Physical(logical uint16) uint32
type Address struct{ Logical uint16; Physical uint32; File int64 /* -1 = unknown */; MPR MPR }
func (a Address) String() string
```

`File` 由 `bus` 在產生事件時填；`onepce` 根套件不知道 mapper。

## 驗收

- 單元測試：A1／A2 的換算（含 page 7、offset 邊界）、格式字串逐字元比對、`File=-1` 印 `unknown`。
- 對原版核對：不適用（純算術），但 M1 的 trace 比對會用同一個格式輸出，格式錯會整批對不上。
