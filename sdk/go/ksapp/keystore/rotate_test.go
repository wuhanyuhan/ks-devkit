package keystore

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/crypto"
)

func TestRotate_PrintOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res, err := Rotate(&RotateOptions{
		FallbackFile: filepath.Join(dir, ".mcp-key"),
		FallbackOld:  filepath.Join(dir, ".mcp-key.old"),
		PrintOnly:    true,
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if res.NewPrivkeyB64 == "" || res.NewPubkeyB64 == "" {
		t.Error("PrintOnly 也必须生成 base64 输出")
	}
	if len(res.FilesWritten) != 0 {
		t.Errorf("PrintOnly 不应写文件, 实际 = %v", res.FilesWritten)
	}
	// 解码验证 32 字节
	priv, err := base64.StdEncoding.DecodeString(res.NewPrivkeyB64)
	if err != nil {
		t.Fatalf("decode privkey: %v", err)
	}
	if len(priv) != x25519PrivkeyLen {
		t.Errorf("privkey 长度 = %d, want %d", len(priv), x25519PrivkeyLen)
	}
	pub, err := base64.StdEncoding.DecodeString(res.NewPubkeyB64)
	if err != nil {
		t.Fatalf("decode pubkey: %v", err)
	}
	if len(pub) != x25519PubkeyLen {
		t.Errorf("pubkey 长度 = %d, want %d", len(pub), x25519PubkeyLen)
	}
	// fingerprint 必须由 pubkey 派生
	if res.Fingerprint != kstypes.Fingerprint(pub) {
		t.Error("fingerprint 不一致")
	}
	// PrintOnly 文件不应被生成
	if _, err := os.Stat(filepath.Join(dir, ".mcp-key")); !os.IsNotExist(err) {
		t.Errorf(".mcp-key 不应存在: err=%v", err)
	}
}

func TestRotate_FileMode_FirstTime_NoOld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, ".mcp-key")
	fpOld := filepath.Join(dir, ".mcp-key.old")
	res, err := Rotate(&RotateOptions{
		FallbackFile: fp,
		FallbackOld:  fpOld,
		PrintOnly:    false,
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	// FilesWritten 应只有 .mcp-key（首次轮换无 .old）
	if len(res.FilesWritten) != 1 || res.FilesWritten[0] != fp {
		t.Errorf("FilesWritten = %v, want [%s]", res.FilesWritten, fp)
	}
	if _, err := os.Stat(fp); err != nil {
		t.Errorf(".mcp-key 应存在: %v", err)
	}
	if _, err := os.Stat(fpOld); !os.IsNotExist(err) {
		t.Errorf(".mcp-key.old 不应存在（首次轮换）: err=%v", err)
	}
	// 校验文件权限 0600
	info, err := os.Stat(fp)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != keyFileMode {
		t.Errorf("file mode = %o, want %o", info.Mode().Perm(), keyFileMode)
	}
}

func TestRotate_FileMode_MovesOld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, ".mcp-key")
	fpOld := filepath.Join(dir, ".mcp-key.old")
	opts := &RotateOptions{
		FallbackFile: fp,
		FallbackOld:  fpOld,
		PrintOnly:    false,
	}
	// 首次轮换：写 .mcp-key
	first, err := Rotate(opts)
	if err != nil {
		t.Fatalf("first Rotate: %v", err)
	}
	// 第二次轮换：旧的搬到 .old，新的写 .mcp-key
	second, err := Rotate(opts)
	if err != nil {
		t.Fatalf("second Rotate: %v", err)
	}
	// FilesWritten 应包含 .old 与 .mcp-key
	wantSet := map[string]bool{fp: true, fpOld: true}
	gotSet := map[string]bool{}
	for _, f := range second.FilesWritten {
		gotSet[f] = true
	}
	for k := range wantSet {
		if !gotSet[k] {
			t.Errorf("FilesWritten 缺少 %s（实际 = %v）", k, second.FilesWritten)
		}
	}
	// 校验 .old 的内容是首次的密钥
	oldKp, err := readMCPKey(fpOld)
	if err != nil {
		t.Fatalf("readMCPKey old: %v", err)
	}
	firstPriv, err := base64.StdEncoding.DecodeString(first.NewPrivkeyB64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(oldKp.Privkey, firstPriv) {
		t.Error(".mcp-key.old 不是第一次的密钥")
	}
	// 校验 .mcp-key 的内容是第二次的密钥
	curKp, err := readMCPKey(fp)
	if err != nil {
		t.Fatalf("readMCPKey current: %v", err)
	}
	secondPriv, err := base64.StdEncoding.DecodeString(second.NewPrivkeyB64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(curKp.Privkey, secondPriv) {
		t.Error(".mcp-key 不是第二次的密钥")
	}
}

func TestRotate_FileMode_OverridesExistingOld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, ".mcp-key")
	fpOld := filepath.Join(dir, ".mcp-key.old")
	opts := &RotateOptions{FallbackFile: fp, FallbackOld: fpOld, PrintOnly: false}

	// 三次轮换：第三次后 .old 应覆盖第二次（不是第一次）
	if _, err := Rotate(opts); err != nil {
		t.Fatal(err)
	}
	second, err := Rotate(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Rotate(opts); err != nil {
		t.Fatal(err)
	}

	oldKp, err := readMCPKey(fpOld)
	if err != nil {
		t.Fatalf("readMCPKey old: %v", err)
	}
	secondPriv, err := base64.StdEncoding.DecodeString(second.NewPrivkeyB64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(oldKp.Privkey, secondPriv) {
		t.Error(".mcp-key.old 应是第二次的密钥（被第三次覆盖前的当前）")
	}
}

func TestRotate_Defaults(t *testing.T) {
	// nil opts → 应用默认值。把 cwd 切到临时目录，避免污染真实 config/。
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	res, err := Rotate(nil)
	if err != nil {
		t.Fatalf("Rotate(nil): %v", err)
	}
	if len(res.FilesWritten) != 1 {
		t.Errorf("FilesWritten 数量 = %d, want 1", len(res.FilesWritten))
	}
	if _, err := os.Stat(filepath.Join(tmp, defaultFallbackFile)); err != nil {
		t.Errorf("默认 fallback 路径未生成: %v", err)
	}
}

func TestPruneOld_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fpOld := filepath.Join(dir, ".mcp-key.old")
	priv, pub, err := crypto.GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	kp := &Keypair{
		Privkey:     priv,
		Pubkey:      pub,
		Fingerprint: kstypes.Fingerprint(pub),
	}
	if err := writeMCPKey(fpOld, kp); err != nil {
		t.Fatal(err)
	}
	if err := PruneOld(fpOld); err != nil {
		t.Fatalf("PruneOld: %v", err)
	}
	if _, err := os.Stat(fpOld); !os.IsNotExist(err) {
		t.Errorf(".mcp-key.old 仍存在: %v", err)
	}
}

func TestPruneOld_NotExist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fpOld := filepath.Join(dir, ".mcp-key.old")
	err := PruneOld(fpOld)
	if err == nil {
		t.Fatal("不存在文件应返回 error，实际 nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error 应可被 errors.Is(_, os.ErrNotExist) 识别, 实际 = %v", err)
	}
}

func TestPruneOld_Default(t *testing.T) {
	// 空字符串 → 用默认 config/.mcp-key.old
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	err = PruneOld("")
	if err == nil {
		t.Fatal("默认路径不存在应返回 error")
	}
}
