#!/usr/bin/env bash
# 规则：SPEC.md §4 /meta 响应 schema
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

BODY=$(http_body "$TARGET/meta")

NAME=$(echo "$BODY" | jq -r '.name // empty')
[[ -n "$NAME" ]] || { echo "FAIL: /meta.name 为空"; exit 1; }

VERSION=$(echo "$BODY" | jq -r '.version // empty')
[[ -n "$VERSION" ]] || { echo "FAIL: /meta.version 为空"; exit 1; }

# tools 字段必须是数组（可为空数组）
TOOLS_TYPE=$(echo "$BODY" | jq -r '.tools | type')
[[ "$TOOLS_TYPE" == "array" ]] || { echo "FAIL: /meta.tools 必须是数组，实际 $TOOLS_TYPE"; exit 1; }

echo "  PASS: /meta schema ok (name=$NAME, version=$VERSION, tools is array)"
