package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/keystore"
)

// chdirTempDir 切换到 t.TempDir() 并在测试结束后恢复原目录。
// 所有涉及 config/ 相对路径的测试都用它来隔离副作用。
func chdirTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})
	return dir
}

// TestConfigSetAndShow 设置一个 api_key 字段后读回，确认加密 → 解密闭环正确，
// 以及 config/.status 写入 via_cli。
func TestConfigSetAndShow(t *testing.T) {
	dir := chdirTempDir(t)

	configSet([]string{"--key=api_key", "--value=sk-xxx"})

	cfg, err := loadCurrentConfigMap()
	if err != nil {
		t.Fatalf("loadCurrentConfigMap: %v", err)
	}
	if cfg["api_key"] != "sk-xxx" {
		t.Errorf("api_key = %v, want sk-xxx", cfg["api_key"])
	}
	status, err := os.ReadFile(filepath.Join(dir, "config/.status"))
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if string(status) != statusViaCLI {
		t.Errorf("status = %q, want %q", status, statusViaCLI)
	}
}

// TestConfigReset 确认 reset 删除 enc 文件 + 写 unconfigured 状态。
func TestConfigReset(t *testing.T) {
	dir := chdirTempDir(t)

	if err := os.MkdirAll("config", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config/mcp-config.enc", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	configReset()

	if _, err := os.Stat("config/mcp-config.enc"); !os.IsNotExist(err) {
		t.Error("reset 后 mcp-config.enc 应不存在")
	}
	status, err := os.ReadFile(filepath.Join(dir, "config/.status"))
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if string(status) != statusUnconfigured {
		t.Errorf("status = %q, want %q", status, statusUnconfigured)
	}
}

// TestConfigSet_FromJSONFile 从 JSON 文件批量导入配置。
func TestConfigSet_FromJSONFile(t *testing.T) {
	chdirTempDir(t)

	// 准备 JSON 源
	src := "cfg.json"
	if err := os.WriteFile(src, []byte(`{"api_key":"sk-json","endpoint":"https://example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := doConfigSetFromFile(src); err != nil {
		t.Fatalf("doConfigSetFromFile: %v", err)
	}

	cfg, err := loadCurrentConfigMap()
	if err != nil {
		t.Fatalf("loadCurrentConfigMap: %v", err)
	}
	if cfg["api_key"] != "sk-json" {
		t.Errorf("api_key = %v, want sk-json", cfg["api_key"])
	}
	if cfg["endpoint"] != "https://example.com" {
		t.Errorf("endpoint = %v, want https://example.com", cfg["endpoint"])
	}
}

// TestConfigSet_FromYAMLFile 从 YAML 文件批量导入配置。
func TestConfigSet_FromYAMLFile(t *testing.T) {
	chdirTempDir(t)

	src := "cfg.yaml"
	yamlBody := "api_key: sk-yaml\nendpoint: https://yaml.example\ntimeout_ms: 3000\n"
	if err := os.WriteFile(src, []byte(yamlBody), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := doConfigSetFromFile(src); err != nil {
		t.Fatalf("doConfigSetFromFile: %v", err)
	}

	cfg, err := loadCurrentConfigMap()
	if err != nil {
		t.Fatalf("loadCurrentConfigMap: %v", err)
	}
	if cfg["api_key"] != "sk-yaml" {
		t.Errorf("api_key = %v, want sk-yaml", cfg["api_key"])
	}
	if cfg["endpoint"] != "https://yaml.example" {
		t.Errorf("endpoint = %v, want https://yaml.example", cfg["endpoint"])
	}
	// 经过 JSON roundtrip (json.Marshal + Encrypt + Decrypt + json.Unmarshal)，
	// 数字字段会变 float64（Go JSON 标准行为）；只验证值等价即可。
	if got, ok := cfg["timeout_ms"].(float64); !ok || got != 3000 {
		t.Errorf("timeout_ms = %v (type %T), want 3000 (float64)", cfg["timeout_ms"], cfg["timeout_ms"])
	}
}

// TestConfigSet_FromJSONFile_BadFormat 解析错误应返回非 nil error。
func TestConfigSet_FromJSONFile_BadFormat(t *testing.T) {
	chdirTempDir(t)

	src := "bad.json"
	if err := os.WriteFile(src, []byte(`{not valid json`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := doConfigSetFromFile(src)
	if err == nil {
		t.Fatal("期望 JSON 解析失败，却 err == nil")
	}
	if !strings.Contains(err.Error(), "JSON 解析失败") {
		t.Errorf("err = %v, 期望含 'JSON 解析失败'", err)
	}
}

// TestRenderConfig_SensitiveRedaction 验证 renderConfig 对敏感字段脱敏。
func TestRenderConfig_SensitiveRedaction(t *testing.T) {
	t.Parallel()

	cfg := map[string]any{
		"api_key":  "sk-1234567890",
		"secret":   "super-secret-value",
		"token":    "tk-abcdef",
		"password": "hunter2",
		"endpoint": "https://example.com",
	}
	var buf bytes.Buffer
	renderConfig(&buf, cfg)
	out := buf.String()

	// 敏感字段不应出现原文
	for k, v := range map[string]string{
		"api_key":  "sk-1234567890",
		"secret":   "super-secret-value",
		"token":    "tk-abcdef",
		"password": "hunter2",
	} {
		if strings.Contains(out, v) {
			t.Errorf("%s 原文 %q 不应出现在输出中: %s", k, v, out)
		}
	}
	// 敏感字段的 key 仍应出现（带 "***" 脱敏标记）
	for _, needle := range []string{"api_key", "secret", "token", "password", "***", "已脱敏"} {
		if !strings.Contains(out, needle) {
			t.Errorf("输出应含 %q: %s", needle, out)
		}
	}
	// 非敏感字段应原文展示
	if !strings.Contains(out, "https://example.com") {
		t.Errorf("endpoint 原文应出现: %s", out)
	}
}

// TestRenderConfig_Nil 验证未配置时打印 "(未配置)"。
func TestRenderConfig_Nil(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	renderConfig(&buf, nil)
	if !strings.Contains(buf.String(), "(未配置)") {
		t.Errorf("输出应含 '(未配置)': %q", buf.String())
	}
}

// TestIsSensitiveKey 单测敏感字段关键字匹配。
func TestIsSensitiveKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		k    string
		want bool
	}{
		{"api_key", true},
		{"API_KEY", true},
		{"MySecret", true},
		{"auth_token", true},
		{"password", true},
		{"api_endpoint", true}, // "api" 子串命中
		{"endpoint", false},
		{"timeout_ms", false},
		{"name", false},
	}
	for _, c := range cases {
		got := isSensitiveKey(c.k)
		if got != c.want {
			t.Errorf("isSensitiveKey(%q) = %v, want %v", c.k, got, c.want)
		}
	}
}

// TestTailN 验证末尾字符截取。
func TestTailN(t *testing.T) {
	t.Parallel()

	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"sk-1234567890", 4, "7890"},
		{"abc", 4, "abc"},
		{"", 4, ""},
		{"hunter2", 4, "ter2"},
	}
	for _, c := range cases {
		got := tailN(c.s, c.n)
		if got != c.want {
			t.Errorf("tailN(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}

// TestPubkeyShow_RendersFromKeystore 预埋 fallback 文件后调 renderKeystore 确认输出格式。
func TestPubkeyShow_RendersFromKeystore(t *testing.T) {
	// 清理可能影响 Load 优先级 1（env）的环境变量
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", "")
	t.Setenv("KSAPP_MCP_PRIVKEY_OLD_B64", "")
	chdirTempDir(t)

	ks, err := keystore.Load(nil)
	if err != nil {
		t.Fatalf("keystore.Load: %v", err)
	}
	if ks.Primary == nil {
		t.Fatal("Primary 不应为 nil")
	}

	var buf bytes.Buffer
	renderKeystore(&buf, ks)
	out := buf.String()
	if !strings.Contains(out, "source:") {
		t.Errorf("输出应含 'source:': %s", out)
	}
	if !strings.Contains(out, "fingerprint:") {
		t.Errorf("输出应含 'fingerprint:': %s", out)
	}
	if !strings.Contains(out, "pubkey:") {
		t.Errorf("输出应含 'pubkey:': %s", out)
	}
	if !strings.Contains(out, ks.Primary.Fingerprint) {
		t.Errorf("输出应含 fingerprint 值 %q: %s", ks.Primary.Fingerprint, out)
	}
}

// TestPubkeyRotate_PrintOnly 验证 print-only 不落盘新对，且 fallback 文件保持原样。
func TestPubkeyRotate_PrintOnly(t *testing.T) {
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", "")
	t.Setenv("KSAPP_MCP_PRIVKEY_OLD_B64", "")
	chdirTempDir(t)

	// 先让 keystore 生成 fallback 文件
	ksBefore, err := keystore.Load(nil)
	if err != nil {
		t.Fatalf("keystore.Load: %v", err)
	}
	fpBefore := ksBefore.Primary.Fingerprint

	// 快照 fallback 文件内容
	before, err := os.ReadFile("config/.mcp-key")
	if err != nil {
		t.Fatalf("读取 fallback 文件: %v", err)
	}

	// print-only 轮换
	r, err := keystore.Rotate(&keystore.RotateOptions{PrintOnly: true})
	if err != nil {
		t.Fatalf("keystore.Rotate: %v", err)
	}
	if r.NewPrivkeyB64 == "" || r.NewPubkeyB64 == "" || r.Fingerprint == "" {
		t.Errorf("RotateResult 字段不应为空: %+v", r)
	}
	if len(r.FilesWritten) != 0 {
		t.Errorf("print-only 不应写文件，FilesWritten = %v", r.FilesWritten)
	}
	if r.Fingerprint == fpBefore {
		t.Errorf("新指纹不应与旧指纹相等（概率碰撞视为 bug）")
	}

	// fallback 文件内容未被替换
	after, err := os.ReadFile("config/.mcp-key")
	if err != nil {
		t.Fatalf("读取 fallback 文件: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("print-only 后 fallback 文件不应被改写")
	}

	// 测渲染
	var buf bytes.Buffer
	renderRotateResult(&buf, r, true)
	out := buf.String()
	if !strings.Contains(out, "KSAPP_MCP_PRIVKEY_B64=") {
		t.Errorf("print-only 输出应含 KSAPP_MCP_PRIVKEY_B64=: %s", out)
	}
	if !strings.Contains(out, r.Fingerprint) {
		t.Errorf("print-only 输出应含 fingerprint: %s", out)
	}
}

// TestPubkeyRotate_FileMode 验证文件模式轮换：旧 primary 搬到 .old，新对写 primary，
// FilesWritten 回显两个路径。
func TestPubkeyRotate_FileMode(t *testing.T) {
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", "")
	t.Setenv("KSAPP_MCP_PRIVKEY_OLD_B64", "")
	chdirTempDir(t)

	// 首次 Load 生成 fallback primary
	ks1, err := keystore.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	oldFp := ks1.Primary.Fingerprint

	// 文件模式轮换（PrintOnly = false）
	r, err := keystore.Rotate(nil)
	if err != nil {
		t.Fatalf("keystore.Rotate: %v", err)
	}
	if len(r.FilesWritten) != 2 {
		t.Errorf("文件模式 FilesWritten 应为 2 项，实际 %v", r.FilesWritten)
	}

	// .mcp-key.old 应存在
	if _, err := os.Stat("config/.mcp-key.old"); err != nil {
		t.Errorf(".mcp-key.old 应存在: %v", err)
	}

	// 重新 Load 确认 Primary 指纹变了，Old 指纹 == oldFp
	ks2, err := keystore.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if ks2.Primary.Fingerprint == oldFp {
		t.Error("rotate 后 Primary 指纹不应等于旧指纹")
	}
	if ks2.Old == nil {
		t.Fatal("rotate 后 Old 不应为 nil")
	}
	if ks2.Old.Fingerprint != oldFp {
		t.Errorf("Old 指纹 = %q, want %q", ks2.Old.Fingerprint, oldFp)
	}
}

// TestPubkeyPruneOld 先创 config/.mcp-key.old 再 PruneOld("") 确认删除。
func TestPubkeyPruneOld(t *testing.T) {
	chdirTempDir(t)

	// 预埋 .old 文件
	if err := os.MkdirAll("config", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config/.mcp-key.old", []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := keystore.PruneOld(""); err != nil {
		t.Fatalf("PruneOld: %v", err)
	}
	if _, err := os.Stat("config/.mcp-key.old"); !os.IsNotExist(err) {
		t.Errorf("PruneOld 后 .mcp-key.old 应不存在: err = %v", err)
	}
}

// TestRenderRotateResult_FileMode 验证文件模式输出包含 "已写入:"。
func TestRenderRotateResult_FileMode(t *testing.T) {
	t.Parallel()

	r := &keystore.RotateResult{
		NewPrivkeyB64: "priv-b64-dummy",
		NewPubkeyB64:  "pub-b64-dummy",
		Fingerprint:   "fp:sha256:dummy",
		FilesWritten:  []string{"config/.mcp-key.old", "config/.mcp-key"},
	}
	var buf bytes.Buffer
	renderRotateResult(&buf, r, false)
	out := buf.String()
	if !strings.Contains(out, "已写入:") {
		t.Errorf("文件模式输出应含 '已写入:': %s", out)
	}
	if !strings.Contains(out, "config/.mcp-key") {
		t.Errorf("文件模式输出应含写入路径: %s", out)
	}
}

// TestWriteConfigStatus_CreatesDir 验证 writeConfigStatus 自动创建 config 目录。
func TestWriteConfigStatus_CreatesDir(t *testing.T) {
	chdirTempDir(t)

	// 确认 config/ 不存在
	if _, err := os.Stat("config"); !os.IsNotExist(err) {
		t.Fatal("前置：config/ 应不存在")
	}

	if err := writeConfigStatus("test_status"); err != nil {
		t.Fatalf("writeConfigStatus: %v", err)
	}

	data, err := os.ReadFile(configStatusPath)
	if err != nil {
		t.Fatalf("读 status: %v", err)
	}
	if string(data) != "test_status" {
		t.Errorf("status = %q, want 'test_status'", data)
	}
}

// TestLoadCurrentConfigMap_NotExist 验证 enc 文件不存在时返回 (nil, nil)。
func TestLoadCurrentConfigMap_NotExist(t *testing.T) {
	chdirTempDir(t)

	cfg, err := loadCurrentConfigMap()
	if err != nil {
		t.Fatalf("首次调用不应报错: %v", err)
	}
	if cfg != nil {
		t.Errorf("enc 文件不存在时应返回 nil map，实际 %v", cfg)
	}
}

// TestConfigCmd_Dispatch 验证 config 子命令路由：set / show / reset 能正确分派到 handler。
// 用 set + show 打组合；reset 单独一测（TestConfigReset 已覆盖 reset 本身）。
func TestConfigCmd_Dispatch_SetShowReset(t *testing.T) {
	chdirTempDir(t)

	// dispatch: config set --key=... --value=...
	configCmd([]string{"set", "--key=endpoint", "--value=https://dispatch.example"})

	cfg, err := loadCurrentConfigMap()
	if err != nil {
		t.Fatal(err)
	}
	if cfg["endpoint"] != "https://dispatch.example" {
		t.Errorf("dispatch set 后 endpoint 未生效: %v", cfg)
	}

	// dispatch: config show（走 configShow 入口）— 捕获 stdout 验证
	captured := captureStdout(t, func() {
		configCmd([]string{"show"})
	})
	if !strings.Contains(captured, "endpoint") {
		t.Errorf("dispatch show 输出应含 endpoint: %q", captured)
	}
	if !strings.Contains(captured, "https://dispatch.example") {
		t.Errorf("dispatch show 输出应含 endpoint 值: %q", captured)
	}

	// dispatch: config reset
	captured = captureStdout(t, func() {
		configCmd([]string{"reset"})
	})
	if !strings.Contains(captured, "unconfigured") {
		t.Errorf("dispatch reset 输出应含 unconfigured: %q", captured)
	}
}

// TestConfigShow_NoConfig 未配置时 configShow 输出 "(未配置)"。
func TestConfigShow_NoConfig(t *testing.T) {
	chdirTempDir(t)

	out := captureStdout(t, func() {
		configShow()
	})
	if !strings.Contains(out, "(未配置)") {
		t.Errorf("configShow 未配置时应输出 (未配置): %q", out)
	}
}

// TestConfigShow_WithConfig 设置后 configShow 能打印出配置项。
func TestConfigShow_WithConfig(t *testing.T) {
	chdirTempDir(t)

	if err := doConfigSetKV("endpoint", "https://show.example"); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		configShow()
	})
	if !strings.Contains(out, "endpoint") || !strings.Contains(out, "https://show.example") {
		t.Errorf("configShow 输出应含 endpoint 和值: %q", out)
	}
}

// TestConfigSet_FileFlag 走 configSet 入口的 --file 分支。
func TestConfigSet_FileFlag(t *testing.T) {
	chdirTempDir(t)

	src := "cfg.json"
	if err := os.WriteFile(src, []byte(`{"endpoint":"https://file-flag.example"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		configSet([]string{"--file=" + src})
	})
	if !strings.Contains(out, "配置已从文件导入") {
		t.Errorf("--file 分支应输出 '配置已从文件导入': %q", out)
	}
	cfg, err := loadCurrentConfigMap()
	if err != nil {
		t.Fatal(err)
	}
	if cfg["endpoint"] != "https://file-flag.example" {
		t.Errorf("cfg[endpoint] = %v, want https://file-flag.example", cfg["endpoint"])
	}
}

// TestPubkeyCmd_Dispatch_Show 无参数走 pubkeyShow 分支。
func TestPubkeyCmd_Dispatch_Show(t *testing.T) {
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", "")
	t.Setenv("KSAPP_MCP_PRIVKEY_OLD_B64", "")
	chdirTempDir(t)

	out := captureStdout(t, func() {
		pubkeyCmd(nil)
	})
	if !strings.Contains(out, "fingerprint:") {
		t.Errorf("pubkeyCmd 无参应走 show 分支: %q", out)
	}
}

// TestPubkeyCmd_Dispatch_Rotate 走 rotate 子命令（print-only 避免写文件副作用扩散）。
func TestPubkeyCmd_Dispatch_Rotate(t *testing.T) {
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", "")
	t.Setenv("KSAPP_MCP_PRIVKEY_OLD_B64", "")
	chdirTempDir(t)

	out := captureStdout(t, func() {
		pubkeyCmd([]string{"rotate", "--print-only"})
	})
	if !strings.Contains(out, "KSAPP_MCP_PRIVKEY_B64=") {
		t.Errorf("pubkeyCmd rotate --print-only 应输出 KSAPP_MCP_PRIVKEY_B64=: %q", out)
	}
}

// TestPubkeyCmd_Dispatch_PruneOld 走 prune-old 子命令。
func TestPubkeyCmd_Dispatch_PruneOld(t *testing.T) {
	chdirTempDir(t)

	// 预埋 .old
	if err := os.MkdirAll("config", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config/.mcp-key.old", []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		pubkeyCmd([]string{"prune-old"})
	})
	if !strings.Contains(out, "已清除") {
		t.Errorf("pubkeyCmd prune-old 应输出 '已清除': %q", out)
	}
	if _, err := os.Stat("config/.mcp-key.old"); !os.IsNotExist(err) {
		t.Errorf("prune-old 后 .mcp-key.old 应不存在")
	}
}

// captureStdout 临时替换 os.Stdout 为 pipe，收集 fn 执行期间的所有 stdout 输出。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	_ = w.Close()
	os.Stdout = orig
	return <-done
}
