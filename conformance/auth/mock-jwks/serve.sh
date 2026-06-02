#!/usr/bin/env bash
# 起一个本地 HTTP 服务，在 /jwks.json 路径暴露运行时生成的 mock JWKS。
#
# 使用方式：
#   ./serve.sh [port] [mock-dir]   (默认 port 9999；未传 mock-dir 时自动生成临时目录)
#
# 背景运行：
#   ./serve.sh 9999 &
#   PID=$!
#   # ... 做测试 ...
#   kill $PID

set -euo pipefail

PORT="${1:-9999}"
DIR="$(cd "$(dirname "$0")" && pwd)"
MOCK_DIR="${2:-}"

if [[ -z "$MOCK_DIR" ]]; then
    MOCK_DIR="$(mktemp -d)"
    "$DIR/generate.sh" "$MOCK_DIR"
fi

cd "$MOCK_DIR"

# 用 Python 内置 http.server（所有 linux/mac 默认有 python3）
exec python3 -m http.server "$PORT" --bind 127.0.0.1
