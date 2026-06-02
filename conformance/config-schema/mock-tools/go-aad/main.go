// Package main — ks-conf-go-aad：AAD canonical 字节 mock-tool。
//
// 用法：
//
//	ks-conf-go-aad <mcp_server_id> <config_version> <fingerprint>
//
// 输出（stdout）：hex 小写字符串（无换行/空格），对应
// kstypes.AADCanonicalBytes 的字节串。用于 conformance 套件字节级互通校验。
package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: ks-conf-go-aad <mcp_server_id> <config_version> <fingerprint>")
		os.Exit(2)
	}
	mcpID := os.Args[1]
	// config_version 可能高达 2^63-1；用 ParseUint 支持到 2^64-1。
	ver, err := strconv.ParseUint(os.Args[2], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config_version 解析失败：%v\n", err)
		os.Exit(2)
	}
	fp := os.Args[3]

	aad := kstypes.AADCanonicalBytes(mcpID, ver, fp)
	fmt.Print(hex.EncodeToString(aad))
}
