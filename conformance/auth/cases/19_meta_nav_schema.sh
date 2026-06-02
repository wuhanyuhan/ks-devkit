#!/usr/bin/env bash
# 规则：SPEC.md §4 Spec B 扩展 —— /meta.nav 数组结构与字段约束
#       必填字段：label / category / open_mode
#       label 长度 ≤ 12 字符（前端 AdminLayout 渲染约束）
#       open_mode ∈ {full_page, dialog, drawer}
#       category ∈ {default, custom, ...}（开放枚举，不强校）
# 守护：Spec B §4.5 nav 字段定义
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

BODY=$(http_body "$TARGET/meta")

# nav 字段如未声明则跳过（向后兼容旧 claimant）
NAV_TYPE=$(echo "$BODY" | jq -r '.nav | type')
if [[ "$NAV_TYPE" == "null" ]]; then
    echo "  SKIP: /meta.nav 未声明（向后兼容）"
    exit 0
fi

[[ "$NAV_TYPE" == "array" ]] || { echo "FAIL: /meta.nav 必须是数组，实际 $NAV_TYPE"; exit 1; }

# 遍历每个 nav entry 校验
COUNT=$(echo "$BODY" | jq -r '.nav | length')
for i in $(seq 0 $((COUNT - 1))); do
    ENTRY=$(echo "$BODY" | jq -c ".nav[$i]")

    LABEL=$(echo "$ENTRY" | jq -r '.label // empty')
    [[ -n "$LABEL" ]] || { echo "FAIL: /meta.nav[$i].label 为空"; exit 1; }
    LABEL_LEN=${#LABEL}
    [[ $LABEL_LEN -le 12 ]] || { echo "FAIL: /meta.nav[$i].label='$LABEL' 长度 $LABEL_LEN > 12"; exit 1; }

    CATEGORY=$(echo "$ENTRY" | jq -r '.category // empty')
    [[ -n "$CATEGORY" ]] || { echo "FAIL: /meta.nav[$i].category 为空"; exit 1; }

    OPEN_MODE=$(echo "$ENTRY" | jq -r '.open_mode // empty')
    case "$OPEN_MODE" in
        full_page|dialog|drawer) ;;
        *) echo "FAIL: /meta.nav[$i].open_mode='$OPEN_MODE' 不在 {full_page, dialog, drawer}"; exit 1 ;;
    esac

    # order / required_perms 是可选字段
    ORDER_TYPE=$(echo "$ENTRY" | jq -r '.order | type')
    if [[ "$ORDER_TYPE" != "null" && "$ORDER_TYPE" != "number" ]]; then
        echo "FAIL: /meta.nav[$i].order 必须是 number 或省略，实际 $ORDER_TYPE"; exit 1
    fi

    PERMS_TYPE=$(echo "$ENTRY" | jq -r '.required_perms | type')
    if [[ "$PERMS_TYPE" != "null" && "$PERMS_TYPE" != "array" ]]; then
        echo "FAIL: /meta.nav[$i].required_perms 必须是 array 或省略，实际 $PERMS_TYPE"; exit 1
    fi
done

echo "  PASS: /meta.nav schema ok ($COUNT entries)"
