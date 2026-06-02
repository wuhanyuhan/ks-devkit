#!/usr/bin/env bash
# cases/05_show_when_go_python_equivalence.sh — show_when 编译等价：Go ↔ Python。
#
# 对 testvectors.show_when[] 每个向量：
#   - accept 型（should_reject 不为 true）：二端编译成功 + canonical JSON 字节一致
#   - reject 型（should_reject == true）：二端都非零退出
#
# 字节对比用 canonical_json_eq（jq -Sc 排键 + compact），避免 Go json.Encoder 与
# Python json.dumps 输出顺序/空格差异影响判断。
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

GO_SW=$(build_go_tool "go-showwhen")

ACCEPT_N=0
REJECT_N=0

while IFS= read -r vec; do
    name=$(echo "$vec" | jq -r .name)
    dsl=$(echo "$vec" | jq -r .dsl)
    should_reject=$(echo "$vec" | jq -r '.should_reject // false')

    # array_item_show_when 的 expected_json_schema 顶层包了 array/items/allOf，
    # 与 mock-tool 输出的 if_then_else 不直接对齐；conformance 只比对单条
    # compile_show_when 产物（if_then_else 本体），而不处理 array wrapper。
    # 这对 Go↔Python 字节对比没影响，只是不走 array wrapper 分支。
    field_name=$(echo "$vec" | jq -r '.context.field_under_if // "value"')

    if [[ "$should_reject" == "true" ]]; then
        set +e
        go_out=$(echo "$dsl" | "$GO_SW" "$field_name" 2>/dev/null)
        go_rc=$?
        py_out=$(echo "$dsl" | run_py_tool py-showwhen.py "$field_name" 2>/dev/null)
        py_rc=$?
        set -e
        if [[ $go_rc -eq 0 || $py_rc -eq 0 ]]; then
            fail "reject case @ $name 应失败，但某一端成功
  go_rc=$go_rc go_out=$go_out
  py_rc=$py_rc py_out=$py_out"
        fi
        # reject 时 rc 语义必须一致
        # （10 = parse error / 11 = SyntaxError）。只校验"都非零"不够严：可能
        # 一端 10 一端 11 — 意味着对同一错误二端分类不同，是 drift 信号。
        if [[ $go_rc -ne $py_rc ]]; then
            fail "reject case @ $name rc 不一致：go_rc=$go_rc, py_rc=$py_rc"
        fi
        REJECT_N=$((REJECT_N + 1))
        info "[$name] reject OK: go_rc=$go_rc py_rc=$py_rc"
    else
        go_out=$(echo "$dsl" | "$GO_SW" "$field_name")
        py_out=$(echo "$dsl" | run_py_tool py-showwhen.py "$field_name")

        if ! canonical_json_eq "$go_out" "$py_out"; then
            fail "show_when mismatch @ $name
  dsl: $dsl
  go:  $go_out
  py:  $py_out"
        fi
        ACCEPT_N=$((ACCEPT_N + 1))
        info "[$name] accept OK"
    fi
done < <(load_vectors show_when)

pass "05_show_when_go_python_equivalence: $ACCEPT_N accept + $REJECT_N reject OK"
