package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/keystore"
)

// pubkeyCmd 是 pubkey 子命令的路由入口。
// 无参 → pubkeyShow；有子命令 → 分派到 rotate / prune-old。
func pubkeyCmd(args []string) {
	if len(args) == 0 {
		pubkeyShow()
		return
	}
	switch args[0] {
	case "rotate":
		pubkeyRotate(args[1:])
	case "prune-old":
		pubkeyPruneOld()
	default:
		fmt.Fprintf(os.Stderr, "未知 pubkey 子命令: %s\n", args[0])
		os.Exit(2)
	}
}

// pubkeyShow 通过 keystore.Load 加载当前密钥，打印 source / fingerprint / pubkey
// （以及可选的 old fingerprint）。
func pubkeyShow() {
	ks, err := keystore.Load(nil)
	if err != nil {
		exitErr("keystore load: %v", err)
	}
	renderKeystore(os.Stdout, ks)
}

// renderKeystore 把 Keystore 的公钥信息渲染到 w，便于测试。
func renderKeystore(w io.Writer, ks *keystore.Keystore) {
	fmt.Fprintf(w, "source:      %s\n", ks.Source.String())
	fmt.Fprintf(w, "fingerprint: %s\n", ks.Primary.Fingerprint)
	fmt.Fprintf(w, "pubkey:      %s\n", base64.StdEncoding.EncodeToString(ks.Primary.Pubkey))
	if ks.Old != nil {
		fmt.Fprintf(w, "old_fingerprint: %s（过渡期）\n", ks.Old.Fingerprint)
	}
}

// pubkeyRotate 解析 --print-only flag 后调用 keystore.Rotate，打印结果。
func pubkeyRotate(args []string) {
	fs := flag.NewFlagSet("pubkey rotate", flag.ExitOnError)
	printOnly := fs.Bool("print-only", false, "env/Secret 模式推荐；只生成不写文件")
	_ = fs.Parse(args)

	r, err := keystore.Rotate(&keystore.RotateOptions{PrintOnly: *printOnly})
	if err != nil {
		exitErr("rotate: %v", err)
	}
	renderRotateResult(os.Stdout, r, *printOnly)
}

// renderRotateResult 把 RotateResult 渲染为运维友好的多行文本。
// printOnly = true → 提示运维搬旧密钥到 _OLD_B64；false → 打印已写入的文件清单。
func renderRotateResult(w io.Writer, r *keystore.RotateResult, printOnly bool) {
	fmt.Fprintln(w, "=== 新密钥对 ===")
	fmt.Fprintf(w, "KSAPP_MCP_PRIVKEY_B64=%s\n", r.NewPrivkeyB64)
	fmt.Fprintf(w, "pubkey (base64): %s\n", r.NewPubkeyB64)
	fmt.Fprintf(w, "fingerprint:     %s\n", r.Fingerprint)
	if printOnly {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "注意: 运维需把当前 PRIVKEY_B64 搬到 KSAPP_MCP_PRIVKEY_OLD_B64，")
		fmt.Fprintln(w, "      并把上面的新值写入 KSAPP_MCP_PRIVKEY_B64，然后 Rolling Update 重启。")
	} else {
		fmt.Fprintf(w, "\n已写入: %v\n", r.FilesWritten)
	}
}

// pubkeyPruneOld 清除 fallback 模式下的 .mcp-key.old。
// 空字符串 → 使用 keystore 包的默认路径（config/.mcp-key.old）。
func pubkeyPruneOld() {
	if err := keystore.PruneOld(""); err != nil {
		exitErr("prune-old: %v", err)
	}
	fmt.Println("已清除 .mcp-key.old")
}
