#!/usr/bin/env bash
# 规则：SPEC.md §4 /meta.auth_mode 必须等于实际生效的 auth mode
# claimant 声明 keystone_jwks，所以 /meta 应返回 keystone_jwks
# （或 none，如果 claimant 启动时 KS_APP_AUTH_MODE=insecure；本测试假设生产配置）
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

BODY=$(http_body "$TARGET/meta")
AUTH_MODE=$(echo "$BODY" | jq -r '.auth_mode // "none"')

# claimant 目标配置：keystone_jwks 生效
assert_eq "$AUTH_MODE" "keystone_jwks" "/meta.auth_mode 应反映 claimant 实际配置"
