#!/usr/bin/env bash
# run.sh — 公开文档防漂移 lint（纯 grep 级，零依赖）。
#
# 命中任一即判定失败（exit 1），用于 CI / 提交前兜底，防止公开文档与代码契约再次漂移：
#   (a) 公开文档 / 模板出现非法 `--type=service`（合法 enum 只有 app/squad/agent/skill）
#   (b) 已退役的死链路径（sdk/go/ksapp/README.md 等）重新出现
#   (c) Go SDK README 用 json.RawMessage 描述 ToolWithSchema（真实签名是 map[string]any）
#   (d) 公开文档里的相对 .md 链接目标必须存在（跳过 http(s) 外链、协议相对链接与纯 #anchor）
#
# Lint 面（公开开发者面）：根 README / CONTRIBUTING / AGENTS、docs/ 下除 superpowers 外的 .md、
# 三个 SDK README（sdk/{go,python,typescript}/README.md），以及 internal/resources/templates/。
# 明确不 lint docs/superpowers/——本地草稿目录，可能含会触发上述规则的字样。
#
# Usage:
#   ./run.sh            # 跑全部检查
#   ./run.sh --verbose  # 额外打印被检查的文件清单
#   ./run.sh --help
#
# Exit codes:
#   0  无违例
#   1  至少一处违例
#   2  参数错误

set -uo pipefail

VERBOSE=""
for arg in "$@"; do
    case "$arg" in
        --verbose) VERBOSE="1" ;;
        --help|-h) sed -n '2,22p' "$0"; exit 0 ;;
        *) echo "Unknown flag: $arg" >&2; exit 2 ;;
    esac
done

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT" || { echo "cannot cd to repo root" >&2; exit 2; }

# ---- 收集 lint 面 ----
# 公开 markdown 散文面：根 3 篇 + 三个 SDK README + docs/ 下除 superpowers 外的 .md。
# （SDK README 纳入是因为检查 (c) 针对 Go README，且 P3 在三个 SDK README 里新增了指向
#  sdk-api-reference 的相对链接，需一并防死链。）
LINT_DOCS=()
for f in README.md CONTRIBUTING.md AGENTS.md \
         sdk/go/README.md sdk/python/README.md sdk/typescript/README.md; do
    [[ -f "$f" ]] && LINT_DOCS+=("$f")
done
while IFS= read -r f; do
    [[ -n "$f" ]] && LINT_DOCS+=("$f")
done < <(find docs -name '*.md' -not -path '*/superpowers/*' 2>/dev/null | sort)

# 模板面：只查非法 type 词汇（模板里的 .md 链接都是 GitHub 外链，不做存在性校验）。
TEMPLATE_FILES=()
while IFS= read -r f; do
    [[ -n "$f" ]] && TEMPLATE_FILES+=("$f")
done < <(find internal/resources/templates -type f 2>/dev/null | sort)

GO_README="sdk/go/README.md"

if [[ ${#LINT_DOCS[@]} -eq 0 ]]; then
    echo "No docs to lint (are you in the repo root?)" >&2
    exit 2
fi

if [[ -n "$VERBOSE" ]]; then
    echo "Lint docs (${#LINT_DOCS[@]}):"; printf '  %s\n' "${LINT_DOCS[@]}"
    echo "Template files: ${#TEMPLATE_FILES[@]}"
    echo
fi

FAILED=0

echo "Running docs-lint ..."

# ---- (a) 非法 --type=service（合法 enum：app/squad/agent/skill）----
if hits=$(grep -nF -e '--type=service' "${LINT_DOCS[@]}" "${TEMPLATE_FILES[@]}" 2>/dev/null); then
    echo "[FAIL] (a) 出现非法 --type=service（合法 enum：app/squad/agent/skill）"
    printf '%s\n' "$hits" | sed 's/^/       /'
    FAILED=1
else
    echo "[PASS] (a) 无非法 --type=service"
fi

# ---- (b) 已退役死链路径 ----
if hits=$(grep -nF -e 'sdk/go/ksapp/README.md' -e 'sdk/python/src/ks_app/README.md' "${LINT_DOCS[@]}" 2>/dev/null); then
    echo "[FAIL] (b) 出现已退役死链路径（应为 sdk/go/README.md、sdk/python/README.md）"
    printf '%s\n' "$hits" | sed 's/^/       /'
    FAILED=1
else
    echo "[PASS] (b) 无已退役死链路径"
fi

# ---- (c) Go README 的 json.RawMessage 签名漂移 ----
if [[ -f "$GO_README" ]] && hits=$(grep -nF -e 'json.RawMessage' "$GO_README" 2>/dev/null); then
    echo "[FAIL] (c) Go README 用 json.RawMessage 描述 schema（真实签名是 map[string]any）"
    printf '%s\n' "$hits" | sed 's/^/       /'
    FAILED=1
else
    echo "[PASS] (c) Go README 无 json.RawMessage"
fi

# ---- (d) 相对 .md 链接存在性（跳过外链 / 协议相对 / 纯 #anchor）----
BROKEN=()
for f in "${LINT_DOCS[@]}"; do
    dir=$(dirname "$f")
    while IFS= read -r tgt; do
        [[ -z "$tgt" ]] && continue
        case "$tgt" in
            http://*|https://*|//*|mailto:*|\#*) continue ;;
        esac
        path="${tgt%% *}"      # 丢掉 ](path "title") 里的 title
        path="${path%%#*}"     # 丢掉 #anchor
        path="${path%%\?*}"    # 丢掉 ?query
        [[ -z "$path" ]] && continue
        case "$path" in
            *.md) ;;           # 只校验 .md 目标
            *) continue ;;
        esac
        if [[ "$path" == /* ]]; then
            resolved="$ROOT$path"
        else
            resolved="$dir/$path"
        fi
        [[ -f "$resolved" ]] || BROKEN+=("$f -> $tgt")
    done < <(grep -oE '\]\([^)]+\)' "$f" 2>/dev/null | sed -E 's/^\]\(//; s/\)$//')
done
if [[ ${#BROKEN[@]} -gt 0 ]]; then
    echo "[FAIL] (d) 相对 .md 链接存在死链"
    printf '       %s\n' "${BROKEN[@]}"
    FAILED=1
else
    echo "[PASS] (d) 相对 .md 链接全部可达"
fi

echo
echo "==================================="
if [[ $FAILED -ne 0 ]]; then
    echo "docs-lint: FAIL"
    exit 1
fi
echo "docs-lint: PASS"
exit 0
