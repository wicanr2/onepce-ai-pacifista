#!/usr/bin/env bash
# 在 nectaris-pce-oracle image 裡用 Mednafen 的 dummy 音訊驅動錄開機音訊（docs/spec/psg.md §6.3）。
# 掛載：/rom.zip（唯讀）、/out（可寫）。環境變數 REC_SECONDS（預設 22）。
# 產物 mednafen.wav 是原版音訊的衍生資料：不進版控、不進 release。
set -euo pipefail
unzip -p /rom.zip '*.pce' > /tmp/rom.pce
export HOME=/tmp/medhome; mkdir -p "$HOME"
Xvfb :99 -screen 0 640x480x24 -nolisten tcp >/out/xvfb.log 2>&1 &
XV=$!
sleep 1
DISPLAY=:99 timeout "${REC_SECONDS:-22}" /usr/games/mednafen -sound 1 -sounddriver dummy -sound.rate 44100 \
  -soundrecord /out/mednafen.wav -force_module pce -video.fs 0 /tmp/rom.pce >/out/mednafen.log 2>&1 || true
kill $XV 2>/dev/null || true
ls -la /out
