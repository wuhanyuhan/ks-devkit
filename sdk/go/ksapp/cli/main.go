// ksapp CLI — Keystone MCP SDK 命令行工具。
//
// 用法：
//
//	ksapp config set --key=<k> --value=<v>     # 设置单字段配置（明文输入）
//	ksapp config set --file=<path>             # 从 YAML/JSON 文件批量导入
//	ksapp config show                          # 显示当前配置（敏感字段脱敏）
//	ksapp config reset                         # 清空配置（回到 unconfigured 状态）
//	ksapp pubkey                               # 显示当前 X25519 公钥 + 指纹
//	ksapp pubkey rotate [--print-only]         # 密钥轮换（--print-only 仅打印不落盘）
//	ksapp pubkey prune-old                     # 清除 .mcp-key.old（过渡期结束后）
//
// 依赖：sdk/go/ksapp/keystore（加解密原语 + 密钥加载/轮换）。
// 规范源：docs/config-schema.md。
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "config":
		configCmd(os.Args[2:])
	case "pubkey":
		pubkeyCmd(os.Args[2:])
	case "fetch-env":
		fetchEnvCmd(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `ksapp — Keystone MCP SDK CLI

用法:
  ksapp config set --key=<k> --value=<v>     # 设置单字段配置（明文）
  ksapp config set --file=<path>             # 从 YAML/JSON 文件批量导入
  ksapp config show                          # 显示当前配置（敏感字段脱敏）
  ksapp config reset                         # 清空配置（回到 unconfigured）
  ksapp pubkey                               # 显示当前 X25519 公钥和指纹
  ksapp pubkey rotate [--print-only]         # 密钥轮换（--print-only 不写文件）
  ksapp pubkey prune-old                     # 7 天过渡期结束后清除 .mcp-key.old
  ksapp fetch-env --gateway <url> --token <t> [--format dotenv|json|shell]
                                             # 从 keystone 拉本应用托管资源凭证`)
}

// exitErr 打印错误消息到 stderr 并退出码 1。
// 本 helper 用于 CLI 入口函数；业务 helper 优先返回 error 让入口统一处理。
func exitErr(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
