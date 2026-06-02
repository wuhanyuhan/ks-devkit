#!/usr/bin/env bash
# run.sh - 跑所有 cases，汇总输出
#
# Usage:
#   ./run.sh \
#       --target=http://localhost:8080 \
#       --jwks=http://localhost:9999/jwks.json \
#       --keystone-signer=./mock-jwks/keys \
#       [--only=01,05] \
#       [--verbose]
#
# Exit codes:
#   0  all passed
#   1  at least one case failed
#   2  argument error
#   3  target or jwks unreachable

set -uo pipefail

TARGET=""
JWKS=""
SIGNER_KEY_DIR=""
ONLY=""
VERBOSE=""

for arg in "$@"; do
    case "$arg" in
        --target=*) TARGET="${arg#*=}" ;;
        --jwks=*) JWKS="${arg#*=}" ;;
        --keystone-signer=*) SIGNER_KEY_DIR="${arg#*=}" ;;
        --only=*) ONLY="${arg#*=}" ;;
        --verbose) VERBOSE="1" ;;
        --help|-h)
            sed -n '3,15p' "$0"
            exit 0
            ;;
        *)
            echo "Unknown arg: $arg" >&2
            exit 2
            ;;
    esac
done

if [[ -z "$TARGET" || -z "$JWKS" || -z "$SIGNER_KEY_DIR" ]]; then
    echo "ERROR: --target, --jwks, --keystone-signer 都是必需参数" >&2
    exit 2
fi

# 预检
if ! curl -sf --max-time 5 "$TARGET/healthz" >/dev/null; then
    echo "ERROR: target 不可达 ($TARGET/healthz)" >&2
    exit 3
fi
if ! curl -sf --max-time 5 "$JWKS" >/dev/null; then
    echo "ERROR: JWKS 不可达 ($JWKS)" >&2
    exit 3
fi

# 导出给 cases 用
export TARGET JWKS SIGNER_KEY_DIR VERBOSE

CASES_DIR="$(cd "$(dirname "$0")/cases" && pwd)"
PASSED=0
FAILED=0
FAILED_LIST=()

# 枚举 cases
mapfile -t ALL_CASES < <(ls "$CASES_DIR"/*.sh | sort)

# 过滤 --only
if [[ -n "$ONLY" ]]; then
    IFS=',' read -ra WANTED <<< "$ONLY"
    FILTERED=()
    for c in "${ALL_CASES[@]}"; do
        name=$(basename "$c" .sh)
        num="${name%%_*}"
        for w in "${WANTED[@]}"; do
            if [[ "$num" == "$w" ]]; then
                FILTERED+=("$c")
                break
            fi
        done
    done
    ALL_CASES=("${FILTERED[@]}")
fi

[[ ${#ALL_CASES[@]} -eq 0 ]] && { echo "No cases to run"; exit 1; }

echo "Running ${#ALL_CASES[@]} case(s) against $TARGET ..."

for case_file in "${ALL_CASES[@]}"; do
    name=$(basename "$case_file" .sh)
    if [[ -n "$VERBOSE" ]]; then
        echo "--- Running $name ---"
        if bash "$case_file"; then
            echo "[PASS] $name"
            ((PASSED++))
        else
            echo "[FAIL] $name"
            ((FAILED++))
            FAILED_LIST+=("$name")
        fi
    else
        output=$(bash "$case_file" 2>&1)
        rc=$?
        if [[ $rc -eq 0 ]]; then
            echo "[PASS] $name"
            ((PASSED++))
        else
            echo "[FAIL] $name"
            echo "$output" | sed 's/^/       /'
            ((FAILED++))
            FAILED_LIST+=("$name")
        fi
    fi
done

echo
echo "==================================="
echo "$PASSED/${#ALL_CASES[@]} passed"
if [[ $FAILED -gt 0 ]]; then
    echo "Failed cases:"
    for n in "${FAILED_LIST[@]}"; do echo "  - $n"; done
    exit 1
fi

exit 0
