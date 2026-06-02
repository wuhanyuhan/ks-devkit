#!/usr/bin/env bash
# cases/17_idempotency_uuid_valid.sh — idempotency_key 格式 smoke。
#
# spec-v1 §8.3 约定 EncryptedConfigPayload.idempotency_key 必须是合法 uuid-v4，
# 正则：^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$。
#
# 本 case 生成 10 个 Python uuid.uuid4()，确认全部匹配。Go 侧的 uuid-v4 合法性
# 由 sdk/go/ksapp/idempotency_test.go 覆盖（正则一致）；TS 侧的 crypto.randomUUID
# 由 WHATWG 规范保证 v4 格式。这里选 Python 代表，避免引入额外 conformance mock。
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

UUID_RE='^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'

N=0
for i in $(seq 1 10); do
    UUID=$(python3 -c 'import uuid; print(uuid.uuid4())')
    if ! [[ "$UUID" =~ $UUID_RE ]]; then
        fail "invalid uuid v4 at #$i: $UUID"
    fi
    info "[#$i] $UUID OK"
    N=$((N + 1))
done
pass "17_idempotency_uuid_valid: $N uuids OK"
