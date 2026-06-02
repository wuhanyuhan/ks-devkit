package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

// AESGCMNonceLen 是 AES-256-GCM 推荐的 nonce 长度（12 字节）。
// 三语言实现（Go / TS / Python）必须保持一致。
const AESGCMNonceLen = 12

// EncryptAESGCM 用 AES-256-GCM 加密 plaintext，并将 aad 作为附加认证数据。
//
// 参数：
//   - kek：32 字节 AES-256 密钥（由 DeriveKEK 派生）
//   - plaintext：待加密明文
//   - aad：附加认证数据（典型由 kstypes.AADCanonicalBytes 生成）
//
// 返回：
//   - ciphertext：密文 + GCM tag（GCM 输出末尾包含 16 字节 tag）
//   - nonce：12 字节随机 nonce（每次加密都重新生成）
//   - err：加密失败
//
// 注意：返回的 nonce 必须与 ciphertext 一起持久化或传输，解密时需提供。
func EncryptAESGCM(kek, plaintext, aad []byte) (ciphertext, nonce []byte, err error) {
	if len(kek) != kekLen {
		return nil, nil, fmt.Errorf("crypto: kek 长度 = %d, 期望 %d", len(kek), kekLen)
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, nil, fmt.Errorf("crypto: 构造 AES cipher 失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("crypto: 构造 GCM 失败: %w", err)
	}
	nonce, err = randomBytes(AESGCMNonceLen)
	if err != nil {
		return nil, nil, err
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, aad)
	return ciphertext, nonce, nil
}

// DecryptAESGCM 用 AES-256-GCM 解密 ciphertext，并校验 aad。
//
// 参数：
//   - kek：32 字节 AES-256 密钥（与加密端一致）
//   - nonce：12 字节 nonce（加密端返回的）
//   - ciphertext：密文 + GCM tag
//   - aad：附加认证数据（必须与加密时完全一致，否则失败）
//
// 错误：
//   - kek / nonce 长度不对：返回 error
//   - GCM tag 校验失败（含 aad 不一致、密文被改、密钥错误）：返回 error
//
// 不在本层包装为 ERR_DECRYPT 等业务错误码——错误码语义由上层 endpoint
// handler 决定。
func DecryptAESGCM(kek, nonce, ciphertext, aad []byte) ([]byte, error) {
	if len(kek) != kekLen {
		return nil, fmt.Errorf("crypto: kek 长度 = %d, 期望 %d", len(kek), kekLen)
	}
	if len(nonce) != AESGCMNonceLen {
		return nil, fmt.Errorf("crypto: nonce 长度 = %d, 期望 %d", len(nonce), AESGCMNonceLen)
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("crypto: 构造 AES cipher 失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: 构造 GCM 失败: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("crypto: AES-GCM 解密失败: %w", err)
	}
	return plaintext, nil
}
