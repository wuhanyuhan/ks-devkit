// Package main — ks-conf-go-fingerprint：公钥指纹 mock-tool。
//
// 用法：
//
//	ks-conf-go-fingerprint <pubkey_hex>
//
// 输入：32 字节 X25519 公钥的 hex（64 字符，大小写不限）。
// 输出（stdout）：spec-v1 §4.2 fingerprint 字符串（8 段 × 4 hex × ':'），
// 例如 `6668:7aad:f862:bd77:6c8f:c18b:8e9f:8e20`。
//
// 退出码：
//   - 0：成功
//   - 2：用法错 / hex 解析错 / pubkey 长度错（三端对齐；避免 kstypes.Fingerprint
//     直接 panic 污染 stderr、退出码意外）
package main

import (
	"encoding/hex"
	"fmt"
	"os"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ks-conf-go-fingerprint <pubkey_hex>")
		os.Exit(2)
	}
	pub, err := hex.DecodeString(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "pubkey hex 解析失败：%v\n", err)
		os.Exit(2)
	}
	// 预检长度：kstypes.Fingerprint 对 len != 32 会 panic，这在 CLI 上下文下
	// 会输出 goroutine stack 污染 stderr 且退出码变为 2（Go runtime 默认）。
	// 三端为了对齐，这里显式校验 + 用统一的 "usage error" 退出码 2。
	if len(pub) != 32 {
		fmt.Fprintf(os.Stderr, "pubkey 长度 = %d, 期望 32\n", len(pub))
		os.Exit(2)
	}
	fmt.Print(kstypes.Fingerprint(pub))
}
