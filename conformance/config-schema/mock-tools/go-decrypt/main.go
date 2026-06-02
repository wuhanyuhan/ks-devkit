// Package main — ks-conf-go-decrypt：X25519-ECDH + AES-256-GCM 解密 mock-tool。
//
// 用法：
//
//	echo '<json>' | ks-conf-go-decrypt
//
// 输入（stdin，JSON）：
//
//	{
//	  "mcp_privkey_b64":   "base64-std 32B",
//	  "ephemeral_pubkey":  "base64-std 32B",
//	  "nonce":             "base64-std 12B",
//	  "aad_canonical":     "base64-std AAD bytes",
//	  "ciphertext":        "base64-std ct||tag"
//	}
//
// 输出（stdout，JSON）：
//
//	{ "plaintext_b64": "base64-std 明文" }
//
// 退出码：
//   - 0：解密成功
//   - 2：用法错 / JSON 解析错
//   - 20：（保留）AAD 不匹配。本 mock 直接把 aad_canonical 原样传给 GCM，
//     所以 AAD 不匹配会在 GCM 层以 tag 失败表现（退出码 22）。
//     20 保留给未来增加的"aad 重算校验"分支。
//   - 21：privkey / ephemeral_pubkey / nonce 长度错 / base64 解码错
//   - 22：GCM tag 校验失败
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/crypto"
)

type input struct {
	MCPPrivkeyB64   string `json:"mcp_privkey_b64"`
	EphemeralPubkey string `json:"ephemeral_pubkey"`
	Nonce           string `json:"nonce"`
	AADCanonical    string `json:"aad_canonical"`
	Ciphertext      string `json:"ciphertext"`
}

type output struct {
	PlaintextB64 string `json:"plaintext_b64"`
}

// exitLen 21 — 长度 / base64 解码 / privkey-pubkey-nonce 相关。
func exitLen(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(21)
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

	priv, err := base64.StdEncoding.DecodeString(in.MCPPrivkeyB64)
	if err != nil {
		exitLen("mcp_privkey_b64 解码失败：%v", err)
	}
	if len(priv) != crypto.X25519PrivkeyLen {
		exitLen("mcp_privkey 长度 = %d, 期望 %d", len(priv), crypto.X25519PrivkeyLen)
	}
	ephPub, err := base64.StdEncoding.DecodeString(in.EphemeralPubkey)
	if err != nil {
		exitLen("ephemeral_pubkey 解码失败：%v", err)
	}
	if len(ephPub) != crypto.X25519PubkeyLen {
		exitLen("ephemeral_pubkey 长度 = %d, 期望 %d", len(ephPub), crypto.X25519PubkeyLen)
	}
	nonce, err := base64.StdEncoding.DecodeString(in.Nonce)
	if err != nil {
		exitLen("nonce 解码失败：%v", err)
	}
	if len(nonce) != crypto.AESGCMNonceLen {
		exitLen("nonce 长度 = %d, 期望 %d", len(nonce), crypto.AESGCMNonceLen)
	}
	aad, err := base64.StdEncoding.DecodeString(in.AADCanonical)
	if err != nil {
		exitLen("aad_canonical 解码失败：%v", err)
	}
	ct, err := base64.StdEncoding.DecodeString(in.Ciphertext)
	if err != nil {
		exitLen("ciphertext 解码失败：%v", err)
	}

	// ECDH + HKDF
	shared, err := crypto.X25519(priv, ephPub)
	if err != nil {
		fmt.Fprintf(os.Stderr, "X25519 ECDH 失败：%v\n", err)
		os.Exit(2)
	}
	kek, err := crypto.DeriveKEK(shared)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DeriveKEK 失败：%v\n", err)
		os.Exit(2)
	}

	// AES-256-GCM Open
	plaintext, err := crypto.DecryptAESGCM(kek, nonce, ct, aad)
	if err != nil {
		// Go 的 crypto/cipher.GCM.Open 失败时返回 "cipher: message authentication failed"；
		// 映射为退出码 22（GCM tag 失败）。其他（如 kek 长度错）不应发生。
		if strings.Contains(err.Error(), "authentication failed") {
			fmt.Fprintf(os.Stderr, "GCM tag 校验失败\n")
			os.Exit(22)
		}
		fmt.Fprintf(os.Stderr, "解密失败：%v\n", err)
		os.Exit(2)
	}

	out := output{PlaintextB64: base64.StdEncoding.EncodeToString(plaintext)}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "JSON 序列化失败：%v\n", err)
		os.Exit(2)
	}
}
