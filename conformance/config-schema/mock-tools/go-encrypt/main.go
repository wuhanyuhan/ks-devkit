// Package main — ks-conf-go-encrypt：X25519-ECDH + AES-256-GCM 加密 mock-tool。
//
// 用法：
//
//	echo '<json>' | ks-conf-go-encrypt
//
// 输入（stdin，JSON）：
//
//	{
//	  "mcp_pubkey_b64":  "base64-std 32B",
//	  "mcp_server_id":   "ks-mcp-test",
//	  "config_version":  123,
//	  "fingerprint":     "ab12:cd34:...",
//	  "plaintext_b64":   "base64-std 明文"
//	}
//
// 输出（stdout，JSON）— 对齐 kstypes.EncryptedConfigPayload，但
// idempotency_key 字段省略（可选，本 mock 不生成 uuid）：
//
//	{
//	  "algorithm":        "x25519-ecdh-aes256gcm-v1",
//	  "ephemeral_pubkey": "base64-std 32B",
//	  "nonce":            "base64-std 12B",
//	  "aad_fields": {
//	    "mcp_server_id":  "...",
//	    "config_version": 123,
//	    "fingerprint":    "..."
//	  },
//	  "aad_canonical":    "base64-std AAD bytes",
//	  "ciphertext":       "base64-std ct||tag"
//	}
//
// 退出码：
//   - 0：加密成功
//   - 2：用法错 / JSON 解析错
//   - 21：pubkey 长度 != 32 / plaintext base64 解码错
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/crypto"
	kstypes "github.com/wuhanyuhan/ks-types"
)

type input struct {
	MCPPubkeyB64  string `json:"mcp_pubkey_b64"`
	MCPServerID   string `json:"mcp_server_id"`
	ConfigVersion uint64 `json:"config_version"`
	Fingerprint   string `json:"fingerprint"`
	PlaintextB64  string `json:"plaintext_b64"`
}

type output struct {
	Algorithm       string         `json:"algorithm"`
	EphemeralPubkey string         `json:"ephemeral_pubkey"`
	Nonce           string         `json:"nonce"`
	AADFields       map[string]any `json:"aad_fields"`
	AADCanonical    string         `json:"aad_canonical"`
	Ciphertext      string         `json:"ciphertext"`
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读 stdin 失败：%v\n", err)
		os.Exit(2)
	}
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		fmt.Fprintf(os.Stderr, "JSON 解析失败：%v\n", err)
		os.Exit(2)
	}

	mcpPub, err := base64.StdEncoding.DecodeString(in.MCPPubkeyB64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp_pubkey_b64 解码失败：%v\n", err)
		os.Exit(21)
	}
	if len(mcpPub) != crypto.X25519PubkeyLen {
		fmt.Fprintf(os.Stderr, "mcp_pubkey 长度 = %d, 期望 %d\n",
			len(mcpPub), crypto.X25519PubkeyLen)
		os.Exit(21)
	}

	plaintext, err := base64.StdEncoding.DecodeString(in.PlaintextB64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plaintext_b64 解码失败：%v\n", err)
		os.Exit(21)
	}

	// 生成临时密钥对并执行 X25519-ECDH + HKDF
	ephPriv, ephPub, err := crypto.GenerateX25519()
	if err != nil {
		fmt.Fprintf(os.Stderr, "GenerateX25519 失败：%v\n", err)
		os.Exit(2)
	}
	shared, err := crypto.X25519(ephPriv, mcpPub)
	if err != nil {
		fmt.Fprintf(os.Stderr, "X25519 ECDH 失败：%v\n", err)
		os.Exit(2)
	}
	kek, err := crypto.DeriveKEK(shared)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DeriveKEK 失败：%v\n", err)
		os.Exit(2)
	}

	// AAD canonical
	aad := kstypes.AADCanonicalBytes(in.MCPServerID, in.ConfigVersion, in.Fingerprint)

	// AES-256-GCM
	ct, nonce, err := crypto.EncryptAESGCM(kek, plaintext, aad)
	if err != nil {
		fmt.Fprintf(os.Stderr, "EncryptAESGCM 失败：%v\n", err)
		os.Exit(2)
	}

	out := output{
		Algorithm:       "x25519-ecdh-aes256gcm-v1",
		EphemeralPubkey: base64.StdEncoding.EncodeToString(ephPub),
		Nonce:           base64.StdEncoding.EncodeToString(nonce),
		AADFields: map[string]any{
			"mcp_server_id":  in.MCPServerID,
			"config_version": in.ConfigVersion,
			"fingerprint":    in.Fingerprint,
		},
		AADCanonical: base64.StdEncoding.EncodeToString(aad),
		Ciphertext:   base64.StdEncoding.EncodeToString(ct),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "JSON 序列化失败：%v\n", err)
		os.Exit(2)
	}
}
