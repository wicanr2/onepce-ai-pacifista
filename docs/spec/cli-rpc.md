# CLI 與 JSON-RPC 規格（`cmd/onepce`、`internal/rpc`）

狀態：READY（M4）。需求出處：`docs/PLAN.md` §一（AI 用 CLI 一次跑完腳本化實驗，或用 stdio
JSON-RPC 做互動式 watch／step／dump）、使用者裁定（Go library＋CLI＋JSON-RPC，並配合 `go test`）。

## 原則

- CLI 與 RPC 都是根套件 `onepce` 的薄包裝：**不新增語意**，每個動詞對應一個既有方法。
- 所有輸出帶 ROM SHA-256、模擬器版本、frame（R12）；watch 輸出帶略過筆數（R3）。
- 私人產物（截圖、快照、savestate）只寫到呼叫端指定的路徑。

## CLI

```
onepce run   -rom X [-press "f:btn:span,…"] [-to-frame N] [-load in.state] [-save out.state]
             [-screenshot out.png] [-snapshot-dir DIR] [-watch "kind:space:lo-hi[:limit]"]…
             [-ignore-pc "a,b,…"] [-trace-hash]
onepce rpc   -rom X            JSON-RPC 2.0，每行一個請求／回應（stdin／stdout）
onepce version
```

`run` 的順序：載入（或 `-load`）→ 排程 `-press` → 掛 `-watch` → 跑到 `-to-frame`（預設不跑）
→ 依序輸出：watch 事件（TSV 到 stdout：`kind space source frame scanline hclock pc opcode addr value a x y s p`）、
每個 watch 的 `count/skipped/ignored` 摘要（stderr）、`-trace-hash`、`-screenshot`（PNG，原生解析度）、
`-snapshot-dir`（`snapshot.json` 頭＋`ram.bin`／`vram.bin`／`sat.bin`／`palette.bin`）、`-save`。
`kind` ∈ `read|write|exec`，`space` ∈ `cpu|vram`，位址十六進位。

## JSON-RPC 方法（`internal/rpc`）

| 方法 | 參數 | 回傳 |
|---|---|---|
| `info` | — | `{version, rom_sha256, frame}` |
| `schedule` | `{presses:[{frame,button,span}]}`（button 字串同 CLI） | `{count}` |
| `run_frames` | `{n}` | `{frame}` |
| `run_to_frame` | `{frame}` | `{frame}` |
| `step` | `{n}` | `{frame, pc, cycles}` |
| `registers` | — | `{pc,a,x,y,s,p,cycles,mpr:[…]}` |
| `peek` | `{addr, len}` | `{bytes: hex}` |
| `resolve` | `{addr}` | `{logical, physical, file, mpr, text}` |
| `snapshot` | `{sections:[…]}` | `{frame, hashes:{…}, sections:{name: base64}}` |
| `watch` | `{kind, space, lo, hi, limit, ignore_pc:[…]}` | `{id}` |
| `events` | `{id, max}` | `{events:[…], count, skipped, ignored, remaining}` |
| `unwatch` | `{id}` | `{}` |
| `screenshot` | `{path}` | `{width, height}` |
| `save_state` / `load_state` | `{path}` | `{frame}` |
| `trace_hash` | `{action: start|stop}` | `{sha256, instructions}` |

錯誤回 JSON-RPC error（code −32602 參數錯、−32000 執行錯）；每個回應都帶 `frame`。
事件在伺服器端排隊，`events` 一次最多回 `max` 筆，未取完的留著；佇列上限 100,000 筆，
超過計入 `skipped`。

## 驗收

- 單元：CLI 參數解析（`-press`、`-watch` 格式錯誤要報錯）、RPC 每個方法對合成 ROM 一次往返、
  事件佇列上限。
- 對 oracle：用 CLI 重做 `outline_oracle_test.go` 的路線並輸出事件 TSV，事件數與測試相同。
- `nectaris-cht`：三條 `go test`（開局 SAT 位置、外框像素、單位落點）用根套件跑，在 `NECTARIS_ROM` 下綠。
