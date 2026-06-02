#!/usr/bin/env bash
# 规则：SPEC.md §4 Spec B 扩展 —— /meta.config_status 枚举（由 Spec A §6.4 引入）
#       值 ∈ {unconfigured, via_frontend, via_cli, mixed}（如有声明）
# 守护：Spec A §6.4 config_status 字段定义
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

BODY=$(http_body "$TARGET/meta")

CONFIG_STATUS=$(echo "$BODY" | jq -r '.config_status // empty')

if [[ -z "$CONFIG_STATUS" ]]; then
    echo "  SKIP: /meta.config_status 未声明（向后兼容）"
    exit 0
fi

case "$CONFIG_STATUS" in
    unconfigured|via_frontend|via_cli|mixed) ;;
    *) echo "FAIL: /meta.config_status='$CONFIG_STATUS' 不在 {unconfigured, via_frontend, via_cli, mixed}"; exit 1 ;;
esac

echo "  PASS: /meta.config_status='$CONFIG_STATUS' 合法"
