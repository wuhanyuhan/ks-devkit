#!/usr/bin/env bash
# cases/16_encrypt_decrypt_ts_ts.sh — 互通矩阵：encrypt=ts / decrypt=ts。
#
# 共用流程见 lib.sh::run_encrypt_decrypt_combination：keygen → encrypt → decrypt
# → base64 解码 → 明文字节级还原断言。9 个 case 只传 enc/dec/case_num 三参数。
set -euo pipefail
source "$(dirname "$0")/../lib.sh"
run_encrypt_decrypt_combination ts ts 16
