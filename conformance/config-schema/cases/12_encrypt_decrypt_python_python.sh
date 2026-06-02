#!/usr/bin/env bash
# cases/12_encrypt_decrypt_python_python.sh — 互通矩阵：encrypt=python / decrypt=python。
#
# 共用流程见 lib.sh::run_encrypt_decrypt_combination：keygen → encrypt → decrypt
# → base64 解码 → 明文字节级还原断言。9 个 case 只传 enc/dec/case_num 三参数。
set -euo pipefail
source "$(dirname "$0")/../lib.sh"
run_encrypt_decrypt_combination py py 12
