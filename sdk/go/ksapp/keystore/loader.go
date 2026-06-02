// Package keystore 负责 MCP 配置 Schema 端到端加密通道的 X25519 私钥
// 加载与轮换：三来源（env / K8s Secret 文件 / fallback 文件），双密钥并存
// （current Primary + old 轮换过渡），以及 ksapp pubkey rotate 命令的底层实现。
//
// 规范源：docs/keystore-and-crypto.md（启动期密钥加载与 .mcp-key 文件结构）。
//
// 优先级互斥（高 → 低）：
//  1. 环境变量 KSAPP_MCP_PRIVKEY_B64 [ + KSAPP_MCP_PRIVKEY_OLD_B64 ]
//  2. K8s Secret 文件 KSAPP_MCP_PRIVKEY_FILE 或 /secrets/mcp-key [ + _OLD_FILE ]
//  3. Fallback 文件 config/.mcp-key（首次自动生成 JSON） [ + .mcp-key.old ]
//
// 选中较高优先级时，更低优先级一律忽略 — 三种来源不混用。
package keystore

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	kstypes "github.com/wuhanyuhan/ks-types"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/crypto"
)

// 环境变量名常量。三语言（Go / TS / Python）必须保持一致，禁止修改。
const (
	envPrivkeyB64     = "KSAPP_MCP_PRIVKEY_B64"
	envPrivkeyOldB64  = "KSAPP_MCP_PRIVKEY_OLD_B64"
	envPrivkeyFile    = "KSAPP_MCP_PRIVKEY_FILE"
	envPrivkeyOldFile = "KSAPP_MCP_PRIVKEY_OLD_FILE"
)

// 默认文件路径常量。生产部署 K8s 走 /secrets，本地开发走 config/.mcp-key。
const (
	defaultSecretFile    = "/secrets/mcp-key"
	defaultSecretFileOld = "/secrets/mcp-key-old"
	defaultFallbackFile  = "config/.mcp-key"
	defaultFallbackOld   = "config/.mcp-key.old"
)

// 密钥长度复用 crypto 包常量（X25519 32 字节）。
const (
	x25519PrivkeyLen = crypto.X25519PrivkeyLen
	x25519PubkeyLen  = crypto.X25519PubkeyLen
)

// 文件权限：私钥文件 0600，目录 0700（安全规范）。
const (
	keyFileMode os.FileMode = 0o600
	keyDirMode  os.FileMode = 0o700
)

// persistedKey 的版本号。当前为 v1；未来新格式请走 v2 路径。
const persistedKeyVersion = 1

// corruptedSuffix 是文件损坏时备份用的后缀。
const corruptedSuffix = ".corrupted"

// Source 标识 Keystore 的密钥来源。
type Source int

// Source 枚举值（从 1 起，0 视为 unknown，便于检测未初始化）。
const (
	SourceEnv          Source = iota + 1 // 环境变量 KSAPP_MCP_PRIVKEY_B64
	SourceSecretFile                     // K8s Secret 文件
	SourceFallbackFile                   // config/.mcp-key fallback
)

// String 返回 Source 的人类可读名，便于日志与错误信息。
func (s Source) String() string {
	switch s {
	case SourceEnv:
		return "env"
	case SourceSecretFile:
		return "secret-file"
	case SourceFallbackFile:
		return "fallback-file"
	default:
		return "unknown"
	}
}

// Keypair 描述一对 X25519 密钥及其指纹。
type Keypair struct {
	Privkey     []byte    // 32 字节 X25519 私钥
	Pubkey      []byte    // 32 字节 X25519 公钥
	Fingerprint string    // fingerprint 格式（kstypes.Fingerprint）
	CreatedAt   time.Time // UTC，仅 fallback 文件保留有效；env / secret 来源近似为 Load 时间
}

// Keystore 是一次 Load 的完整结果：当前 Primary 密钥 + 可选的 Old 轮换密钥。
type Keystore struct {
	Primary *Keypair
	Old     *Keypair
	Source  Source
}

