// Package main — ks-conf-go-keygen：X25519 密钥对 + fingerprint 生成 mock-tool。
//
// 用法：
//
//	ks-conf-go-keygen
//
// 输入：无。
// 输出（stdout，JSON）：
//
//	{
//	  "privkey_b64":   "base64-std 32B",
//	  "pubkey_b64":    "base64-std 32B",
//	  "fingerprint":   "ab12:cd34:..."
//	}
//
// 用于 encrypt-decrypt 9 组合互通 case：在 shell 里动态拿到一对
// 密钥 + 对应 fingerprint，作为 encrypt 端的入参。
//
// 退出码：
//   - 0：成功
//   - 2：执行失败（不应发生，标准库 crypto/rand 出错才会）
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/crypto"
	kstypes "github.com/wuhanyuhan/ks-types"
)

type output struct {
	PrivkeyB64  string `json:"privkey_b64"`
	PubkeyB64   string `json:"pubkey_b64"`
	Fingerprint string `json:"fingerprint"`
}

func main() {
	priv, pub, err := crypto.GenerateX25519()
	if err != nil {
		fmt.Fprintf(os.Stderr, "GenerateX25519 失败：%v\n", err)
		os.Exit(2)
	}
	out := output{
		PrivkeyB64:  base64.StdEncoding.EncodeToString(priv),
		PubkeyB64:   base64.StdEncoding.EncodeToString(pub),
		Fingerprint: kstypes.Fingerprint(pub),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "JSON 序列化失败：%v\n", err)
		os.Exit(2)
	}
}
