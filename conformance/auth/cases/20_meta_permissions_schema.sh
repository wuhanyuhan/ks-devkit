#!/usr/bin/env bash
# 规则：SPEC.md §4 Spec B 扩展 —— /meta.permissions[] 结构
#       code 格式 ^mcp\.[a-z_]+\.[a-z_]+$
#       label 非空
# 守护：Spec B §4.5 permissions 字段定义
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

BODY=$(http_body "$TARGET/meta")

PERMS_TYPE=$(echo "$BODY" | jq -r '.permissions | type')
if [[ "$PERMS_TYPE" == "null" ]]; then
    echo "  SKIP: /meta.permissions 未声明（向后兼容）"
    exit 0
fi

[[ "$PERMS_TYPE" == "array" ]] || { echo "FAIL: /meta.permissions 必须是数组，实际 $PERMS_TYPE"; exit 1; }

COUNT=$(echo "$BODY" | jq -r '.permissions | length')
PATTERN='^mcp\.[a-z_]+\.[a-z_]+$'

for i in $(seq 0 $((COUNT - 1))); do
    ENTRY=$(echo "$BODY" | jq -c ".permissions[$i]")

    CODE=$(echo "$ENTRY" | jq -r '.code // empty')
    [[ -n "$CODE" ]] || { echo "FAIL: /meta.permissions[$i].code 为空"; exit 1; }
    if [[ ! "$CODE" =~ $PATTERN ]]; then
        echo "FAIL: /meta.permissions[$i].code='$CODE' 不匹配 $PATTERN"
        exit 1
    fi

    LABEL=$(echo "$ENTRY" | jq -r '.label // empty')
    [[ -n "$LABEL" ]] || { echo "FAIL: /meta.permissions[$i].label 为空"; exit 1; }
done

echo "  PASS: /meta.permissions schema ok ($COUNT entries)"
