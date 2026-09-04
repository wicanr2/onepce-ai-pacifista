# 最近交接

最後更新：2026-09-04

## Repository

- branch `main`，public，remote `github.com/wicanr2/onepce-ai-remake`。
- 參考副本：`~/cht/tmp/Mesen2`（`b9fa69d`，只取行為事實）、`~/cht/tmp/ares`（`7b51c8a`，結構）。
- 建置 image 暫借 `nectaris-ebiten-test:20260816-v3`。

## 本輪已完成

- 規劃書、README、RRSAL-1.0 授權、`CLAUDE.md` 工作契約。
- M0：`tools/gate.sh`、`tools/test_public_tree.py`、`docs/spec/address-model.md`、
  根套件 `onepce` 的 `MPR`／`Address`／固定顯示格式與測試。

## 私人輸入

- 目前沒有。oracle fixture 之後放本機 `dist-all/`（已 gitignore）。

## 下一個最小動作

1. 跑 gate 確認 M0 綠。
2. 寫 `docs/spec/huc6280.md`：opcode 表來源用公開指令集文件，Mesen2 只作交叉核對。
