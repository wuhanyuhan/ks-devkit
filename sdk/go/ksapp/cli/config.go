package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/keystore"
)

// 文件系统权限位（private — 当前用户读写）。
const (
	filePermPrivate = 0o600
	dirPermPrivate  = 0o700
)

// 配置文件路径（相对 cwd，便于测试用 t.Chdir(t.TempDir()) 隔离副作用）。
const (
	configDir        = "config"
	configDEKPath    = configDir + "/.local-dek"
	configEncPath    = configDir + "/mcp-config.enc"
	configStatusPath = configDir + "/.status"
)

// 状态值常量（参见 sdk/go/ksapp/app.go 的 configStatus 枚举）。
const (
	statusViaCLI       = "via_cli"
	statusUnconfigured = "unconfigured"
)

// 脱敏尾段长度：显示敏感字段最后 N 个字符便于识别而不泄露全文。
const redactTailLen = 4

// 敏感字段前缀（用于 configShow 脱敏）。
var sensitiveKeywords = []string{"key", "secret", "token", "password", "api"}

// configCmd 是 config 子命令的路由入口。
func configCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "config 需要子命令: set/show/reset")
		os.Exit(2)
	}
	switch args[0] {
	case "set":
		configSet(args[1:])
	case "show":
		configShow()
	case "reset":
		configReset()
	default:
		fmt.Fprintf(os.Stderr, "未知 config 子命令: %s\n", args[0])
		os.Exit(2)
	}
}

// configSet 解析 flag 后分派给可测 helper（doConfigSetKV / doConfigSetFromFile）。
// CLI 入口函数：副作用在 helper 内，失败调用 exitErr。
func configSet(args []string) {
	fs := flag.NewFlagSet("config set", flag.ExitOnError)
	key := fs.String("key", "", "单字段 key（snake_case）")
	value := fs.String("value", "", "单字段 value")
	file := fs.String("file", "", "YAML/JSON 配置文件路径（批量）")
	_ = fs.Parse(args)

	if *file != "" {
		if err := doConfigSetFromFile(*file); err != nil {
			exitErr("%v", err)
		}
		fmt.Println("配置已从文件导入（via_cli）")
		return
	}

	if *key == "" || *value == "" {
		exitErr("--key 与 --value 或 --file 必填其一")
	}

	if err := doConfigSetKV(*key, *value); err != nil {
		exitErr("%v", err)
	}
	fmt.Println("配置已更新（via_cli）")
}

// doConfigSetKV 读现有配置（若不存在以空 map 开始），更新单字段，写回加密文件，
// 并同步 config/.status = via_cli。返回错误让 CLI 入口处理。
func doConfigSetKV(key, value string) error {
	current, err := loadCurrentConfigMap()
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}
	if current == nil {
		current = map[string]any{}
	}
	current[key] = value
	if err := saveConfigMap(current); err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	if err := writeConfigStatus(statusViaCLI); err != nil {
		return fmt.Errorf("写状态文件失败: %w", err)
	}
	return nil
}

// doConfigSetFromFile 从 YAML 或 JSON 文件批量导入配置。
// 按扩展名 (.yaml / .yml → YAML; 其他 → JSON) 选择解析器。
func doConfigSetFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读文件失败: %w", err)
	}
	cfg := map[string]any{}
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("YAML 解析失败: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("JSON 解析失败: %w", err)
		}
	}
	if err := saveConfigMap(cfg); err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	if err := writeConfigStatus(statusViaCLI); err != nil {
		return fmt.Errorf("写状态文件失败: %w", err)
	}
	return nil
}

// configShow 读配置并渲染到 stdout（敏感字段脱敏）。
// 入口函数：失败 exitErr；渲染逻辑抽到 renderConfig 便于测试。
func configShow() {
	cfg, err := loadCurrentConfigMap()
	if err != nil {
		exitErr("读取配置失败: %v", err)
	}
	renderConfig(os.Stdout, cfg)
}

// renderConfig 把配置 map 以 "key: value" 形式写入 w，敏感字段脱敏。
// cfg == nil → 输出 "(未配置)"。
func renderConfig(w io.Writer, cfg map[string]any) {
	if cfg == nil {
		fmt.Fprintln(w, "(未配置)")
		return
	}
	// 排序保证输出稳定（便于 diff / 跨语言 conformance 字节级比对）。
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := cfg[k]
		if isSensitiveKey(k) {
			fmt.Fprintf(w, "%-20s: ***%s（已脱敏）\n", k, tailN(fmt.Sprintf("%v", v), redactTailLen))
		} else {
			fmt.Fprintf(w, "%-20s: %v\n", k, v)
		}
	}
}

// configReset 删除加密配置文件 + 写 status = unconfigured。
// 文件不存在静默忽略（reset 幂等）；其他错误（权限/磁盘）上报给运维。
func configReset() {
	if err := os.Remove(configEncPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		exitErr("删除加密配置失败: %v", err)
	}
	if err := writeConfigStatus(statusUnconfigured); err != nil {
		exitErr("写状态文件失败: %v", err)
	}
	fmt.Println("配置已重置 (unconfigured)")
}

// loadCurrentConfigMap 从 mcp-config.enc 解密并解析为 map[string]any。
// 文件不存在返回 (nil, nil)，允许 show / set 首次调用自然 upsert。
func loadCurrentConfigMap() (map[string]any, error) {
	dek, err := keystore.LoadOrGenerateDEK(configDEKPath)
	if err != nil {
		return nil, err
	}
	data, err := keystore.DecryptConfigFromFile(configEncPath, dek)
	if err != nil {
		// keystore 包用 fmt.Errorf("...: %w", err) 包装，errors.Is 穿透 wrap chain 可靠识别 NotExist。
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// saveConfigMap 把 cfg 序列化为 JSON 后用 DEK 加密写入 mcp-config.enc。
func saveConfigMap(cfg map[string]any) error {
	dek, err := keystore.LoadOrGenerateDEK(configDEKPath)
	if err != nil {
		return err
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return keystore.EncryptConfigToFile(configEncPath, dek, data)
}

// writeConfigStatus 写 config/.status 文件，记录当前配置来源状态。
// 父目录不存在自动 MkdirAll。
func writeConfigStatus(status string) error {
	if err := os.MkdirAll(configDir, dirPermPrivate); err != nil {
		return err
	}
	return os.WriteFile(configStatusPath, []byte(status), filePermPrivate)
}

// isSensitiveKey 判断 key 是否命中敏感关键字（大小写不敏感 contains 匹配）。
func isSensitiveKey(k string) bool {
	lower := strings.ToLower(k)
	for _, needle := range sensitiveKeywords {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// tailN 返回 s 末尾 n 个字符；len(s) <= n 时返回 s 本身。
func tailN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
