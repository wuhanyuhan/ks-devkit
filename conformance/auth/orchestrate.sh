#!/usr/bin/env bash
# orchestrate.sh - 一键起 mock-jwks + claimant + 跑 cases + 清理
#
# Usage:
#   ./orchestrate.sh \
#       --claimant-cmd="cd /path && go run ." \
#       --claimant-port=8080 \
#       [--claimant-env="KEY1=val1 KEY2=val2"] \
#       [--keep-alive-on-fail]

set -uo pipefail

CLAIMANT_CMD=""
CLAIMANT_PORT=""
CLAIMANT_ENV=""
KEEP_ALIVE=""

for arg in "$@"; do
    case "$arg" in
        --claimant-cmd=*) CLAIMANT_CMD="${arg#*=}" ;;
        --claimant-port=*) CLAIMANT_PORT="${arg#*=}" ;;
        --claimant-env=*) CLAIMANT_ENV="${arg#*=}" ;;
        --keep-alive-on-fail) KEEP_ALIVE="1" ;;
        --help|-h) sed -n '3,11p' "$0"; exit 0 ;;
        *) echo "Unknown arg: $arg" >&2; exit 2 ;;
    esac
done

[[ -z "$CLAIMANT_CMD" || -z "$CLAIMANT_PORT" ]] && {
    echo "ERROR: --claimant-cmd and --claimant-port are required" >&2
    exit 2
}

HERE="$(cd "$(dirname "$0")" && pwd)"
MOCK_PID=""
CLAIMANT_PID=""
MOCK_DIR=""

cleanup() {
    local exit_code=$?
    if [[ -n "$KEEP_ALIVE" && $exit_code -ne 0 ]]; then
        echo
        echo "--- KEEP ALIVE ON FAIL ---"
        echo "mock-jwks PID: $MOCK_PID  (http://localhost:9999/jwks.json)"
        echo "claimant PID:  $CLAIMANT_PID  (http://localhost:$CLAIMANT_PORT)"
        echo "Sleeping 30s for manual curl debugging..."
        echo "-------------------------"
        sleep 30
    fi
    [[ -n "$CLAIMANT_PID" ]] && kill "$CLAIMANT_PID" 2>/dev/null || true
    [[ -n "$MOCK_PID" ]] && kill "$MOCK_PID" 2>/dev/null || true
    [[ -n "$MOCK_DIR" ]] && rm -rf "$MOCK_DIR"
    exit $exit_code
}
trap cleanup EXIT INT TERM

# 1. 起 mock-jwks
echo "Starting mock-jwks on :9999 ..."
MOCK_DIR="$(mktemp -d)"
"$HERE/mock-jwks/generate.sh" "$MOCK_DIR"
"$HERE/mock-jwks/serve.sh" 9999 "$MOCK_DIR" >/tmp/mock-jwks.log 2>&1 &
MOCK_PID=$!
sleep 1
if ! curl -sf --max-time 3 http://localhost:9999/jwks.json >/dev/null; then
    echo "ERROR: mock-jwks 启动失败，日志：" >&2
    cat /tmp/mock-jwks.log >&2
    exit 3
fi
echo "mock-jwks ready (PID=$MOCK_PID)"

# 2. 起 claimant
echo "Starting claimant: $CLAIMANT_CMD"
export KEYSTONE_JWKS_URL="http://localhost:9999/jwks.json"
export KSAPP_SERVER_ID="${KSAPP_SERVER_ID:-1}"
if [[ -n "$CLAIMANT_ENV" ]]; then
    eval "export $CLAIMANT_ENV"
fi
eval "$CLAIMANT_CMD" >/tmp/claimant.log 2>&1 &
CLAIMANT_PID=$!

# 3. 等 claimant healthy（30s 超时）
for i in $(seq 1 30); do
    if curl -sf --max-time 2 "http://localhost:$CLAIMANT_PORT/healthz" >/dev/null; then
        echo "claimant ready (PID=$CLAIMANT_PID)"
        break
    fi
    if ! kill -0 "$CLAIMANT_PID" 2>/dev/null; then
        echo "ERROR: claimant 进程已退出，日志：" >&2
        cat /tmp/claimant.log >&2
        exit 3
    fi
    sleep 1
    [[ $i -eq 30 ]] && { echo "ERROR: claimant 30s 未就绪" >&2; cat /tmp/claimant.log >&2; exit 3; }
done

# 4. 跑 run.sh
echo
echo "Running conformance cases ..."
"$HERE/run.sh" \
    --target="http://localhost:$CLAIMANT_PORT" \
    --jwks="http://localhost:9999/jwks.json" \
    --keystone-signer="$MOCK_DIR/keys"
