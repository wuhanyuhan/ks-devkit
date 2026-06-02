#!/usr/bin/env bash
# cases/10_encrypt_decrypt_go_ts.sh — 互通矩阵：encrypt=go / decrypt=ts。
#
# 共用流程见 lib.sh::run_encrypt_decrypt_combination：keygen → encrypt → decrypt
# → base64 解码 → 明文字节级还原断言。9 个 case 只传 enc/dec/case_num 三参数。
set -euo pipefail
source "$(dirname "$0")/../lib.sh"
run_encrypt_decrypt_combination go ts 10
