package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func TestX25519ECDH_Roundtrip(t *testing.T) {
	t.Parallel()
	alicePriv, alicePub, err := GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	bobPriv, bobPub, err := GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	aliceShared, err := X25519(alicePriv, bobPub)
	if err != nil {
		t.Fatal(err)
	}
	bobShared, err := X25519(bobPriv, alicePub)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(aliceShared, bobShared) {
		t.Fatal("shared secrets differ")
	}
}

func TestHKDF_SHA256(t *testing.T) {
	t.Parallel()
	shared := make([]byte, 32)
	if _, err := rand.Read(shared); err != nil {
		t.Fatal(err)
	}
	kek, err := DeriveKEK(shared)
	if err != nil {
		t.Fatal(err)
	}
	if len(kek) != 32 {
		t.Errorf("KEK len = %d, want 32", len(kek))
	}
	kek2, err := DeriveKEK(shared)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kek, kek2) {
		t.Error("HKDF 不确定")
	}
}

func TestAESGCM_EncryptDecrypt(t *testing.T) {
	t.Parallel()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("hello secret config payload")
	aad := kstypes.AADCanonicalBytes("ks-mcp-test", 1, "0000:0000:0000:0000:0000:0000:0000:0000")
	ct, nonce, err := EncryptAESGCM(key, plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	if len(nonce) != 12 {
		t.Errorf("nonce len = %d, want 12", len(nonce))
	}
	pt, err := DecryptAESGCM(key, nonce, ct, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Errorf("decrypt mismatch")
	}
	wrongAAD := kstypes.AADCanonicalBytes("ks-mcp-test", 2, "0000:0000:0000:0000:0000:0000:0000:0000")
	if _, err := DecryptAESGCM(key, nonce, ct, wrongAAD); err == nil {
		t.Error("错误 AAD 应导致解密失败")
	}
}

func TestEndToEndEncryptDecrypt(t *testing.T) {
	t.Parallel()
	mcpPriv, mcpPub, err := GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	ephPriv, ephPub, err := GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	sharedF, err := X25519(ephPriv, mcpPub)
	if err != nil {
		t.Fatal(err)
	}
	kekF, err := DeriveKEK(sharedF)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte(`{"api_key":"sk-xxx"}`)
	fp := kstypes.Fingerprint(mcpPub)
	aad := kstypes.AADCanonicalBytes("ks-mcp-image-gen", 1, fp)
	ct, nonce, err := EncryptAESGCM(kekF, plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	sharedM, err := X25519(mcpPriv, ephPub)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sharedF, sharedM) {
		t.Fatal("ECDH shared 不一致")
	}
	kekM, err := DeriveKEK(sharedM)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := DecryptAESGCM(kekM, nonce, ct, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Errorf("MCP 解密 mismatch: %s", pt)
	}
}

func TestFingerprint_FromTestvectors(t *testing.T) {
	t.Parallel()
	// 全零 pubkey → sha256(32 zeros) 前 16 字节 = 66687aadf862bd776c8fc18b8e9f8e20
	pubkey := make([]byte, 32)
	got := kstypes.Fingerprint(pubkey)
	want := "6668:7aad:f862:bd77:6c8f:c18b:8e9f:8e20"
	if got != want {
		t.Errorf("Fingerprint(zero) = %q, want %q", got, want)
	}
	t.Logf("got %q (%s)", got, hex.EncodeToString(pubkey))
}

func TestDeriveX25519Pub_Roundtrip(t *testing.T) {
	t.Parallel()
	// 生成密钥对，然后用相同 priv 重新派生 pub，验证一致
	priv, pub, err := GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	if len(priv) != 32 {
		t.Fatalf("priv len = %d, want 32", len(priv))
	}
	if len(pub) != 32 {
		t.Fatalf("pub len = %d, want 32", len(pub))
	}
	priv2, pub2, err := DeriveX25519Pub(priv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(priv, priv2) {
		t.Errorf("DeriveX25519Pub priv mismatch")
	}
	if !bytes.Equal(pub, pub2) {
		t.Errorf("DeriveX25519Pub pub mismatch: got %x want %x", pub2, pub)
	}
}

// TestX25519_BadLengths 覆盖 X25519 参数长度校验分支。
func TestX25519_BadLengths(t *testing.T) {
	t.Parallel()
	good := make([]byte, 32)
	if _, err := X25519(make([]byte, 31), good); err == nil {
		t.Error("privkey 短 1 字节应返回 error")
	}
	if _, err := X25519(good, make([]byte, 33)); err == nil {
		t.Error("peerPubkey 长 1 字节应返回 error")
	}
}

// TestDeriveX25519Pub_BadLength 覆盖 DeriveX25519Pub 长度校验分支。
func TestDeriveX25519Pub_BadLength(t *testing.T) {
	t.Parallel()
	if _, _, err := DeriveX25519Pub(make([]byte, 16)); err == nil {
		t.Error("privkey 长度错应返回 error")
	}
}

// TestDeriveKEK_EmptyShared 覆盖 DeriveKEK 空入参分支。
func TestDeriveKEK_EmptyShared(t *testing.T) {
	t.Parallel()
	if _, err := DeriveKEK(nil); err == nil {
		t.Error("空 shared secret 应返回 error")
	}
}

// TestEncryptAESGCM_BadKEK 覆盖 EncryptAESGCM kek 长度校验分支。
func TestEncryptAESGCM_BadKEK(t *testing.T) {
	t.Parallel()
	if _, _, err := EncryptAESGCM(make([]byte, 16), []byte("hi"), nil); err == nil {
		t.Error("kek 16 字节应返回 error")
	}
}

// TestDecryptAESGCM_BadInputs 覆盖 DecryptAESGCM 长度校验与 GCM 失败分支。
func TestDecryptAESGCM_BadInputs(t *testing.T) {
	t.Parallel()
	good32 := make([]byte, 32)
	good12 := make([]byte, 12)
	if _, err := DecryptAESGCM(make([]byte, 16), good12, []byte("ct"), nil); err == nil {
		t.Error("kek 16 字节应返回 error")
	}
	if _, err := DecryptAESGCM(good32, make([]byte, 8), []byte("ct"), nil); err == nil {
		t.Error("nonce 8 字节应返回 error")
	}
	// kek/nonce 合法但 ciphertext 是垃圾，GCM Open 应失败
	if _, err := DecryptAESGCM(good32, good12, []byte("garbage-ciphertext-without-tag"), nil); err == nil {
		t.Error("非法密文应返回 error")
	}
}