// LoadOptions 控制 Load 的来源路径。零值字段会被默认值填充（详见各字段注释）。
type LoadOptions struct {
	// SecretFile：K8s Secret 文件主路径。零值时取 env KSAPP_MCP_PRIVKEY_FILE，
	// 再降级为 /secrets/mcp-key。
	SecretFile string
	// SecretFileOld：K8s Secret 文件旧密钥路径。零值时取 env
	// KSAPP_MCP_PRIVKEY_OLD_FILE，再降级为 /secrets/mcp-key-old。
	SecretFileOld string
	// FallbackFile：本地 fallback 主文件。零值时取 config/.mcp-key。
	FallbackFile string
	// FallbackOld：本地 fallback 旧密钥文件。零值时取 config/.mcp-key.old。
	FallbackOld string
}

// applyDefaults 用环境变量与默认常量填充零值字段，返回补全后的副本。
func (o *LoadOptions) applyDefaults() *LoadOptions {
	out := LoadOptions{}
	if o != nil {
		out = *o
	}
	if out.SecretFile == "" {
		out.SecretFile = envOrDefault(envPrivkeyFile, defaultSecretFile)
	}
	if out.SecretFileOld == "" {
		out.SecretFileOld = envOrDefault(envPrivkeyOldFile, defaultSecretFileOld)
	}
	if out.FallbackFile == "" {
		out.FallbackFile = defaultFallbackFile
	}
	if out.FallbackOld == "" {
		out.FallbackOld = defaultFallbackOld
	}
	return &out
}

// Load 按优先级（env → secret-file → fallback-file）加载 X25519 私钥，返回一个
// 完整的 Keystore（Primary 必非 nil；Old 视来源是否提供）。fallback 来源若文件
// 不存在会自动生成新对并写入；若文件存在但损坏（JSON 解析失败/版本不匹配/长度
// 错/fingerprint 不匹配）则备份为 .corrupted 后重生。
//
// nil opts 等价于零值 LoadOptions（全部使用默认路径）。
func Load(opts *LoadOptions) (*Keystore, error) {
	o := opts.applyDefaults()

	// 优先级 1：env
	if envVal := os.Getenv(envPrivkeyB64); envVal != "" {
		primary, err := keypairFromB64Env(envVal)
		if err != nil {
			return nil, fmt.Errorf("keystore: 加载 %s 失败: %w", envPrivkeyB64, err)
		}
		ks := &Keystore{Primary: primary, Source: SourceEnv}
		if oldVal := os.Getenv(envPrivkeyOldB64); oldVal != "" {
			old, err := keypairFromB64Env(oldVal)
			if err != nil {
				return nil, fmt.Errorf("keystore: 加载 %s 失败: %w", envPrivkeyOldB64, err)
			}
			ks.Old = old
		}
		return ks, nil
	}

	// 优先级 2：Secret 文件
	if fileExists(o.SecretFile) {
		primary, err := keypairFromB64File(o.SecretFile)
		if err != nil {
			return nil, fmt.Errorf("keystore: 加载 SecretFile %s 失败: %w", o.SecretFile, err)
		}
		ks := &Keystore{Primary: primary, Source: SourceSecretFile}
		if fileExists(o.SecretFileOld) {
			old, err := keypairFromB64File(o.SecretFileOld)
			if err != nil {
				// SecretFileOld 损坏视为致命：K8s Secret 由运维管控，
				// 损坏意味着 Secret 配置错误，必须立即暴露而不是静默降级。
				// 与 FallbackOld 路径有意区别（fallback 是 dev 自愈侧）。
				return nil, fmt.Errorf("keystore: 加载 SecretFileOld %s 失败: %w", o.SecretFileOld, err)
			}
			ks.Old = old
		}
		return ks, nil
	}

	// 优先级 3：Fallback 文件
	primary, err := loadOrGenerateFallback(o.FallbackFile)
	if err != nil {
		return nil, fmt.Errorf("keystore: 加载 fallback %s 失败: %w", o.FallbackFile, err)
	}
	ks := &Keystore{Primary: primary, Source: SourceFallbackFile}
	if fileExists(o.FallbackOld) {
		old, err := readMCPKey(o.FallbackOld)
		if err != nil {
			// fallback 的 old 文件损坏不影响主流程：退化为无 old，但记录错误为返回值
			// 的话会让常见首启失败。这里选择静默忽略 + 不设 Old：
			// "不存在不报错"，损坏视为同等。
			//
			// FallbackOld 损坏静默为 nil：fallback 是 dev / single-replica 自愈侧，
			// .old 损坏不应阻塞主启动；与 SecretFileOld 路径有意区别（运维管控的
			// Secret 损坏必须暴露）。
			ks.Old = nil
		} else {
			ks.Old = old
		}
	}
	return ks, nil
}

