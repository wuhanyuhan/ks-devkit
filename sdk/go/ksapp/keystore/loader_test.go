package keystore

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/crypto"
)

// 注意：本文件几乎所有测试都使用 t.Setenv，根据 Go 测试规则禁止 t.Parallel()。

// clearMCPEnv 清空所有 KSAPP_MCP_* 环境变量，防止外部环境污染测试。
func clearMCPEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envPrivkeyB64, "")
	t.Setenv(envPrivkeyOldB64, "")
	t.Setenv(envPrivkeyFile, "")
	t.Setenv(envPrivkeyOldFile, "")
}

// genPrivkey 生成 32 字节 X25519 私钥，测试期间使用。
func genPrivkey(t *testing.T) []byte {
	t.Helper()
	priv, _, err := crypto.GenerateX25519()
	if err != nil {
		t.Fatalf("GenerateX25519: %v", err)
	}
	return priv
}

// noExistFile 返回临时目录下保证不存在的路径，测试用作 Secret 文件占位。
func noExistFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "definitely-not-here")
}

// optsForTempDir 构造一组指向临时目录的 LoadOptions，避免污染真实 config/.mcp-key。
func optsForTempDir(t *testing.T) (*LoadOptions, string) {
	t.Helper()
	dir := t.TempDir()
	return &LoadOptions{
		SecretFile:    noExistFile(t),
		SecretFileOld: noExistFile(t),
		FallbackFile:  filepath.Join(dir, ".mcp-key"),
		FallbackOld:   filepath.Join(dir, ".mcp-key.old"),
	}, dir
}

func TestLoad_FromEnv_Primary(t *testing.T) {
	clearMCPEnv(t)
	priv := genPrivkey(t)
	t.Setenv(envPrivkeyB64, base64.RawURLEncoding.EncodeToString(priv))

	opts, _ := optsForTempDir(t)
	ks, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ks.Source != SourceEnv {
		t.Errorf("Source = %v, want SourceEnv", ks.Source)
	}
	if ks.Primary == nil {
		t.Fatal("Primary nil")
	}
	if !bytes.Equal(ks.Primary.Privkey, priv) {
		t.Error("Primary privkey 不匹配")
	}
	if len(ks.Primary.Pubkey) != x25519PubkeyLen {
		t.Errorf("Pubkey len = %d, want %d", len(ks.Primary.Pubkey), x25519PubkeyLen)
	}
	expFp := kstypes.Fingerprint(ks.Primary.Pubkey)
	if ks.Primary.Fingerprint != expFp {
		t.Errorf("Fingerprint = %s, want %s", ks.Primary.Fingerprint, expFp)
	}
	if ks.Old != nil {
		t.Errorf("Old 应为 nil, 实际 = %+v", ks.Old)
	}
	if ks.Primary.CreatedAt.IsZero() {
		t.Error("CreatedAt 应非零")
	}
}

func TestLoad_FromEnv_WithOld(t *testing.T) {
	clearMCPEnv(t)
	priv := genPrivkey(t)
	old := genPrivkey(t)
	t.Setenv(envPrivkeyB64, base64.RawURLEncoding.EncodeToString(priv))
	t.Setenv(envPrivkeyOldB64, base64.RawURLEncoding.EncodeToString(old))

	opts, _ := optsForTempDir(t)
	ks, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ks.Source != SourceEnv {
		t.Errorf("Source = %v, want SourceEnv", ks.Source)
	}
	if ks.Old == nil {
		t.Fatal("Old nil")
	}
	if !bytes.Equal(ks.Old.Privkey, old) {
		t.Error("Old privkey 不匹配")
	}
}

func TestLoad_FromEnv_StdEncodingFallback(t *testing.T) {
	// env 优先 RawURLEncoding，但应能 fallback 到 StdEncoding（手填）
	clearMCPEnv(t)
	priv := genPrivkey(t)
	t.Setenv(envPrivkeyB64, base64.StdEncoding.EncodeToString(priv))

	opts, _ := optsForTempDir(t)
	ks, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(ks.Primary.Privkey, priv) {
		t.Error("StdEncoding fallback 失败")
	}
}

