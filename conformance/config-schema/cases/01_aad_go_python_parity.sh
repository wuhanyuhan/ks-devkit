#!/usr/bin/env bash
# cases/01_aad_go_python_parity.sh — AAD canonical 字节：Go ↔ Python 互通。
#
# 对 testvectors.json `.aad_canonical[]` 里的每个 vector，把 (id, version, fp)
# 喂给 Go / Python mock-tool 各跑一遍，比对 stdout hex 是否字节级相同。
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
    py_out=$(run_py_tool py-aad.py "$mcp_id" "$version" "$fingerprint")

    if ! bytes_eq "$go_out" "$py_out"; then
        fail "AAD mismatch @ $name
  go: $go_out
  py: $py_out"
    fi
    N=$((N + 1))
    info "[$name] OK"
done < <(load_vectors aad_canonical)

pass "01_aad_go_python_parity: $N vectors OK"
