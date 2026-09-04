#!/usr/bin/env bash
set -euo pipefail
# 在 nectaris-mesen2-pce Docker image 內執行 Mesen2 Lua probe（自 nectaris-cht 搬來，同一套契約）。
# 在 nectaris-mesen2-pce Docker image 內執行 Mesen2 Lua probe。
# 呼叫端必須以 Docker 的 -e HOME=<已初始化且可寫的 Mesen 設定目錄> 提供 home；
# 本工具不修改 host 設定、不複製 ROM 到 output，也不接受未初始化的 headless config。
# --pcEngine.disableFrameSkipping=true 不可省：全速模式下 Mesen2 距上次繪製不到 10 ms 就略過
# 繪製（PceVpc::ProcessStartFrame），畫面傾印會隨牆鐘時間拿到舊 frame 的圖。

ROM_ARCHIVE=${1:?用法：mesen2_pce_headless_probe.sh <rom.zip> <output-dir> <probe.lua>}
OUTPUT_DIR=${2:?用法：mesen2_pce_headless_probe.sh <rom.zip> <output-dir> <probe.lua>}
LUA_SCRIPT=${3:?用法：mesen2_pce_headless_probe.sh <rom.zip> <output-dir> <probe.lua>}

MESEN_BIN=${MESEN_ROOT:-/opt/mesen}/Mesen
MESEN_TIMEOUT_SECONDS=${MESEN_TIMEOUT_SECONDS:-45}
OUTER_TIMEOUT_SECONDS=${OUTER_TIMEOUT_SECONDS:-60}
MESEN_CONFIG_DIR=${HOME:?需要由 Docker 提供 HOME}/.config/Mesen2
SCRIPT_DATA_ROOT=${MESEN_CONFIG_DIR}/LuaScriptData

case "$MESEN_TIMEOUT_SECONDS" in ''|*[!0-9]*) echo "MESEN_TIMEOUT_SECONDS 必須是非負整數" >&2; exit 2 ;; esac
case "$OUTER_TIMEOUT_SECONDS" in ''|*[!0-9]*) echo "OUTER_TIMEOUT_SECONDS 必須是非負整數" >&2; exit 2 ;; esac

test -r "$ROM_ARCHIVE"
test -r "$LUA_SCRIPT"
test -d "$OUTPUT_DIR"
test -w "$OUTPUT_DIR"
test -x "$MESEN_BIN"
test -f "${MESEN_CONFIG_DIR}/settings.json" || {
  echo "找不到已初始化的 Mesen2 settings.json；先執行 GUI 初始設定 probe" >&2
  exit 2
}

WORK_DIR=$(mktemp -d /tmp/nectaris-mesen2-headless-XXXXXX)
RUN_DIR=$(mktemp -d "${OUTPUT_DIR%/}/mesen2-headless-XXXXXX")
cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

ROM_FILE=${WORK_DIR}/oracle.pce
SCRIPT_ALIAS=$(mktemp "${WORK_DIR}/mesen2-run-XXXXXX.lua")
SCRIPT_TOKEN=$(basename "$SCRIPT_ALIAS" .lua)
SCRIPT_OUTPUT_DIR=${SCRIPT_DATA_ROOT}/${SCRIPT_TOKEN}

unzip -p "$ROM_ARCHIVE" '*.pce' > "$ROM_FILE"
test -s "$ROM_FILE"
cp -- "$LUA_SCRIPT" "$SCRIPT_ALIAS"

set +e
timeout "${OUTER_TIMEOUT_SECONDS}s" "$MESEN_BIN" \
  --testRunner \
  --timeout="$MESEN_TIMEOUT_SECONDS" \
  --doNotSaveSettings \
  --debug.scriptWindow.allowIoOsAccess=true \
  --pcEngine.disableFrameSkipping=true \
  "$SCRIPT_ALIAS" "$ROM_FILE" >"${RUN_DIR}/mesen.log" 2>&1
MESEN_EXIT=$?
set -e

if test -d "$SCRIPT_OUTPUT_DIR"; then
  mkdir -p "${RUN_DIR}/script-output"
  cp -a "$SCRIPT_OUTPUT_DIR/." "${RUN_DIR}/script-output/"
else
  printf 'Mesen2 沒有建立 per-script output：%s\n' "$SCRIPT_OUTPUT_DIR" >&2
  MESEN_EXIT=7
fi

{
  printf 'schema\tmesen2-pce-headless-probe-v1\n'
  printf 'mesen_exit\t%s\n' "$MESEN_EXIT"
  printf 'mesen_timeout_seconds\t%s\n' "$MESEN_TIMEOUT_SECONDS"
  printf 'outer_timeout_seconds\t%s\n' "$OUTER_TIMEOUT_SECONDS"
  printf 'mesen_binary_sha256\t%s\n' "$(sha256sum "$MESEN_BIN" | awk '{print $1}')"
  printf 'rom_archive_sha256\t%s\n' "$(sha256sum "$ROM_ARCHIVE" | awk '{print $1}')"
  printf 'rom_extracted_sha256\t%s\n' "$(sha256sum "$ROM_FILE" | awk '{print $1}')"
  printf 'lua_script_sha256\t%s\n' "$(sha256sum "$LUA_SCRIPT" | awk '{print $1}')"
  printf 'script_token\t%s\n' "$SCRIPT_TOKEN"
} >"${RUN_DIR}/metadata.tsv"

# 不替 hash manifest 本身計 hash，避免建立檔案的同時把空檔雜湊寫入結果。
find "$RUN_DIR" -type f ! -name hashes.sha256 -print0 | sort -z | xargs -0 sha256sum >"${RUN_DIR}/hashes.sha256"
printf 'Mesen2 headless probe output: %s\n' "$RUN_DIR"
exit "$MESEN_EXIT"