// loadOrGenerateFallback 处理 fallback 文件的加载或首次生成 + 损坏自愈。
func loadOrGenerateFallback(path string) (*Keypair, error) {
	info, err := os.Stat(path)
	switch {
	case err != nil && os.IsNotExist(err):
		return generateAndWriteFallback(path)
	case err != nil:
		return nil, fmt.Errorf("stat: %w", err)
	case info.IsDir():
		return nil, fmt.Errorf("%s 是目录，期望文件", path)
	}
	kp, err := readMCPKey(path)
	if err != nil {
		// 损坏：备份为 .corrupted，再递归重生
		backup := path + corruptedSuffix
		if renameErr := os.Rename(path, backup); renameErr != nil {
			return nil, fmt.Errorf("文件损坏（%w）但备份失败: %v", err, renameErr)
		}
		return loadOrGenerateFallback(path)
	}
	return kp, nil
}

// generateAndWriteFallback 在 path 处生成新密钥对，写文件后返回 Keypair。
func generateAndWriteFallback(path string) (*Keypair, error) {
	priv, pub, err := crypto.GenerateX25519()
	if err != nil {
		return nil, fmt.Errorf("生成 X25519 失败: %w", err)
	}
	kp := &Keypair{
		Privkey:     priv,
		Pubkey:      pub,
		Fingerprint: kstypes.Fingerprint(pub),
		CreatedAt:   time.Now().UTC(),
	}
	if err := os.MkdirAll(filepath.Dir(path), keyDirMode); err != nil {
		return nil, fmt.Errorf("MkdirAll: %w", err)
	}
	if err := writeMCPKey(path, kp); err != nil {
		return nil, err
	}
	return kp, nil
}

// envOrDefault 返回 env var 值，为空则返回 fallback。
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// fileExists 判断 path 处是否存在普通文件（不存在或目录都视为否）。
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// decodeB64Privkey 解码 base64 编码的私钥并校验长度。
//
// 参数：
//   - s：base64 字符串（已剥 trailing 空白）
//   - urlFirst：true 时优先尝试 RawURLEncoding（env），false 时优先 StdEncoding（文件）
//
// 解码失败时尝试另一编码作为 fallback，便于人工手填两种 base64 变体。
func decodeB64Privkey(s string, urlFirst bool) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("base64 字符串为空")
	}
	first, second := base64.StdEncoding, base64.RawURLEncoding
	if urlFirst {
		first, second = base64.RawURLEncoding, base64.StdEncoding
	}
	if b, err := first.DecodeString(s); err == nil {
		if len(b) != x25519PrivkeyLen {
			return nil, fmt.Errorf("privkey 长度 = %d, 期望 %d", len(b), x25519PrivkeyLen)
		}
		return b, nil
	}
	b, err := second.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("base64 解码失败（两种编码都试过）: %w", err)
	}
	if len(b) != x25519PrivkeyLen {
		return nil, fmt.Errorf("privkey 长度 = %d, 期望 %d", len(b), x25519PrivkeyLen)
	}
	return b, nil
}

// readB64FromFile 读取 base64 文件内容（剥 trailing \n / \r / 空白）。
func readB64FromFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取文件: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// keypairFromPrivkey 由 32 字节私钥派生公钥并构造 Keypair（CreatedAt = 现在 UTC）。
func keypairFromPrivkey(priv []byte) (*Keypair, error) {
	priv2, pub, err := crypto.DeriveX25519Pub(priv)
	if err != nil {
		return nil, fmt.Errorf("派生公钥: %w", err)
	}
	return &Keypair{
		Privkey:     priv2,
		Pubkey:      pub,
		Fingerprint: kstypes.Fingerprint(pub),
		CreatedAt:   time.Now().UTC(),
	}, nil
}

