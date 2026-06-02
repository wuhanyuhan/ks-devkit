package crypto

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// testvectorsFile 是 conformance testvectors 的默认相对路径（从 crypto 包目录看）。
// 通过 testdata/ 下的 symlink 指向 ks-devkit/conformance/config-schema/testvectors.json。
//
// 可通过环境变量 KS_CONFORMANCE_TESTVECTORS 覆盖（CI / 重定位用）。
const testvectorsFile = "testdata/testvectors.json"

type aadVector struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	MCPServerID      string `json:"mcp_server_id"`
	ConfigVersion    uint64 `json:"config_version"`
	Fingerprint      string `json:"fingerprint"`
	ExpectedBytesHex string `json:"expected_bytes_hex"`
}

type fingerprintVector struct {
	Name                string `json:"name"`
	Description         string `json:"description"`
	PubkeyHex           string `json:"pubkey_hex"`
	ExpectedFingerprint string `json:"expected_fingerprint"`
}

type testVectors struct {
	Version      int                 `json:"version"`
	AADCanonical []aadVector         `json:"aad_canonical"`
	Fingerprint  []fingerprintVector `json:"fingerprint"`
}

// loadTestVectors 加载 conformance testvectors。
// 若环境变量 KS_CONFORMANCE_TESTVECTORS 设置，优先使用；否则用 testvectorsFile。
// 找不到文件时返回 (nil, error)，测试调用方应 t.Skip(err) 处理（不让缺文件硬阻塞）。
func loadTestVectors(t *testing.T) (*testVectors, error) {
	t.Helper()
	path := testvectorsFile
	if env := os.Getenv("KS_CONFORMANCE_TESTVECTORS"); env != "" {
		path = env
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tv testVectors
	if err := json.Unmarshal(data, &tv); err != nil {
		return nil, err
	}
	return &tv, nil
}

func TestAAD_Vectors(t *testing.T) {
	t.Parallel()
	tv, err := loadTestVectors(t)
	if err != nil {
		t.Skip(err)
	}
	if len(tv.AADCanonical) == 0 {
		t.Fatal("aad_canonical 样本为空")
	}
	for _, v := range tv.AADCanonical {
		t.Run(v.Name, func(t *testing.T) {
			t.Parallel()
			// expected_bytes_hex 字段使用空格分隔的 hex，
			// 解码前先去除空白。
			hexStr := strings.ReplaceAll(v.ExpectedBytesHex, " ", "")
			want, err := hex.DecodeString(hexStr)
			if err != nil {
				t.Fatalf("无法解析 expected_bytes_hex: %v", err)
			}
			got := kstypes.AADCanonicalBytes(v.MCPServerID, v.ConfigVersion, v.Fingerprint)
			if !bytes.Equal(got, want) {
				t.Errorf("AAD 字节不匹配\n  got  = %x\n  want = %x", got, want)
			}
		})
	}
}

func TestFingerprint_Vectors(t *testing.T) {
	t.Parallel()
	tv, err := loadTestVectors(t)
	if err != nil {
		t.Skip(err)
	}
	if len(tv.Fingerprint) == 0 {
		t.Fatal("fingerprint 样本为空")
	}
	for _, v := range tv.Fingerprint {
		t.Run(v.Name, func(t *testing.T) {
			t.Parallel()
			pubkey, err := hex.DecodeString(v.PubkeyHex)
			if err != nil {
				t.Fatalf("无法解析 pubkey_hex: %v", err)
			}
			if len(pubkey) != X25519PubkeyLen {
				t.Fatalf("pubkey 长度 = %d, 期望 %d", len(pubkey), X25519PubkeyLen)
			}
			got := kstypes.Fingerprint(pubkey)
			if got != v.ExpectedFingerprint {
				t.Errorf("Fingerprint 不匹配: got %q, want %q", got, v.ExpectedFingerprint)
			}
		})
	}
}
