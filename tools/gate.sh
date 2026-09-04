#!/usr/bin/env bash
# 公開 gate：在容器裡跑（CLAUDE.md §3）。任何一步紅就整體紅。
set -euo pipefail
cd "$(dirname "$0")/.."

unformatted=$(gofmt -l ./ 2>/dev/null | grep -v '^$' || true)
if [[ -n "$unformatted" ]]; then
  echo "gofmt 未格式化：" >&2
  echo "$unformatted" >&2
  exit 1
fi
go vet ./...
go test -count=1 ./...
go build ./...
python3 tools/test_public_tree.py
echo "GATE-OK"
