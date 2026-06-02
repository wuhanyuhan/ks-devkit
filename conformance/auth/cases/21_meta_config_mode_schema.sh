#!/usr/bin/env bash
# 规则：SPEC.md §4 Spec B 扩展 —— /meta.config_mode 枚举
#       值 ∈ {schema, iframe, none}（如有声明）
# 守护：Spec B §4.5 config_mode 字段定义
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

BODY=$(http_body "$TARGET/meta")

CONFIG_MODE=$(echo "$BODY" | jq -r '.config_mode // empty')

if [[ -z "$CONFIG_MODE" ]]; then
    echo "  SKIP: /meta.config_mode 未声明（向后兼容）"
    exit 0
fi

case "$CONFIG_MODE" in
    schema|iframe|none) ;;
    *) echo "FAIL: /meta.config_mode='$CONFIG_MODE' 不在 {schema, iframe, none}"; exit 1 ;;
esac

echo "  PASS: /meta.config_mode='$CONFIG_MODE' 合法"
