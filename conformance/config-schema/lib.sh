#!/usr/bin/env bash
# conformance/config-schema/lib.sh — 共享辅助（字节比对 + mock-tool 构建）。
#
# cases/ 下每个脚本开头都 source 它：
#   source "$(dirname "$0")/../lib.sh"
#
# 三端 mock-tool 的字节互通验证不需要启 server，只需要把 testvectors 里的输入
# 分发给三个子进程，然后用 bytes_eq / canonical_json_eq 比较 stdout。

set -euo pipefail

CONF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TESTVECTORS="$CONF_DIR/testvectors.json"
REPO_ROOT="$(cd "$CONF_DIR/../.." && pwd)"
PY_SDK_SRC="$REPO_ROOT/sdk/python/src"

# Python 侧自动把 SDK src 加入 PYTHONPATH，免去用户手动 pip install / venv 激活。
export PYTHONPATH="${PYTHONPATH:-}:$PY_SDK_SRC"

# TS runtime 优先 Bun，fallback tsx。每个 ts-* 目录都是独立项目。
TS_RUNTIME="${TS_RUNTIME:-}"
if [[ -z "$TS_RUNTIME" ]]; then
    if command -v bun >/dev/null 2>&1; then
        TS_RUNTIME="bun"
    elif command -v tsx >/dev/null 2>&1; then
        TS_RUNTIME="tsx"
    elif command -v npx >/dev/null 2>&1; then
        TS_RUNTIME="npx tsx"
    else
        echo "ERROR: 需要 bun 或 tsx 跑 TS mock-tool（参考 SPEC.md『TS 运行时』）" >&2
        exit 3
    fi
fi
export TS_RUNTIME

# ----- 打印 -----

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

pass() {
    echo "PASS: $*"
}

info() {
    [[ -n "${VERBOSE:-}" ]] && echo "  [info] $*" >&2 || true
}

# ----- 向量加载 -----

# load_vectors <section> — 输出指定 section 的 JSON array（一行一条，jq -c）
load_vectors() {
    jq -c ".${1}[]" "$TESTVECTORS"
}

# ----- 字节 / 字符串比对 -----

# bytes_eq <hex1> <hex2> — 比较两个 hex 字符串（先归一化：去空白、转小写）
bytes_eq() {
    local a b
    a=$(echo "$1" | tr -d '[:space:]' | tr 'A-F' 'a-f')
    b=$(echo "$2" | tr -d '[:space:]' | tr 'A-F' 'a-f')
    [[ "$a" == "$b" ]]
}

# str_eq <s1> <s2> — 直接字符串比对（去首尾换行）
str_eq() {
    local a b
    a=$(printf '%s' "$1" | tr -d '\r\n')
    b=$(printf '%s' "$2" | tr -d '\r\n')
    [[ "$a" == "$b" ]]
}

# canonical_json_eq <json1> <json2> — 用 jq 排键 + compact 后字节比对。
# 参数是 JSON 字符串；输出为 0/1 通过 [[ ]] 语义判断。
canonical_json_eq() {
    local a b
    a=$(echo "$1" | jq -Sc .) || return 1
    b=$(echo "$2" | jq -Sc .) || return 1
    [[ "$a" == "$b" ]]
}

# ----- Mock-tool 构建 -----

# build_go_tool <name> — cd 到 mock-tools/<name>/ 跑 go build，输出二进制路径
build_go_tool() {
    local name="$1"
    local out="/tmp/ks-conf-$name"
    if [[ -z "${SKIP_GO_BUILD:-}" ]]; then
        (cd "$CONF_DIR/mock-tools/$name" && go build -o "$out" . >&2)
    fi
    echo "$out"
}

# run_ts_tool <dir> [args...] — 统一 TS 调用入口
# 按 TS_RUNTIME 字面分发：bun 走 `bun run`；其他（tsx / npx tsx / node --loader tsx）直接传入口。
# 避免原先 `A || B` fallback 的副作用：reject case 本来就退出非零（rc=10/11），
# || 会误触发第二次执行让 TS 工具跑 2 次。
run_ts_tool() {
    local dir="$1"
    shift
    local entry="$CONF_DIR/mock-tools/$dir/index.ts"
    if [[ ! -f "$entry" ]]; then
        fail "TS mock-tool 入口不存在：$entry"
    fi
    # shellcheck disable=SC2086
    case "$TS_RUNTIME" in
        bun|*/bun) $TS_RUNTIME run "$entry" "$@" ;;
        *)         $TS_RUNTIME "$entry" "$@" ;;
    esac
}

