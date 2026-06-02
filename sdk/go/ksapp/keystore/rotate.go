package keystore

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"

	kstypes "github.com/wuhanyuhan/ks-types"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/crypto"
)

// OldKeyRetention 是 old 密钥保留窗口的推荐值（7 天）。
//
// 实际清理由调用方（如 ksapp pubkey rotate --prune-after 或定时任务）触发，
// 本包不强制执行 — 仅提供 PruneOld 入口，便于运维或 CLI 在 7 天后调用。
const OldKeyRetention = time.Hour * 24 * 7

// RotateOptions 控制 Rotate 的行为。零值字段会被默认值填充。
type RotateOptions struct {
	// FallbackFile：本地 fallback 主文件。零值时取 config/.mcp-key。
	FallbackFile string
	// FallbackOld：本地 fallback 旧密钥文件。零值时取 config/.mcp-key.old。
	FallbackOld string
	// PrintOnly：env / Secret 模式下，true → 只生成 + 返回 base64，不写文件，
	// 由运维注入到 KSAPP_MCP_PRIVKEY_B64 或 K8s Secret。false → 文件模式：
	// 把 FallbackFile 搬到 FallbackOld，然后写新对到 FallbackFile。
	PrintOnly bool
}

// applyDefaults 用默认常量填充零值字段，返回补全后的副本。
func (o *RotateOptions) applyDefaults() *RotateOptions {
	out := RotateOptions{}
	if o != nil {
		out = *o
	}
	if out.FallbackFile == "" {
		out.FallbackFile = defaultFallbackFile
	}
	if out.FallbackOld == "" {
		out.FallbackOld = defaultFallbackOld
	}
	return &out
}

// RotateResult 是 Rotate 的产物：新密钥对的 base64 + fingerprint，以及（文件模式
// 下）写入的文件清单（便于审计 + CLI 回显）。
type RotateResult struct {
	NewPrivkeyB64 string // base64.StdEncoding（带 padding）
	NewPubkeyB64  string // base64.StdEncoding（带 padding）
	Fingerprint   string
	FilesWritten  []string // 文件模式实际写入的路径；PrintOnly 模式为空
}

// Rotate 生成新 X25519 密钥对，按模式落盘或仅打印：
//   - PrintOnly = true：env / Secret 模式，仅返回 base64 + fingerprint，不写文件。
//   - PrintOnly = false：文件模式，把当前 FallbackFile 搬到 FallbackOld（若存在，
//     覆盖更旧的 .old），然后写新对到 FallbackFile。
//
// nil opts 等价于零值 RotateOptions（全部使用默认路径，PrintOnly = false）。
func Rotate(opts *RotateOptions) (*RotateResult, error) {
	o := opts.applyDefaults()

	priv, pub, err := crypto.GenerateX25519()
	if err != nil {
		return nil, fmt.Errorf("keystore: 生成新密钥对失败: %w", err)
	}
	fp := kstypes.Fingerprint(pub)
	res := &RotateResult{
		NewPrivkeyB64: base64.StdEncoding.EncodeToString(priv),
		NewPubkeyB64:  base64.StdEncoding.EncodeToString(pub),
		Fingerprint:   fp,
		FilesWritten:  nil,
	}
	if o.PrintOnly {
		return res, nil
	}

	// 文件模式：先把当前 FallbackFile 搬到 FallbackOld（覆盖更旧的 .old）
	oldExisted := fileExists(o.FallbackFile)
	if oldExisted {
		if err := os.Rename(o.FallbackFile, o.FallbackOld); err != nil {
			return nil, fmt.Errorf("keystore: 搬迁旧密钥 %s → %s 失败: %w",
				o.FallbackFile, o.FallbackOld, err)
		}
	}
	// 写新对
	newKp := &Keypair{
		Privkey:     priv,
		Pubkey:      pub,
		Fingerprint: fp,
		CreatedAt:   time.Now().UTC(),
	}
	if err := writeMCPKey(o.FallbackFile, newKp); err != nil {
		// 回滚 rename，避免留下 "primary 不见、old 是上一代 primary" 的中间态。
		// 不回滚的话下一次 Load 会触发自动 fallback 重生，运维感知不到丢失。
		if oldExisted {
			if rollbackErr := os.Rename(o.FallbackOld, o.FallbackFile); rollbackErr != nil {
				return nil, fmt.Errorf("keystore: 写新密钥到 %s 失败（%w）且回滚 rename 失败: %v",
					o.FallbackFile, err, rollbackErr)
			}
		}
		return nil, fmt.Errorf("keystore: 写新密钥到 %s 失败: %w", o.FallbackFile, err)
	}
	// FilesWritten 在 writeMCPKey 成功之后才填充，避免写失败时误报 .old 已写。
	if oldExisted {
		res.FilesWritten = append(res.FilesWritten, o.FallbackOld)
	}
	res.FilesWritten = append(res.FilesWritten, o.FallbackFile)
	return res, nil
}

// PruneOld 删除指定路径的旧密钥文件（典型为 config/.mcp-key.old），
// 用于 OldKeyRetention 窗口结束后的清理。
//
// 空字符串 path → 使用 defaultFallbackOld（config/.mcp-key.old）。
//
// 文件不存在视为错误（返回值可被 errors.Is(_, os.ErrNotExist) 识别），
// 让运维明确知道清理动作没生效。
func PruneOld(path string) error {
	if path == "" {
		path = defaultFallbackOld
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("keystore: 删除旧密钥文件 %s 失败: %w", path, err)
	}
	return nil
}
