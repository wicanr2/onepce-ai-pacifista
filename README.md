# OnePCE AI Remake

給 AI 代理與 remake 專案對拍用的 PC Engine 模擬器，以 Go 撰寫，headless 優先。

它的價值不在「能玩」，在**狀態可完整觀測、可序列化、可在 `go test` 裡當函式庫呼叫**：
watchpoint 帶完整 MPR、區段快照有標籤、輸入以 frame 腳本化且決定性、畫面以原生像素輸出、
PSG 可記錄成 VGM。第一個客戶是 [`nectaris-cht`](https://github.com/wicanr2/nectaris-cht)。

規劃書：[`docs/PLAN.md`](docs/PLAN.md)（草案，含待決事項）。目前沒有程式碼。

## 範圍

第一版：HuCard 標準機（HuC6280、HuC6270 VDC、HuC6260 VCE、PSG、兩鍵手把、含 384 KB
這類非 2 的冪 HuCard）。不做 CD-ROM²、SuperGrafx、Arcade Card。GUI 在 headless 全部
通過之後做，定位是對拍輔助（原生畫面、單步、快照、與 remake 畫面並排／疊圖）。

## 授權、致謝與聲明

採 **RRSAL-1.0**（復古重製 source-available 授權條款），全文見 `LICENSE`：非商業用途免費
（含修改與再散布），商業用途請洽 `wicanr2@gmail.com`。

參考來源的邊界：Mesen2（GPL-3.0）只作**行為事實**與執行期 oracle，不翻譯其程式碼；
結構參考 ares（ISC），併入處會在 `NOTICE.md` 保留其版權聲明。細則在 `docs/PLAN.md` 第三節。
本儲存庫不含任何遊戲 ROM、BIOS 或由原版資料重建的素材。
