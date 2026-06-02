#!/usr/bin/env bash
# orchestrate.sh — 预检三语言 runtime + 预构建 Go mock-tool + 跑 run.sh。
#
# 与 conformance/auth 不同，本套件不需要启 mock-server：所有 case 都是
# 三个本地子进程的字节 / 字符串比对。
#
# Usage:
#   ./orchestrate.sh [--verbose] [--only=01,04]

set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"

echo "Preflight: checking toolchain ..."

# Python
if ! command -v python3 >/dev/null; then
    echo "ERROR: 缺 python3" >&2
    exit 3
fi

# Go
if ! command -v go >/dev/null; then
    echo "ERROR: 缺 go" >&2
    exit 3
fi

# TS runtime
if command -v bun >/dev/null; then
    TS_RT="bun ($(bun --version))"
elif command -v tsx >/dev/null; then
    TS_RT="tsx"
elif command -v npx >/dev/null; then
    TS_RT="npx tsx (按需下载)"
else
    echo "ERROR: 缺 bun / tsx / npx 任一（TS runtime）" >&2
    exit 3
fi

# jq — 要求 1.7+。1.6 及更早对 JSON number 精度有 64-bit 丢精度问题
# （testvectors.json 有 9223372036854775807 之类的 u63 值），会让 AAD canonical
# 字节直接错位。此处要求显式门禁。
if ! command -v jq >/dev/null; then
    echo "ERROR: 缺 jq" >&2
    exit 3
fi
JQ_VER=$(jq --version | sed 's/^jq-//')
JQ_MAJOR=${JQ_VER%%.*}
JQ_REST=${JQ_VER#*.}
JQ_MINOR=${JQ_REST%%.*}
if (( JQ_MAJOR < 1 )) || { (( JQ_MAJOR == 1 )) && (( JQ_MINOR < 7 )); }; then
    echo "ERROR: jq 版本 $JQ_VER 过旧，至少要 1.7（精度）" >&2
    exit 3
fi

echo "  python3: $(python3 --version 2>&1)"
echo "  go:      $(go version 2>&1 | awk '{print $3}')"
echo "  ts:      $TS_RT"
echo "  jq:      $(jq --version 2>&1)"

# 预构建 Go mock-tool（避免 case 内串行 go build）
# 新增 go-encrypt / go-decrypt / go-keygen。
echo
echo "Pre-building Go mock-tools ..."
for tool in go-aad go-fingerprint go-showwhen go-keygen go-encrypt go-decrypt; do
    if [[ ! -d "$HERE/mock-tools/$tool" ]]; then
        continue
    fi
    out="/tmp/ks-conf-$tool"
    if (cd "$HERE/mock-tools/$tool" && go build -o "$out" .); then
        echo "  built $tool → $out"
    else
        echo "ERROR: 构建 $tool 失败" >&2
        exit 3
    fi
done

export SKIP_GO_BUILD=1

# TS 依赖预检：ts-encrypt / ts-decrypt / ts-keygen 使用 @noble/curves，要求各自
# 目录下已 `bun install`。缺 node_modules 直接报错，让用户一次性跑 bun install。
# 新增。
echo
echo "Checking TS mock-tool deps ..."
for tool in ts-encrypt ts-decrypt ts-keygen; do
    dir="$HERE/mock-tools/$tool"
    if [[ ! -d "$dir/node_modules/@noble/curves" ]]; then
        echo "ERROR: $tool 缺依赖 — 请在 $dir 下跑 'bun install'" >&2
        exit 3
    fi
    echo "  $tool: node_modules OK"
done

echo
echo "Running cases ..."
exec "$HERE/run.sh" "$@"
