package keystore

// 本文件实现"独立 DEK 落盘"机制：
//
//   - .local-dek：32 字节随机对称密钥，与 X25519 私钥**完全无关**。
//     X25519 私钥只用于端到端加密通道，DEK 只用于本地 mcp-config.enc 加解密。
//     两者解耦使 X25519 私钥可随意轮换而不破坏历史密文。
//
//   - mcp-config.enc：[version u8][nonce 12][AES-GCM ct+tag]，
//     AAD = nil（落盘只靠 DEK 保密；不需要绑定上下文）。
//
// MVP 不做 DEK 轮换。容器重建会丢 DEK，由 K8s PVC / volume mount
// 承担持久化，SDK 不管。

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/crypto"
)

// configFileVersion 是 mcp-config.enc 文件首字节的版本号。
// MVP 固定为 1；未来新格式需升版本号并定义 v2 解析路径。
const configFileVersion uint8 = 1

// dekLen 是 DEK 的固定字节长度（32 字节，AES-256 密钥长度）。
const dekLen = 32

// configFileMinSize 是 mcp-config.enc 的最小合法长度：
//   - 1 字节 version
//   - 12 字节 AES-GCM nonce
//   - 16 字节 AES-GCM tag（无 plaintext 的极端情况）
//
// 实际文件长度 = 1 + 12 + len(plaintext) + 16。
const configFileMinSize = 1 + crypto.AESGCMNonceLen + 16

// dekFileMode 是 .local-dek 的文件权限（0600，与 .mcp-key 一致）。
const dekFileMode os.FileMode = 0o600

// LoadOrGenerateDEK 加载或首次生成 32 字节独立 DEK。
//
// 行为：
//   - 文件存在：os.ReadFile，长度 != 32 返回 error（视为损坏；与 .mcp-key 不同的是
//     DEK 损坏不自愈，因为静默重生会让 mcp-config.enc 永久无法解密）。
//   - 文件不存在：crypto/rand 生成 32 字节 → MkdirAll Dir(path) 0700 → 写文件 0600。
//
// 错误信息不会泄露 DEK 字节。
func LoadOrGenerateDEK(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != dekLen {
			return nil, fmt.Errorf("keystore: DEK 文件长度 = %d, 期望 %d（视为损坏）", len(data), dekLen)
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("keystore: 读取 DEK 文件失败: %w", err)
	}

	// 不存在 → 生成
	dek := make([]byte, dekLen)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("keystore: 生成 DEK 随机字节失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), keyDirMode); err != nil {
		return nil, fmt.Errorf("keystore: MkdirAll DEK 父目录失败: %w", err)
	}
	if err := os.WriteFile(path, dek, dekFileMode); err != nil {
		return nil, fmt.Errorf("keystore: 写 DEK 文件失败: %w", err)
	}
	return dek, nil
}

// EncryptConfigToFile 用 DEK 加密 plaintext 并按文件格式原子写入 cfgPath。
//
// 文件格式：[version u8][nonce 12 bytes][AES-GCM ciphertext + 16-byte tag]
//
// 实现细节：
//   - AAD = nil（落盘只靠 DEK 保密）
//   - 写入采用 .tmp + rename 原子模式（与 keystore.writeMCPKey 一致）
//   - 失败时清理 .tmp，不留残文件
//   - 父目录不存在自动 MkdirAll 0700
func EncryptConfigToFile(cfgPath string, dek, plaintext []byte) error {
	if len(dek) != dekLen {
		return fmt.Errorf("keystore: DEK 长度 = %d, 期望 %d", len(dek), dekLen)
	}
	ct, nonce, err := crypto.EncryptAESGCM(dek, plaintext, nil)
	if err != nil {
		return fmt.Errorf("keystore: AES-GCM 加密失败: %w", err)
	}
	// 拼装 [version][nonce][ct+tag]
	buf := make([]byte, 0, 1+len(nonce)+len(ct))
	buf = append(buf, configFileVersion)
	buf = append(buf, nonce...)
	buf = append(buf, ct...)

	if err := os.MkdirAll(filepath.Dir(cfgPath), keyDirMode); err != nil {
		return fmt.Errorf("keystore: MkdirAll 配置父目录失败: %w", err)
	}
	tmp := cfgPath + ".tmp"
	if err := os.WriteFile(tmp, buf, dekFileMode); err != nil {
		return fmt.Errorf("keystore: 写 mcp-config.enc.tmp 失败: %w", err)
	}
	if err := os.Rename(tmp, cfgPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("keystore: rename mcp-config.enc.tmp → final 失败: %w", err)
	}
	return nil
}

// DecryptConfigFromFile 读取并解密 mcp-config.enc，返回 plaintext。
//
// 损坏分支处理（区分 .corrupted 备份与否）：
//   - 长度 < configFileMinSize → 备份 .corrupted + error（数据完全无效，自动隔离）
//   - data[0] != configFileVersion → 仅 error（版本语义清晰，可能是协议升级，
//     运维需手工处理而非自动销毁）
//   - AES-GCM 解密失败（含错 DEK / 篡改）→ 备份 .corrupted + error
//
// 文件不存在 → 直接返回 error（不备份）。
//
// 错误信息不泄露 DEK 字节或 plaintext 字节。
func DecryptConfigFromFile(cfgPath string, dek []byte) ([]byte, error) {
	if len(dek) != dekLen {
		return nil, fmt.Errorf("keystore: DEK 长度 = %d, 期望 %d", len(dek), dekLen)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("keystore: 读取 mcp-config.enc 失败: %w", err)
	}
	if len(data) < configFileMinSize {
		// 长度异常：备份 + error
		backupCorrupted(cfgPath)
		return nil, fmt.Errorf("keystore: mcp-config.enc 长度 = %d, 期望 >= %d（视为损坏）",
			len(data), configFileMinSize)
	}
	if data[0] != configFileVersion {
		// version 不匹配：不备份，让运维处理
		return nil, fmt.Errorf("keystore: mcp-config.enc version = %d, 期望 %d",
			data[0], configFileVersion)
	}
	nonce := data[1 : 1+crypto.AESGCMNonceLen]
	ct := data[1+crypto.AESGCMNonceLen:]
	plaintext, err := crypto.DecryptAESGCM(dek, nonce, ct, nil)
	if err != nil {
		// AES-GCM tag 校验失败：错 DEK / 被篡改 / 截断 → 备份 + error
		backupCorrupted(cfgPath)
		return nil, fmt.Errorf("keystore: mcp-config.enc 解密失败: %w", err)
	}
	return plaintext, nil
}

// backupCorrupted 把损坏的文件备份到 path + ".corrupted"。
// 备份失败也不阻塞主流程：调用方只关心解密失败 + 文件已隔离的语义；
// 备份失败的额外错误吞掉以保持错误返回值的"为什么解密失败"语义清晰。
func backupCorrupted(cfgPath string) {
	_ = os.Rename(cfgPath, cfgPath+corruptedSuffix)
}
