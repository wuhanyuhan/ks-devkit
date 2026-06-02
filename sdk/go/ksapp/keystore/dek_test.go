package keystore

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrGenerateDEK_FirstTime 首次调用应生成 32 字节 DEK；二次调用应返回相同字节（幂等）。
func TestLoadOrGenerateDEK_FirstTime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, ".local-dek")

	dek, err := LoadOrGenerateDEK(p)
	if err != nil {
		t.Fatalf("首次 LoadOrGenerateDEK: %v", err)
	}
	if len(dek) != dekLen {
		t.Errorf("dek len = %d, want %d", len(dek), dekLen)
	}
	dek2, err := LoadOrGenerateDEK(p)
	if err != nil {
		t.Fatalf("二次 LoadOrGenerateDEK: %v", err)
	}
	if !bytes.Equal(dek, dek2) {
		t.Errorf("DEK 不幂等：dek != dek2")
	}

	// 校验文件权限 0600
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("文件权限 = %#o, want 0600", perm)
	}
}

// TestLoadOrGenerateDEK_CorruptedLength 已存在但长度损坏的 DEK 文件应返回 error，
// 不应静默重生（与 .mcp-key 损坏自愈策略不同：DEK 损坏 = mcp-config.enc 不可解，
// 直接重生会丢配置；交给运维处理更安全）。
func TestLoadOrGenerateDEK_CorruptedLength(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, ".local-dek")
	// 写入 16 字节（不足 32）
	if err := os.WriteFile(p, []byte("only-sixteen-byt"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadOrGenerateDEK(p)
	if err == nil {
		t.Fatal("DEK 长度不对，应返回 error，但 err == nil")
	}
}

// TestLoadOrGenerateDEK_CreatesParentDir 父目录不存在时应自动 MkdirAll 0700。
func TestLoadOrGenerateDEK_CreatesParentDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "nested", ".local-dek")
	dek, err := LoadOrGenerateDEK(p)
	if err != nil {
		t.Fatalf("LoadOrGenerateDEK: %v", err)
	}
	if len(dek) != dekLen {
		t.Errorf("dek len = %d, want %d", len(dek), dekLen)
	}
	parent := filepath.Dir(p)
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("父目录 stat: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("父目录应是 dir")
	}
}

// TestEncryptConfigToFile_RoundtripWithDEK 加密落盘后能用同一 DEK 解回原文。
func TestEncryptConfigToFile_RoundtripWithDEK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dekPath := filepath.Join(dir, ".local-dek")
	cfgPath := filepath.Join(dir, "mcp-config.enc")
	dek, err := LoadOrGenerateDEK(dekPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateDEK: %v", err)
	}
	plaintext := []byte(`{"api_key":"sk-xxx","region":"cn"}`)
	if err := EncryptConfigToFile(cfgPath, dek, plaintext); err != nil {
		t.Fatalf("EncryptConfigToFile: %v", err)
	}
	got, err := DecryptConfigFromFile(cfgPath, dek)
	if err != nil {
		t.Fatalf("DecryptConfigFromFile: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("roundtrip mismatch: got=%q, want=%q", got, plaintext)
	}
}

// TestEncryptConfigToFile_VersionField 文件首字节应是 version = 1。
func TestEncryptConfigToFile_VersionField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp-config.enc")
	dek, err := LoadOrGenerateDEK(filepath.Join(dir, ".local-dek"))
	if err != nil {
		t.Fatalf("LoadOrGenerateDEK: %v", err)
	}
	if err := EncryptConfigToFile(cfgPath, dek, []byte("x")); err != nil {
		t.Fatalf("EncryptConfigToFile: %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if data[0] != configFileVersion {
		t.Errorf("version byte = %d, want %d", data[0], configFileVersion)
	}
	// 文件长度至少 1 (version) + 12 (nonce) + 1 (plaintext) + 16 (tag) = 30
	if len(data) < configFileMinSize {
		t.Errorf("文件长度 = %d, want >= %d", len(data), configFileMinSize)
	}
}

// TestEncryptConfigToFile_AtomicWrite 加密写应是原子的：写过程中 cfgPath 一旦存在就是完整的；
// 旧文件存在时被新内容覆盖，且不留 .tmp 残留。
func TestEncryptConfigToFile_AtomicWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp-config.enc")
	dek, _ := LoadOrGenerateDEK(filepath.Join(dir, ".local-dek"))

	if err := EncryptConfigToFile(cfgPath, dek, []byte("v1")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := EncryptConfigToFile(cfgPath, dek, []byte("v2")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := DecryptConfigFromFile(cfgPath, dek)
	if err != nil {
		t.Fatalf("Decrypt after overwrite: %v", err)
	}
	if !bytes.Equal(got, []byte("v2")) {
		t.Errorf("覆盖写后内容 = %q, want \"v2\"", got)
	}
	// 不应有 .tmp 残留
	if _, err := os.Stat(cfgPath + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp 文件应已被 rename，但仍存在: %v", err)
	}
	// 文件权限 0600
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("文件权限 = %#o, want 0600", perm)
	}
}

