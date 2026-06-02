#!/usr/bin/env bash
# cases/02_aad_go_ts_parity.sh — AAD canonical 字节：Go ↔ TS 互通。
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

GO_AAD=$(build_go_tool "go-aad")

N=0
while IFS= read -r vec; do
    name=$(echo "$vec" | jq -r .name)
    mcp_id=$(echo "$vec" | jq -r .mcp_server_id)
    version=$(echo "$vec" | jq -r .config_version)
    fingerprint=$(echo "$vec" | jq -r .fingerprint)

    go_out=$("$GO_AAD" "$mcp_id" "$version" "$fingerprint")
    ts_out=$(run_ts_tool ts-aad "$mcp_id" "$version" "$fingerprint")

    if ! bytes_eq "$go_out" "$ts_out"; then
        fail "AAD mismatch @ $name
  go: $go_out
  ts: $ts_out"
    fi
    N=$((N + 1))
    info "[$name] OK"
done < <(load_vectors aad_canonical)

pass "02_aad_go_ts_parity: $N vectors OK"
