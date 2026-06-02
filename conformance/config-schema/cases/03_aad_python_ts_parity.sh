#!/usr/bin/env bash
# cases/03_aad_python_ts_parity.sh — AAD canonical 字节：Python ↔ TS 互通。
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

N=0
while IFS= read -r vec; do
    name=$(echo "$vec" | jq -r .name)
    mcp_id=$(echo "$vec" | jq -r .mcp_server_id)
    version=$(echo "$vec" | jq -r .config_version)
    fingerprint=$(echo "$vec" | jq -r .fingerprint)

    py_out=$(run_py_tool py-aad.py "$mcp_id" "$version" "$fingerprint")
    ts_out=$(run_ts_tool ts-aad "$mcp_id" "$version" "$fingerprint")

    if ! bytes_eq "$py_out" "$ts_out"; then
        fail "AAD mismatch @ $name
  py: $py_out
  ts: $ts_out"
    fi
    N=$((N + 1))
    info "[$name] OK"
done < <(load_vectors aad_canonical)

pass "03_aad_python_ts_parity: $N vectors OK"