func TestLoad_FromSecretFile(t *testing.T) {
	clearMCPEnv(t)
	priv := genPrivkey(t)
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "mcp-key")
	if err := os.WriteFile(secretPath, []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), keyFileMode); err != nil {
		t.Fatal(err)
	}
	opts := &LoadOptions{
		SecretFile:    secretPath,
		SecretFileOld: noExistFile(t),
		FallbackFile:  filepath.Join(dir, ".mcp-key"),
		FallbackOld:   filepath.Join(dir, ".mcp-key.old"),
	}
	ks, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ks.Source != SourceSecretFile {
		t.Errorf("Source = %v, want SourceSecretFile", ks.Source)
	}
	if !bytes.Equal(ks.Primary.Privkey, priv) {
		t.Error("Privkey 不匹配")
	}
	if ks.Old != nil {
		t.Error("Old 应为 nil")
	}
}

func TestLoad_FromSecretFile_WithOld(t *testing.T) {
	clearMCPEnv(t)
	priv := genPrivkey(t)
	old := genPrivkey(t)
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "mcp-key")
	secretOldPath := filepath.Join(dir, "mcp-key-old")
	if err := os.WriteFile(secretPath, []byte(base64.StdEncoding.EncodeToString(priv)), keyFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretOldPath, []byte(base64.StdEncoding.EncodeToString(old)+"\r\n"), keyFileMode); err != nil {
		t.Fatal(err)
	}
	opts := &LoadOptions{
		SecretFile:    secretPath,
		SecretFileOld: secretOldPath,
		FallbackFile:  filepath.Join(dir, ".mcp-key"),
		FallbackOld:   filepath.Join(dir, ".mcp-key.old"),
	}
	ks, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ks.Source != SourceSecretFile {
		t.Errorf("Source = %v, want SourceSecretFile", ks.Source)
	}
	if ks.Old == nil {
		t.Fatal("Old nil")
	}
	if !bytes.Equal(ks.Old.Privkey, old) {
		t.Error("Old privkey 不匹配（trailing \\r\\n 剥离失败？）")
	}
}

