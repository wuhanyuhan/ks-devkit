// ksapp fetch-env：从 keystone 拉本应用被分配的托管资源凭证。
//
// 用法：
//
//	ksapp fetch-env --gateway $KS_GATEWAY_URL --token $KS_APP_TOKEN
//	ksapp fetch-env --gateway ... --token ... --format json
//	ksapp fetch-env --gateway ... --token ... --format shell
//
// dotenv（默认）输出用 BEGIN/END marker 注释包夹，方便脚本幂等替换。
// json 输出 sort_keys 的对象，便于 diff。
// shell 输出每行 export KEY="value"，双引号内转义 \\ " $ ` 防展开。
//
// spec: managed resources fetch-env contract
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/keystoneclient"
)

// dotenvMarkerBeginFmt / dotenvMarkerEnd 跟 Python SDK 字节级一致，
// 跨语言写出的 .env.local 互相兼容。
const (
	dotenvMarkerBeginFmt = "# ─── BEGIN KEYSTONE MANAGED (generated %s) ───"
	dotenvMarkerEnd      = "# ─── END KEYSTONE MANAGED ───"
)

// dotenvQuoteChars 触发加双引号的字符集：空格、tab、#、双引号、反斜杠。
var dotenvQuoteChars = []string{" ", "\t", "#", `"`, `\`}

// shellEscapeChars 双引号内 shell 需转义的字符：反斜杠、双引号、$、反引号。
// 注意顺序：必须 `\\` 先转，否则后续转义引入的 \ 会被再次 escape。
var shellEscapeChars = []string{`\`, `"`, `$`, "`"}

// fetcher 抽象 SelfClient.FetchEnv 调用，便于测试注入 stub。
// 默认实现 realFetcher 走真 HTTP。
type fetcher interface {
	FetchEnv(ctx context.Context) (map[string]string, error)
}

// fetchEnvCmd CLI 入口（main.go switch 分派到这里）。
func fetchEnvCmd(args []string) {
	fs := flag.NewFlagSet("fetch-env", flag.ExitOnError)
	gateway := fs.String("gateway", "", "Keystone 网关地址（必填）")
	token := fs.String("token", "", "KS_APP_TOKEN 凭证（必填）")
	format := fs.String("format", "dotenv", "输出格式: dotenv / json / shell")
	_ = fs.Parse(args)

	if *gateway == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "--gateway 与 --token 都必填")
		os.Exit(2)
	}

	client := keystoneclient.New(*gateway, *token)
	if err := doFetchEnv(os.Stdout, client, *format); err != nil {
		exitErr("%v", err)
	}
}

// doFetchEnv 可测 helper：调 fetcher → 选 format → 渲染到 w。
// 失败返回 error，由 CLI 入口转 exitErr 退出。
func doFetchEnv(w io.Writer, f fetcher, format string) error {
	env, err := f.FetchEnv(context.Background())
	if err != nil {
		return fmt.Errorf("fetch keystone env failed: %w", err)
	}
	switch format {
	case "dotenv":
		renderDotenv(w, env, time.Now().UTC())
	case "json":
		return renderJSON(w, env)
	case "shell":
		renderShell(w, env)
	default:
		return fmt.Errorf("unknown format: %q (choose dotenv/json/shell)", format)
	}
	return nil
}

// renderDotenv 输出 BEGIN/END marker 包夹的 KEY=value 行，key 字母序。
func renderDotenv(w io.Writer, env map[string]string, ts time.Time) {
	fmt.Fprintln(w, fmt.Sprintf(dotenvMarkerBeginFmt, ts.Format("2006-01-02T15:04:05Z")))
	keys := sortedKeys(env)
	for _, k := range keys {
		fmt.Fprintln(w, k+"="+quoteDotenv(env[k]))
	}
	fmt.Fprintln(w, dotenvMarkerEnd)
}

// renderJSON 输出 sort_keys + indent 2 的 JSON object。
func renderJSON(w io.Writer, env map[string]string) error {
	// 用 json.MarshalIndent + 一份排序 map 保证 byte-equivalent
	keys := sortedKeys(env)
	ordered := make(map[string]string, len(env))
	for _, k := range keys {
		ordered[k] = env[k]
	}
	// encoding/json 对 map 已经按 key 字母序输出，无需额外排序
	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return err
	}
	_, _ = w.Write(data)
	_, _ = w.Write([]byte{'\n'})
	return nil
}

// renderShell 输出 export KEY="value" 行，双引号内转义防展开。
func renderShell(w io.Writer, env map[string]string) {
	keys := sortedKeys(env)
	for _, k := range keys {
		fmt.Fprintf(w, "export %s=\"%s\"\n", k, escapeShellDoubleQuoted(env[k]))
	}
}

// sortedKeys 返回 env map 按字母序排好的 key slice。
func sortedKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// quoteDotenv 简单值不加引号；含特殊字符（DOTENV_QUOTE_CHARS）才双引号 + 转义 \\ "。
func quoteDotenv(v string) string {
	needsQuote := false
	for _, c := range dotenvQuoteChars {
		if strings.Contains(v, c) {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return v
	}
	// 先转 \ 再转 "（顺序敏感）
	escaped := strings.ReplaceAll(v, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// escapeShellDoubleQuoted 转义双引号 shell 字符串内的特殊字符。
// 顺序：反斜杠先，否则后续转义引入的 \ 会被二次 escape。
func escapeShellDoubleQuoted(v string) string {
	out := v
	for _, c := range shellEscapeChars {
		out = strings.ReplaceAll(out, c, `\`+c)
	}
	return out
}