// TestEncryptConfigToFile_BadDEKLen DEK 长度不对应返回 error。
func TestEncryptConfigToFile_BadDEKLen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp-config.enc")
	err := EncryptConfigToFile(cfgPath, []byte("too-short"), []byte("x"))
	if err == nil {
		t.Fatal("DEK 长度不对应返回 error，但 err == nil")
	}
	// 不应留下 cfg 文件
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Errorf("cfgPath 不应存在: %v", err)
	}
}

// TestDecryptConfigFromFile_FileNotExist 文件不存在应返回 error（不备份）。
func TestDecryptConfigFromFile_FileNotExist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp-config.enc")
	dek := make([]byte, dekLen)
	if _, err := rand.Read(dek); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	_, err := DecryptConfigFromFile(cfgPath, dek)
	if err == nil {
		t.Fatal("文件不存在应返回 error")
	}
	if _, err := os.Stat(cfgPath + corruptedSuffix); !os.IsNotExist(err) {
		t.Errorf(".corrupted 不应存在: %v", err)
	}
}

// TestDecryptConfigFromFile_TooShort 长度不足 1+12+16 应备份 .corrupted 并返回 error。
func TestDecryptConfigFromFile_TooShort(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp-config.enc")
	if err := os.WriteFile(cfgPath, []byte("short"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	dek := make([]byte, dekLen)
	if _, err := rand.Read(dek); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	_, err := DecryptConfigFromFile(cfgPath, dek)
	if err == nil {
		t.Fatal("长度不足应返回 error")
	}
	if _, err := os.Stat(cfgPath + corruptedSuffix); err != nil {
		t.Errorf("应备份到 .corrupted: %v", err)
	}
}

// TestDecryptConfigFromFile_CorruptBackup 完全无效的内容应备份 .corrupted。
func TestDecryptConfigFromFile_CorruptBackup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp-config.enc")
	// 长度小于 minSize：触发"长度不足"分支 + 备份
	if err := os.WriteFile(cfgPath, []byte("corrupted"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := DecryptConfigFromFile(cfgPath, make([]byte, dekLen))
	if err == nil {
		t.Fatal("损坏文件应报错")
	}
	if _, err := os.Stat(cfgPath + corruptedSuffix); err != nil {
		t.Errorf("损坏文件应备份到 .corrupted: %v", err)
	}
}

// TestDecryptConfigFromFile_VersionMismatch version 字段不匹配应返回 error，但**不**备份 .corrupted
// （version 语义清晰，可能是协议升级，运维需自行处理而非自动销毁）。
func TestDecryptConfigFromFile_VersionMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp-config.enc")
	dek, _ := LoadOrGenerateDEK(filepath.Join(dir, ".local-dek"))
	if err := EncryptConfigToFile(cfgPath, dek, []byte("hello")); err != nil {
		t.Fatalf("EncryptConfigToFile: %v", err)
	}
	// 篡改首字节：version = 99
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	data[0] = 99
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	_, err = DecryptConfigFromFile(cfgPath, dek)
	if err == nil {
		t.Fatal("version 不匹配应返回 error")
	}
	if _, err := os.Stat(cfgPath + corruptedSuffix); !os.IsNotExist(err) {
		t.Errorf("version 不匹配不应备份 .corrupted: %v", err)
	}
}

// TestDecryptConfigFromFile_WrongDEK 用错误 DEK 解密应触发 AES-GCM tag 校验失败 + 备份 .corrupted。
func TestDecryptConfigFromFile_WrongDEK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp-config.enc")
	dek1, _ := LoadOrGenerateDEK(filepath.Join(dir, ".local-dek-1"))
	if err := EncryptConfigToFile(cfgPath, dek1, []byte("secret")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	dek2 := make([]byte, dekLen)
	if _, err := rand.Read(dek2); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	_, err := DecryptConfigFromFile(cfgPath, dek2)
	if err == nil {
		t.Fatal("用错误 DEK 应解密失败")
	}
	if _, err := os.Stat(cfgPath + corruptedSuffix); err != nil {
		t.Errorf("AES-GCM 失败应备份 .corrupted: %v", err)
	}
}

// TestDecryptConfigFromFile_BadDEKLen DEK 长度不对应返回 error。
func TestDecryptConfigFromFile_BadDEKLen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp-config.enc")
	dek, _ := LoadOrGenerateDEK(filepath.Join(dir, ".local-dek"))
	if err := EncryptConfigToFile(cfgPath, dek, []byte("x")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	_, err := DecryptConfigFromFile(cfgPath, []byte("too-short"))
	if err == nil {
		t.Fatal("DEK 长度不对应返回 error")
	}
}