// keypairFromB64Env 把 env 中的 base64 私钥串构造成 Keypair。
func keypairFromB64Env(s string) (*Keypair, error) {
	priv, err := decodeB64Privkey(s, true)
	if err != nil {
		return nil, err
	}
	return keypairFromPrivkey(priv)
}

// keypairFromB64File 把文件中的 base64 私钥构造成 Keypair。
func keypairFromB64File(path string) (*Keypair, error) {
	s, err := readB64FromFile(path)
	if err != nil {
		return nil, err
	}
	priv, err := decodeB64Privkey(s, false)
	if err != nil {
		return nil, err
	}
	return keypairFromPrivkey(priv)
}

// persistedKey 是 .mcp-key 文件的 JSON 结构。
type persistedKey struct {
	Version     int    `json:"version"`
	Pubkey      string `json:"pubkey"`     // base64.StdEncoding（带 padding）
	Privkey     string `json:"privkey"`    // base64.StdEncoding（带 padding）
	CreatedAt   string `json:"created_at"` // RFC 3339 UTC
	Fingerprint string `json:"fingerprint"`
}

// encodePersisted 把 persistedKey 序列化为 JSON 字节（缩进 2 空格，便于人工查阅）。
func encodePersisted(p *persistedKey) ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

// writeMCPKey 把 Keypair 以 JSON 形式原子写入 path（先写 .tmp 再 rename），
// 权限 0600。
func writeMCPKey(path string, kp *Keypair) error {
	createdAt := kp.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	p := &persistedKey{
		Version:     persistedKeyVersion,
		Pubkey:      base64.StdEncoding.EncodeToString(kp.Pubkey),
		Privkey:     base64.StdEncoding.EncodeToString(kp.Privkey),
		CreatedAt:   createdAt.UTC().Format(time.RFC3339),
		Fingerprint: kp.Fingerprint,
	}
	data, err := encodePersisted(p)
	if err != nil {
		return fmt.Errorf("encode persistedKey: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), keyDirMode); err != nil {
		return fmt.Errorf("MkdirAll: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, keyFileMode); err != nil {
		return fmt.Errorf("写 tmp 文件: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// 清理 tmp，避免遗留残文件
		_ = os.Remove(tmp)
		return fmt.Errorf("rename tmp → final: %w", err)
	}
	return nil
}

// readMCPKey 解析并校验 .mcp-key 文件，返回 Keypair；任一字段不合规返回 error。
//
// 校验包括：
//   - 版本号必须等于 persistedKeyVersion（v1）
//   - pubkey/privkey base64 解码后必须 32 字节
//   - kstypes.Fingerprint(pubkey) 必须等于 stored fingerprint（防篡改/损坏）
func readMCPKey(path string) (*Keypair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件: %w", err)
	}
	var p persistedKey
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("解析 JSON: %w", err)
	}
	if p.Version != persistedKeyVersion {
		return nil, fmt.Errorf("不支持的 version = %d, 期望 %d", p.Version, persistedKeyVersion)
	}
	pub, err := base64.StdEncoding.DecodeString(p.Pubkey)
	if err != nil {
		return nil, fmt.Errorf("pubkey base64: %w", err)
	}
	if len(pub) != x25519PubkeyLen {
		return nil, fmt.Errorf("pubkey 长度 = %d, 期望 %d", len(pub), x25519PubkeyLen)
	}
	priv, err := base64.StdEncoding.DecodeString(p.Privkey)
	if err != nil {
		return nil, fmt.Errorf("privkey base64: %w", err)
	}
	if len(priv) != x25519PrivkeyLen {
		return nil, fmt.Errorf("privkey 长度 = %d, 期望 %d", len(priv), x25519PrivkeyLen)
	}
	expFp := kstypes.Fingerprint(pub)
	if expFp != p.Fingerprint {
		return nil, fmt.Errorf("fingerprint 不匹配: stored=%s, computed=%s", p.Fingerprint, expFp)
	}
	createdAt, err := time.Parse(time.RFC3339, p.CreatedAt)
	if err != nil {
		// CreatedAt 错误不致命，使用零值 + 警告并继续
		createdAt = time.Time{}
	}
	return &Keypair{
		Privkey:     priv,
		Pubkey:      pub,
		Fingerprint: p.Fingerprint,
		CreatedAt:   createdAt.UTC(),
	}, nil
}