func TestLoad_FallbackFile_AutoGenerate(t *testing.T) {
	clearMCPEnv(t)
	opts, _ := optsForTempDir(t)

	ks1, err := Load(opts)
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if ks1.Source != SourceFallbackFile {
		t.Errorf("Source = %v, want SourceFallbackFile", ks1.Source)
	}
	if _, err := os.Stat(opts.FallbackFile); err != nil {
		t.Errorf("fallback file 应已生成: %v", err)
	}
	// 校验权限 0600
	info, err := os.Stat(opts.FallbackFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != keyFileMode {
		t.Errorf("file mode = %o, want %o", info.Mode().Perm(), keyFileMode)
	}

	// 再次加载，必须幂等（私钥字节相同）
	ks2, err := Load(opts)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if !bytes.Equal(ks1.Primary.Privkey, ks2.Primary.Privkey) {
		t.Error("二次加载 privkey 不一致（应幂等）")
	}
	if ks1.Primary.Fingerprint != ks2.Primary.Fingerprint {
		t.Errorf("二次加载 fingerprint 不一致: %s vs %s", ks1.Primary.Fingerprint, ks2.Primary.Fingerprint)
	}
}

func TestLoad_FallbackFile_WithOld(t *testing.T) {
	clearMCPEnv(t)
	opts, _ := optsForTempDir(t)

	// 先生成 primary
	if _, err := Load(opts); err != nil {
		t.Fatal(err)
	}
	// 手工再生成一份当 old
	priv, pub, err := crypto.GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	oldKp := &Keypair{
		Privkey:     priv,
		Pubkey:      pub,
		Fingerprint: kstypes.Fingerprint(pub),
	}
	if err := writeMCPKey(opts.FallbackOld, oldKp); err != nil {
		t.Fatal(err)
	}

	ks, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	if ks.Old == nil {
		t.Fatal("Old nil")
	}
	if !bytes.Equal(ks.Old.Privkey, priv) {
		t.Error("Old privkey 不匹配")
	}
}

func TestLoad_FallbackOldCorrupted_SilentNil(t *testing.T) {
	// FallbackOld 损坏应静默降级为 ks.Old = nil（与 SecretFileOld 致命有意区别）
	clearMCPEnv(t)
	opts, _ := optsForTempDir(t)
	if _, err := Load(opts); err != nil {
		t.Fatal(err)
	}
	// 写一个损坏的 .old 文件
	if err := os.WriteFile(opts.FallbackOld, []byte("not-valid-json"), keyFileMode); err != nil {
		t.Fatal(err)
	}
	ks, err := Load(opts)
	if err != nil {
		t.Errorf("Old 损坏不应返回 error: %v", err)
	}
	if ks.Old != nil {
		t.Errorf("Old 损坏应静默为 nil, 实际 = %+v", ks.Old)
	}
}

func TestLoad_PriorityEnvOverSecret(t *testing.T) {
	clearMCPEnv(t)
	envPriv := genPrivkey(t)
	secretPriv := genPrivkey(t)

	dir := t.TempDir()
	secretPath := filepath.Join(dir, "mcp-key")
	if err := os.WriteFile(secretPath, []byte(base64.StdEncoding.EncodeToString(secretPriv)), keyFileMode); err != nil {
		t.Fatal(err)
	}

	t.Setenv(envPrivkeyB64, base64.RawURLEncoding.EncodeToString(envPriv))

	opts := &LoadOptions{
		SecretFile:    secretPath,
		SecretFileOld: noExistFile(t),
		FallbackFile:  filepath.Join(dir, ".mcp-key"),
		FallbackOld:   filepath.Join(dir, ".mcp-key.old"),
	}
	ks, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ks.Source != SourceEnv {
		t.Errorf("Source = %v, want SourceEnv（env 应胜出）", ks.Source)
	}
	if !bytes.Equal(ks.Primary.Privkey, envPriv) {
		t.Error("应使用 env 私钥，实际 = secret 私钥")
	}
}

func TestLoad_PrioritySecretOverFallback(t *testing.T) {
	clearMCPEnv(t)
	secretPriv := genPrivkey(t)

	dir := t.TempDir()
	secretPath := filepath.Join(dir, "mcp-key")
	if err := os.WriteFile(secretPath, []byte(base64.StdEncoding.EncodeToString(secretPriv)), keyFileMode); err != nil {
		t.Fatal(err)
	}

	opts := &LoadOptions{
		SecretFile:    secretPath,
		SecretFileOld: noExistFile(t),
		FallbackFile:  filepath.Join(dir, ".mcp-key"),
		FallbackOld:   filepath.Join(dir, ".mcp-key.old"),
	}
	ks, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ks.Source != SourceSecretFile {
		t.Errorf("Source = %v, want SourceSecretFile", ks.Source)
	}
	// fallback 文件不应被生成
	if _, err := os.Stat(opts.FallbackFile); !os.IsNotExist(err) {
		t.Errorf("fallback 不应被创建（secret 已胜出），err=%v", err)
	}
}

func TestLoad_InvalidLengthRejected(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv(envPrivkeyB64, base64.RawURLEncoding.EncodeToString([]byte("short")))

	opts, _ := optsForTempDir(t)
	_, err := Load(opts)
	if err == nil {
		t.Fatal("期望 error，实际 nil")
	}
	if !strings.Contains(err.Error(), "32") {
		t.Errorf("期望 error 包含长度信息，实际: %v", err)
	}
}

func TestLoad_InvalidBase64Rejected(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv(envPrivkeyB64, "@@@not-base64@@@")

	opts, _ := optsForTempDir(t)
	_, err := Load(opts)
	if err == nil {
		t.Fatal("期望 error，实际 nil")
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	clearMCPEnv(t)
	// 传 nil opts，应用默认值；不依赖具体默认路径，只验证默认 fallback 被尝试。
	// 因为生产默认 fallback 是 "config/.mcp-key"，本测试设到一个 cwd 下的临时目录。
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	// 显式覆盖 secret 默认路径，避免触发 /secrets/mcp-key
	t.Setenv(envPrivkeyFile, noExistFile(t))
	t.Setenv(envPrivkeyOldFile, noExistFile(t))

	ks, err := Load(nil)
	if err != nil {
		t.Fatalf("Load(nil): %v", err)
	}
	if ks.Source != SourceFallbackFile {
		t.Errorf("Source = %v, want SourceFallbackFile", ks.Source)
	}
	if _, err := os.Stat(filepath.Join(tmp, defaultFallbackFile)); err != nil {
		t.Errorf("默认 fallback 路径未生成: %v", err)
	}
}

func TestLoad_CorruptedFile_BackupAndRegen(t *testing.T) {
	clearMCPEnv(t)
	opts, _ := optsForTempDir(t)
	if err := os.MkdirAll(filepath.Dir(opts.FallbackFile), keyDirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.FallbackFile, []byte("not-valid-json"), keyFileMode); err != nil {
		t.Fatal(err)
	}
	ks, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ks.Primary == nil {
		t.Fatal("Primary nil")
	}
	if _, err := os.Stat(opts.FallbackFile + ".corrupted"); err != nil {
		t.Errorf("corrupted 备份未生成: %v", err)
	}
}

func TestLoad_FingerprintMismatchRejected(t *testing.T) {
	clearMCPEnv(t)
	opts, _ := optsForTempDir(t)
	if err := os.MkdirAll(filepath.Dir(opts.FallbackFile), keyDirMode); err != nil {
		t.Fatal(err)
	}
	priv, pub, err := crypto.GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	// 故意篡改 fingerprint
	bad := &Keypair{
		Privkey:     priv,
		Pubkey:      pub,
		Fingerprint: "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
	}
	if err := writeMCPKey(opts.FallbackFile, bad); err != nil {
		t.Fatal(err)
	}
	// loadOrGenerate 会判 corrupted → 备份 → 重生
	ks, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	if ks.Primary == nil {
		t.Fatal("Primary nil")
	}
	if ks.Primary.Fingerprint == "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff" {
		t.Error("应已重生新对，fingerprint 不该等于篡改值")
	}
}

func TestLoad_VersionMismatchRejected(t *testing.T) {
	clearMCPEnv(t)
	opts, _ := optsForTempDir(t)
	if err := os.MkdirAll(filepath.Dir(opts.FallbackFile), keyDirMode); err != nil {
		t.Fatal(err)
	}
	priv, pub, err := crypto.GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	v2 := persistedKey{
		Version:     2,
		Pubkey:      base64.StdEncoding.EncodeToString(pub),
		Privkey:     base64.StdEncoding.EncodeToString(priv),
		Fingerprint: kstypes.Fingerprint(pub),
	}
	data, err := encodePersisted(&v2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.FallbackFile, data, keyFileMode); err != nil {
		t.Fatal(err)
	}
	ks, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	if ks.Primary == nil {
		t.Fatal("Primary nil")
	}
	// 应已 corrupted 备份
	if _, err := os.Stat(opts.FallbackFile + ".corrupted"); err != nil {
		t.Errorf("corrupted 备份未生成: %v", err)
	}
}

func TestSourceString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s Source
		w string
	}{
		{SourceEnv, "env"},
		{SourceSecretFile, "secret-file"},
		{SourceFallbackFile, "fallback-file"},
		{Source(0), "unknown"},
	}
	for _, c := range cases {
		if c.s.String() != c.w {
			t.Errorf("Source(%d).String() = %s, want %s", c.s, c.s.String(), c.w)
		}
	}
}
