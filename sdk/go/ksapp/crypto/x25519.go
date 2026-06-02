// Package crypto 提供 MCP 配置 Schema 端到端加密通道的低层加密原语：
// X25519-ECDH 密钥交换、HKDF-SHA256 KEK 派生、AES-256-GCM 认证加密。
//
// 规范源：docs/keystore-and-crypto.md（端到端加密通道）。
//
// 三语言（Go / TypeScript / Python）实现必须字节级互通：
//   - HKDF info 串："ksapp-config-v1"（HKDFInfo 常量，三语言一致）
//   - HKDF salt：32 字节全零（zero salt）
//   - AES-GCM nonce：12 字节随机
//   - AAD canonical 编码由 ks-types kstypes.AADCanonicalBytes 提供，本包不重复实现
//   - Pubkey 指纹（fingerprint）由 ks-types kstypes.Fingerprint 提供
//
// 本包只暴露原语，不负责错误码（如 ERR_DECRYPT）：错误码包装是上层 endpoint
// handler 的职责。所有 runtime error（参数长度、解密失败等）一律返回 error。
package crypto

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// HKDFInfo 是 HKDF-SHA256 派生 KEK 时使用的 info 串。
// 三语言（Go / TypeScript / Python）必须保持完全一致，禁止修改。
const HKDFInfo = "ksapp-config-v1"

// X25519PubkeyLen 是 X25519 公钥的固定字节长度。
const X25519PubkeyLen = 32

// X25519PrivkeyLen 是 X25519 私钥的固定字节长度。
const X25519PrivkeyLen = 32

// kekLen 是派生 KEK（AES-256 密钥）的字节长度。
const kekLen = 32

// GenerateX25519 生成一对随机 X25519 密钥。
// 返回 (privkey, pubkey, err)，两者均为 32 字节。
func GenerateX25519() (privkey, pubkey []byte, err error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("crypto: 生成 X25519 密钥失败: %w", err)
	}
	return priv.Bytes(), priv.PublicKey().Bytes(), nil
}

// X25519 用本端私钥与对端公钥执行 X25519-ECDH，返回 32 字节共享秘密。
// 私钥与公钥都必须是 32 字节，否则返回 error。
func X25519(privkey, peerPubkey []byte) ([]byte, error) {
	if len(privkey) != X25519PrivkeyLen {
		return nil, fmt.Errorf("crypto: privkey 长度 = %d, 期望 %d", len(privkey), X25519PrivkeyLen)
	}
	if len(peerPubkey) != X25519PubkeyLen {
		return nil, fmt.Errorf("crypto: peerPubkey 长度 = %d, 期望 %d", len(peerPubkey), X25519PubkeyLen)
	}
	curve := ecdh.X25519()
	priv, err := curve.NewPrivateKey(privkey)
	if err != nil {
		return nil, fmt.Errorf("crypto: 解析 privkey 失败: %w", err)
	}
	pub, err := curve.NewPublicKey(peerPubkey)
	if err != nil {
		return nil, fmt.Errorf("crypto: 解析 peerPubkey 失败: %w", err)
	}
	shared, err := priv.ECDH(pub)
	if err != nil {
		return nil, fmt.Errorf("crypto: X25519 ECDH 失败: %w", err)
	}
	return shared, nil
}

// DeriveX25519Pub 从 32 字节 X25519 私钥派生对应公钥。
// 返回 (privkey 副本, pubkey, err)，便于调用方获得规范化的密钥对。
//
// 该 helper 主要服务 ksapp/keystore：从持久化的私钥字节加载后
// 重建完整密钥对。
func DeriveX25519Pub(privkey []byte) (priv, pub []byte, err error) {
	if len(privkey) != X25519PrivkeyLen {
		return nil, nil, fmt.Errorf("crypto: privkey 长度 = %d, 期望 %d", len(privkey), X25519PrivkeyLen)
	}
	curve := ecdh.X25519()
	parsed, err := curve.NewPrivateKey(privkey)
	if err != nil {
		return nil, nil, fmt.Errorf("crypto: 解析 privkey 失败: %w", err)
	}
	return parsed.Bytes(), parsed.PublicKey().Bytes(), nil
}

// DeriveKEK 用 HKDF-SHA256 从 X25519 共享秘密派生 32 字节 KEK（AES-256 密钥）。
//
// 参数：
//   - shared：X25519 ECDH 共享秘密（典型 32 字节，但长度由 HKDF 自身约束，本函数不强制）
//
// 行为：
//   - salt = 32 字节全零（与 TS / Python 实现一致）
//   - info = HKDFInfo 常量（"ksapp-config-v1"）
//   - 输出 32 字节
//
// 返回的 KEK 用于 AES-256-GCM 加解密。
func DeriveKEK(shared []byte) ([]byte, error) {
	if len(shared) == 0 {
		return nil, errors.New("crypto: shared secret 不能为空")
	}
	salt := make([]byte, sha256.Size) // 32 字节全零
	prk, err := hkdf.Extract(sha256.New, shared, salt)
	if err != nil {
		return nil, fmt.Errorf("crypto: HKDF Extract 失败: %w", err)
	}
	kek, err := hkdf.Expand(sha256.New, prk, HKDFInfo, kekLen)
	if err != nil {
		return nil, fmt.Errorf("crypto: HKDF Expand 失败: %w", err)
	}
	return kek, nil
}

// randomBytes 返回 n 字节加密强度随机数据。包内 helper，给 aesgcm.go 复用。
func randomBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return nil, fmt.Errorf("crypto: 读取 %d 字节随机数失败: %w", n, err)
	}
	return buf, nil
}