# run_py_tool <file> [args...] — 统一 Python 调用入口
run_py_tool() {
    local script="$CONF_DIR/mock-tools/$1"
    shift
    python3 "$script" "$@"
}

# ----- 加解密 helper（互通 case 复用）-----
#
# Mock-tool runner 以 stdin JSON / stdout JSON 约束统一；下面三个 helper 把语言
# 名（go / py / ts）映射到具体的 runner 调用，屏蔽 cases 08-16 的重复 boilerplate。
#
# 退出码语义（三语言一致）：
#   0    成功
#   2    用法错 / JSON 解析错
#   20   （decrypt）AAD 重算不一致（本 mock-tool 未实现该分支）
#   21   长度 / base64 解码错（pubkey / privkey / nonce / ct 等）
#   22   （decrypt）AES-GCM tag 校验失败（含 aad 不一致 / 密文被改）

# run_keygen <lang> — lang ∈ {go, py, ts}，stdout 输出 keygen JSON
run_keygen() {
    case "$1" in
        go) "$(build_go_tool go-keygen)" ;;
        py) run_py_tool py-keygen.py ;;
        ts) run_ts_tool ts-keygen ;;
        *)  fail "run_keygen: 未知语言 $1" ;;
    esac
}

# run_encrypt <lang> — lang ∈ {go, py, ts}，stdin 读 encrypt 请求 JSON
run_encrypt() {
    case "$1" in
        go) "$(build_go_tool go-encrypt)" ;;
        py) run_py_tool py-encrypt.py ;;
        ts) run_ts_tool ts-encrypt ;;
        *)  fail "run_encrypt: 未知语言 $1" ;;
    esac
}

# run_decrypt <lang> — lang ∈ {go, py, ts}，stdin 读 decrypt 请求 JSON
run_decrypt() {
    case "$1" in
        go) "$(build_go_tool go-decrypt)" ;;
        py) run_py_tool py-decrypt.py ;;
        ts) run_ts_tool ts-decrypt ;;
        *)  fail "run_decrypt: 未知语言 $1" ;;
    esac
}

# run_encrypt_decrypt_combination <enc_lang> <dec_lang> <case_num>
# 抽象 9 个 3×3 矩阵 case 的公共流程（keygen → encrypt → decrypt → assert 还原）。
# enc_lang / dec_lang ∈ {go, py, ts}；case_num 是 case ID 两位数前缀（"08"..."16"）。
# pass 字符串按 <case_num>_encrypt_decrypt_<pretty_enc>_<pretty_dec> 拼装，
# 其中 `py → python` 以保持与文件命名惯例一致。
run_encrypt_decrypt_combination() {
    local enc_lang="$1"
    local dec_lang="$2"
    local case_num="$3"

    local kp_json mcp_priv mcp_pub fp
    kp_json=$(run_keygen "$enc_lang")
    mcp_priv=$(echo "$kp_json" | jq -r .privkey_b64)
    mcp_pub=$(echo "$kp_json"  | jq -r .pubkey_b64)
    fp=$(echo "$kp_json"       | jq -r .fingerprint)

    local plaintext pt_b64
    plaintext='{"api_key":"sk-conformance-test-'"${case_num}"'","region":"cn","expires_at":1735689600}'
    pt_b64=$(echo -n "$plaintext" | base64 -w0)

    local encrypted
    encrypted=$(jq -cn \
        --arg pk "$mcp_pub" \
        --arg mid "ks-mcp-conf-${case_num}" \
        --arg fp "$fp" \
        --arg pt "$pt_b64" \
        '{mcp_pubkey_b64:$pk, mcp_server_id:$mid, config_version:1, fingerprint:$fp, plaintext_b64:$pt}' \
      | run_encrypt "$enc_lang")

    info "encrypted: $(echo "$encrypted" | jq -c .)"

    local dec_in decrypted decrypted_pt
    dec_in=$(echo "$encrypted" | jq --arg priv "$mcp_priv" \
        '{mcp_privkey_b64:$priv,
          ephemeral_pubkey:.ephemeral_pubkey,
          nonce:.nonce,
          aad_canonical:.aad_canonical,
          ciphertext:.ciphertext}')

    decrypted=$(echo "$dec_in" | run_decrypt "$dec_lang")
    decrypted_pt=$(echo "$decrypted" | jq -r .plaintext_b64 | base64 -d)

    if [[ "$decrypted_pt" != "$plaintext" ]]; then
        fail "plaintext mismatch:
  want: $plaintext
  got:  $decrypted_pt"
    fi
    local enc_pretty=${enc_lang/py/python}
    local dec_pretty=${dec_lang/py/python}
    pass "${case_num}_encrypt_decrypt_${enc_pretty}_${dec_pretty}"
}
