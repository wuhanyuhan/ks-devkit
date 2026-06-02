#!/usr/bin/env bash
# run.sh — 跑单个或多个 case，汇总输出。
#
# Usage:
#   ./run.sh                        # 跑全部
#   ./run.sh 01_aad_go_python_parity
#   ./run.sh --only=01,05 [--verbose]
#
# Exit codes:
#   0  全部通过
#   1  至少一个 case 失败
#   2  参数错误

set -uo pipefail

ONLY=""
VERBOSE=""
POSITIONAL=()

for arg in "$@"; do
    case "$arg" in
        --only=*) ONLY="${arg#*=}" ;;
        --verbose) VERBOSE="1" ;;
        --help|-h)
            sed -n '3,12p' "$0"
            exit 0
            ;;
        -*)
            echo "Unknown flag: $arg" >&2
            exit 2
            ;;
        *)
            POSITIONAL+=("$arg")
            ;;
    esac
done

export VERBOSE

HERE="$(cd "$(dirname "$0")" && pwd)"
CASES_DIR="$HERE/cases"

mapfile -t ALL_CASES < <(ls "$CASES_DIR"/*.sh | sort)

# 过滤逻辑：位置参数（精确匹配 basename）> --only=编号列表 > 默认全跑
if [[ ${#POSITIONAL[@]} -gt 0 ]]; then
    FILTERED=()
    for want in "${POSITIONAL[@]}"; do
        for c in "${ALL_CASES[@]}"; do
            name=$(basename "$c" .sh)
            if [[ "$name" == "$want" ]]; then
                FILTERED+=("$c")
            fi
        done
    done
    ALL_CASES=("${FILTERED[@]}")
elif [[ -n "$ONLY" ]]; then
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

if [[ ${#ALL_CASES[@]} -eq 0 ]]; then
    echo "No cases to run"
    exit 1
fi

PASSED=0
FAILED=0
FAILED_LIST=()

echo "Running ${#ALL_CASES[@]} config-schema conformance case(s) ..."
for case_file in "${ALL_CASES[@]}"; do
    name=$(basename "$case_file" .sh)
    if [[ -n "$VERBOSE" ]]; then
        echo "--- $name ---"
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
