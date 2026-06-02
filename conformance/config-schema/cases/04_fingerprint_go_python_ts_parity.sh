#!/usr/bin/env bash
# cases/04_fingerprint_go_python_ts_parity.sh — Fingerprint 算法三端互通。
#
# 对 .fingerprint[] 的每个 pubkey_hex，三端各跑一遍，要求：
#   1. 三端彼此一致
#   2. 三端与 testvectors.expected_fingerprint 也一致（这是 golden 比对顺手验证，
#      golden 正向 case 会进一步完整覆盖）
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

GO_FP=$(build_go_tool "go-fingerprint")

N=0
while IFS= read -r vec; do
    name=$(echo "$vec" | jq -r .name)
    pubkey_hex=$(echo "$vec" | jq -r .pubkey_hex)
    expected=$(echo "$vec" | jq -r .expected_fingerprint)

    go_out=$("$GO_FP" "$pubkey_hex")
    py_out=$(run_py_tool py-fingerprint.py "$pubkey_hex")
    ts_out=$(run_ts_tool ts-fingerprint "$pubkey_hex")

    if ! str_eq "$go_out" "$py_out"; then
        fail "fingerprint Go ↔ Python mismatch @ $name: go=$go_out, py=$py_out"
    fi
    if ! str_eq "$go_out" "$ts_out"; then
        fail "fingerprint Go ↔ TS mismatch @ $name: go=$go_out, ts=$ts_out"
    fi
    if ! str_eq "$py_out" "$ts_out"; then
        fail "fingerprint Python ↔ TS mismatch @ $name: py=$py_out, ts=$ts_out"
    fi
    if ! str_eq "$go_out" "$expected"; then
        fail "fingerprint vs golden mismatch @ $name: got=$go_out, golden=$expected"
    fi
    N=$((N + 1))
    info "[$name] OK: $go_out"
done < <(load_vectors fingerprint)

pass "04_fingerprint_go_python_ts_parity: $N vectors OK（三端一致 + golden 一致）"
