# OnePCE AI Pacifista — 工作契約

給 AI 與 remake 對拍用的 PC Engine 模擬器（Go）。規劃在 `docs/PLAN.md`，目前狀態與
worklist 在 `CONTEXT.md`，每輪交接在 `docs/HANDOFF.md`。本檔只寫工作方法。

## 1. 固定順序

1. 讀 `CONTEXT.md` 的現況與下一個最小動作，再讀 `docs/HANDOFF.md`。
2. `git config user.email` 必須是 `wicanr2@gmail.com`；`git status --short` 乾淨才動手。
3. **[HARD] spec 先行**：每個模組動程式碼前先在 `docs/spec/` 寫或更新規格：規則表、
   來源（公開文件／Mesen2 檔名與 commit／ares 檔名與 commit）、證據等級、驗收條件。
   規格與實作不一致時以實作為準先修規格。
4. 每輪結束更新 `CONTEXT.md` 的 worklist 與 `docs/HANDOFF.md`，跑 §3 的 gate。

## 2. 硬紅線

- **[HARD] 授權路線 B**（`docs/PLAN.md` §三）：repo 是 RRSAL-1.0，**public**。
  Mesen2（GPL-3.0，`~/cht/tmp/Mesen2` @ `b9fa69d`）只取行為事實：暫存器語意、週期表、
  旗標規則、時序順序。**不對照它的函式逐行寫、不複製函式切分／變數名／註解／表格排列。**
  結構參考 ares（ISC，`~/cht/tmp/ares` @ `7b51c8a`），併入處在 `NOTICE.md` 保留版權聲明。
  每個模組檔頭記 `參考行為：Mesen2 <檔> @ b9fa69d §<主題>` 或 `參考結構：ares <檔> @ 7b51c8a`。
  判準：拿掉 Mesen2 就寫不出來的碼就是翻的，那就不能寫。
- **[HARD] 私人輸入不進版控**：任何遊戲 ROM、BIOS、savestate、trace、VRAM 傾印、可重建
  原版的 fixture 一律不進 Git。fixture 只留 SHA-256 與轉檔腳本；測試沒有 ROM 就 `t.Skip`
  並印出原因。`tools/test_public_tree.py` 是機械 gate。
- **[HARD] Docker-only**：建置、測試、oracle 執行都在容器裡。主機只做 `docker`、`git`、
  檔案編輯。`docker run --rm --network none`，目前 UID/GID，`--memory`／`--cpus`／
  `--pids-limit`／`--log-opt max-size=10m --log-opt max-file=3`，外層 `timeout`。
- **[HARD] 不碰共用 docker 資源**：禁止任何 prune／rmi／動別人的 container。
- **[HARD] 位址三空間分開記**（`docs/spec/address-model.md`）：CPU logical、physical
  （21-bit）、ROM file offset，且每筆都帶完整 MPR0–7。沒有 MPR 的位址不得寫進任何輸出。
- **[HARD] 措辭邊界**：沒有同 ROM、同輸入、同 frame 的比對，不得寫「一致」「exact」；
  單元測試全綠只代表內部自洽。結論等級用 `已證實／強推論／假說／未知`。
- **[HARD] 沉默不是成功**：watchpoint 配額用完要回報略過筆數；oracle 比對跑完「沒差異」
  要先證明比對真的執行了（正對照）。

## 3. Gate

```bash
docker run --rm --network none -v "$PWD":/work -w /work -u "$(id -u):$(id -g)" -e HOME=/tmp \
  --memory 4g --cpus 4 --pids-limit 512 --log-opt max-size=10m --log-opt max-file=3 \
  nectaris-ebiten-test:20260816-v3 bash tools/gate.sh
```

`tools/gate.sh`：`gofmt -l`、`go vet ./...`、`go test -count=1 ./...`、`go build ./...`、
`python3 tools/test_public_tree.py`。`set -o pipefail` 不可省。
oracle 比對測試以 `ONEPCE_ROM`（ROM 路徑）與 `ONEPCE_FIXTURES`（本機 fixture 目錄）開關，
沒設就 skip，所以「gate 綠」不代表它們綠，收尾要另外跑一次。

image 暫時借用 `nectaris-ebiten-test:20260816-v3`（Go 1.25.12、`GOTOOLCHAIN=local`、
離線模組快取、Xvfb）；本專案只用標準程式庫，GUI（M6）才需要 Ebiten。

## 4. Oracle 工具

| 工具 | image | 用途 |
|---|---|---|
| Mesen2 2.1.1 headless Lua | `nectaris-mesen2-pce:20260812` | 指令 trace、watchpoint、VRAM／RAM 傾印 |
| Mednafen 1.29 | `nectaris-pce-oracle:20260811` | savestate 區段、畫面、`-soundrecord` |

probe 腳本放 `tools/oracle/`，全部以環境變數收參數，輸出到指定的本機目錄。
既有可搬用的 Lua 在 `~/cht/nectaris/tools/mesen2_*.lua`。

## 5. Git

- 逐一指定檔名 `git add`，commit message 繁中，結尾 `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`，
  不放 session 連結。
- push 前先跑 gate；建 tag／release 先回報使用者。
