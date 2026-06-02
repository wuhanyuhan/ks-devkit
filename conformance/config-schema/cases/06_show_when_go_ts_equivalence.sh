#!/usr/bin/env bash
# cases/06_show_when_go_ts_equivalence.sh — show_when 编译等价：Go ↔ TS。
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

GO_SW=$(build_go_tool "go-showwhen")

ACCEPT_N=0
REJECT_N=0

while IFS= read -r vec; do
    name=$(echo "$vec" | jq -r .name)
    dsl=$(echo "$vec" | jq -r .dsl)
    should_reject=$(echo "$vec" | jq -r '.should_reject // false')
    field_name=$(echo "$vec" | jq -r '.context.field_under_if // "value"')

    if [[ "$should_reject" == "true" ]]; then
        set +e
        go_out=$(echo "$dsl" | "$GO_SW" "$field_name" 2>/dev/null)
        go_rc=$?
        ts_out=$(echo "$dsl" | run_ts_tool ts-showwhen "$field_name" 2>/dev/null)
        ts_rc=$?
        set -e
        if [[ $go_rc -eq 0 || $ts_rc -eq 0 ]]; then
            fail "reject case @ $name 应失败，但某一端成功
  go_rc=$go_rc go_out=$go_out
  ts_rc=$ts_rc ts_out=$ts_out"
        fi
        # reject 时 rc 语义必须一致（10 vs 11 分类对齐）。
        if [[ $go_rc -ne $ts_rc ]]; then
            fail "reject case @ $name rc 不一致：go_rc=$go_rc, ts_rc=$ts_rc"
        fi
        REJECT_N=$((REJECT_N + 1))
        info "[$name] reject OK: go_rc=$go_rc ts_rc=$ts_rc"
    else
        go_out=$(echo "$dsl" | "$GO_SW" "$field_name")
        ts_out=$(echo "$dsl" | run_ts_tool ts-showwhen "$field_name")

        if ! canonical_json_eq "$go_out" "$ts_out"; then
            fail "show_when mismatch @ $name
  dsl: $dsl
  go:  $go_out
  ts:  $ts_out"
        fi
        ACCEPT_N=$((ACCEPT_N + 1))
        info "[$name] accept OK"
    fi
done < <(load_vectors show_when)

pass "06_show_when_go_ts_equivalence: $ACCEPT_N accept + $REJECT_N reject OK"
