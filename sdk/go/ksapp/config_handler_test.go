package ksapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/crypto"
	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/keystore"
	kstypes "github.com/wuhanyuhan/ks-types"
)

// 专门给 config_handler 端点测试用的 payload 类型；和 config_handle_test.go 的
// testCfg 区分，避免同 App 两处 NewConfigOn[testCfg] 引起的重复注册 panic。
type handlerTestCfg struct {
	APIKey string `ksconfig:"required,type:password,label:API Key"`
}

type handlerSecretMergeCfg struct {
	APIKey string `json:"api_key" ksconfig:"required,type:password,label:API Key"`
	Model  string `json:"model" ksconfig:"default:default-model,label:模型"`
}

// 预生成的 X25519 测试 keypair（sdk/go/ksapp/crypto/x25519.go 一致实现）。
// 通过 env KSAPP_MCP_PRIVKEY_B64 注入，让 keystore.Load 走 env 分支，
// 避免写 fallback 文件到 CWD 污染测试（ScheduleWakeup / CI 环境也友好）。
const (
	testPrivkeyB64URL = "Gx19uYzMFkgASsaV6tcU9p68yPAkTxocenZAMMacxO8"
	testPubkeyB64Std  = "qImndoV6pjUvrjdVlneipSbjY3BTRig2sNP2iuczNmk="
)

// fingerprintRegex 匹配 "ab12:cd34:ef56:7890:1234:5678:9abc:def0" 指纹格式。
var fingerprintRegex = regexp.MustCompile(`^[0-9a-f]{4}(:[0-9a-f]{4}){7}$`)

// TestHandleConfigSchema_ReturnsJSONSchema 覆盖 /config-schema 正常路径：
//   - 注册了 Config handle 后端点应返回 200 + Result{code:0, data:{schema,ui_schema,version}}
//   - schema.properties 必须包含注册 struct 的字段（api_key）
func TestHandleConfigSchema_ReturnsJSONSchema(t *testing.T) {
	t.Parallel()
	app := New("schema-happy")
	_ = NewConfigOn(app, ConfigSpec[handlerTestCfg]{})

	req := httptest.NewRequest("GET", "/config-schema", nil)
	rec := httptest.NewRecorder()
	app.configSchemaHandler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Code    any            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	// code 规范要求 int 0；JSON 反序列化后是 float64(0)
	if code, ok := resp.Code.(float64); !ok || code != 0 {
		t.Errorf("code = %v (%T), 期望 0", resp.Code, resp.Code)
	}
	if resp.Data == nil {
		t.Fatal("data 不应为 nil")
	}
	if v, _ := resp.Data["version"].(string); v != "1.0.0" {
		t.Errorf("version = %q, 期望 \"1.0.0\"", v)
	}
	schema, ok := resp.Data["schema"].(map[string]any)
	if !ok {
		t.Fatalf("data.schema 类型不对: %T", resp.Data["schema"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties 类型不对: %T", schema["properties"])
	}
	if _, has := props["api_key"]; !has {
		t.Errorf("schema.properties 缺少 api_key, 实际键: %v", keysOf(props))
	}
	// ui_schema 也应存在（即便可能为空 map）
	if _, has := resp.Data["ui_schema"]; !has {
		t.Errorf("data 缺少 ui_schema 字段")
	}
}

// TestHandleConfigSchema_NoConfigHandle_Returns404 覆盖 /config-schema 边界：
// 未调用 NewConfigOn 时 app.configHandles 为空，端点应返回 404 + ERR_NO_CONFIG_HANDLE。
func TestHandleConfigSchema_NoConfigHandle_Returns404(t *testing.T) {
	t.Parallel()
	app := New("schema-empty")

	req := httptest.NewRequest("GET", "/config-schema", nil)
	rec := httptest.NewRecorder()
	app.configSchemaHandler().ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Fatalf("status = %d, 期望 404, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if code, _ := resp.Code.(string); code != "ERR_NO_CONFIG_HANDLE" {
		t.Errorf("code = %v, 期望 \"ERR_NO_CONFIG_HANDLE\"", resp.Code)
	}
}

func TestHandleConfigCurrent_RedactsSensitiveFields(t *testing.T) {
	t.Parallel()
	app := New("current-redacted")
	cfg := NewConfigOn(app, ConfigSpec[handlerSecretMergeCfg]{})
	cfg.ptr.Store(&handlerSecretMergeCfg{APIKey: "sk-secret-value", Model: "bocha"})

	req := httptest.NewRequest("GET", "/ks-config/current", nil)
	rec := httptest.NewRecorder()
	app.configCurrentHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code float64 `json:"code"`
		Data struct {
			Configured bool                      `json:"configured"`
			Values     map[string]any            `json:"values"`
			Secrets    map[string]map[string]any `json:"secrets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !resp.Data.Configured {
		t.Fatal("expected configured=true")
	}
	if resp.Data.Values["model"] != "bocha" {
		t.Fatalf("expected model echoed, got %+v", resp.Data.Values)
	}
	if _, leaked := resp.Data.Values["api_key"]; leaked {
		t.Fatalf("api_key must not be echoed in values: %+v", resp.Data.Values)
	}
	if configured, _ := resp.Data.Secrets["api_key"]["configured"].(bool); !configured {
		t.Fatalf("expected api_key configured state, got %+v", resp.Data.Secrets)
	}
	if masked, _ := resp.Data.Secrets["api_key"]["masked"].(string); strings.Contains(masked, "secret") || masked == "" {
		t.Fatalf("masked secret leaked or empty: %q", masked)
	}
}

func TestConfigSave_MergesSecretKeepAction(t *testing.T) {
	t.Parallel()
	app := New("secret-merge")
	var applied atomic.Pointer[handlerSecretMergeCfg]
	cfg := NewConfigOn(app, ConfigSpec[handlerSecretMergeCfg]{
		OnApply: func(_ context.Context, c *handlerSecretMergeCfg) error {
			applied.Store(c)
			return nil
		},
	})
	dir := t.TempDir()
	cfg.persistPath = filepath.Join(dir, "mcp-config.enc")
	cfg.dekPath = filepath.Join(dir, ".local-dek")
	dek, err := keystore.LoadOrGenerateDEK(cfg.dekPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateDEK: %v", err)
	}
	cfg.dek = dek
	cfg.ptr.Store(&handlerSecretMergeCfg{APIKey: "sk-old", Model: "old"})

	appliedVer, status, code, msg := cfg.applySaveFromBytes(
		context.Background(),
		[]byte(`{"model":"new","api_key":{"__ks_secret_action":"keep"}}`),
		map[string]any{"config_version": float64(2)},
	)
	if code != "" || status != 0 || msg != "" {
		t.Fatalf("unexpected error status=%d code=%q msg=%q", status, code, msg)
	}
	if appliedVer != 2 {
		t.Fatalf("applied version = %d, want 2", appliedVer)
	}
	got := applied.Load()
	if got == nil || got.APIKey != "sk-old" || got.Model != "new" {
		t.Fatalf("unexpected applied cfg: %+v", got)
	}
}

// TestHandleConfigPubkey_ReturnsX25519Pubkey 覆盖 /config-pubkey 正常路径：
//   - 通过 env 注入固定 privkey（避免 fallback 文件写 CWD）
//   - 端点返回 200 + Result{data:{pubkey, fingerprint, algorithm, created_at}}
//   - fingerprint 格式 "xxxx:xxxx:..." 8 段 x 4 hex
//   - algorithm = "x25519-ecdh-aes256gcm-v1"
func TestHandleConfigPubkey_ReturnsX25519Pubkey(t *testing.T) {
	// env 注入 → 不加 t.Parallel()
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", testPrivkeyB64URL)

	app := New("pubkey-happy")
	_ = NewConfigOn(app, ConfigSpec[handlerTestCfg]{})

	req := httptest.NewRequest("GET", "/config-pubkey", nil)
	rec := httptest.NewRecorder()
	app.configPubkeyHandler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code any `json:"code"`
		Data struct {
			Pubkey      string `json:"pubkey"`
			Fingerprint string `json:"fingerprint"`
			Algorithm   string `json:"algorithm"`
			CreatedAt   string `json:"created_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp.Data.Algorithm != "x25519-ecdh-aes256gcm-v1" {
		t.Errorf("algorithm = %q, 期望 \"x25519-ecdh-aes256gcm-v1\"", resp.Data.Algorithm)
	}
	if !fingerprintRegex.MatchString(resp.Data.Fingerprint) {
		t.Errorf("fingerprint = %q 不符合格式 ^[0-9a-f]{4}(:[0-9a-f]{4}){7}$", resp.Data.Fingerprint)
	}
	// pubkey 应该是 base64-std 32 字节（44 字符含 padding）
	pub, err := base64.StdEncoding.DecodeString(resp.Data.Pubkey)
	if err != nil {
		t.Fatalf("pubkey base64 解码失败: %v", err)
	}
	if len(pub) != 32 {
		t.Errorf("pubkey 长度 = %d, 期望 32", len(pub))
	}
	// env 注入的固定 privkey 对应的 pubkey 是 testPubkeyB64Std
	if resp.Data.Pubkey != testPubkeyB64Std {
		t.Errorf("pubkey = %q, 期望 %q", resp.Data.Pubkey, testPubkeyB64Std)
	}
	// created_at 应该是 RFC 3339 UTC（以 Z 结尾）
	if !strings.HasSuffix(resp.Data.CreatedAt, "Z") {
		t.Errorf("created_at = %q 不是 UTC (缺 Z 后缀)", resp.Data.CreatedAt)
	}
}

// TestApp_Mux_RegistersConfigEndpoints 覆盖 Mux 自动挂 4 端点：
//   - 注册了 Config handle + env 注入 privkey → /config-schema, /config-pubkey,
//     /ks-config/save, /ks-config/validate 四个路径都不应返回 404（stub 返 501 也算"注册成功"）
func TestApp_Mux_RegistersConfigEndpoints(t *testing.T) {
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", testPrivkeyB64URL)

	app := New("mux-register")
	_ = NewConfigOn(app, ConfigSpec[handlerTestCfg]{})

	mux := app.Mux()
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"config-schema", "GET", "/config-schema"},
		{"config-pubkey", "GET", "/config-pubkey"},
		{"ks-config-current", "GET", "/ks-config/current"},
		{"ks-config-save", "POST", "/ks-config/save"},
		{"ks-config-validate", "POST", "/ks-config/validate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var body *strings.Reader
			if c.method == "POST" {
				body = strings.NewReader("{}")
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(c.method, c.path, body)
			if c.method == "POST" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Errorf("%s %s 返回 404 - 路由未注册, body=%s", c.method, c.path, rec.Body.String())
			}
		})
	}
}

// TestApp_Mux_NoConfigHandles_NoEndpoints 覆盖 Mux 条件性挂载：
// 未调用 NewConfigOn 时不应注册任何 /config-* 端点。
func TestApp_Mux_NoConfigHandles_NoEndpoints(t *testing.T) {
	t.Parallel()
	app := New("mux-no-handles")
	mux := app.Mux()

	req := httptest.NewRequest("GET", "/config-schema", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("无 Config handle 时 /config-schema 应 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestApp_Bootstrap_InjectsDEK_Success 覆盖 Bootstrap 正常路径：
//   - 通过临时目录接管 fallback 文件路径避免污染 CWD
//   - Bootstrap 调用后每个 handle 应有 dek / persistPath / dekPath 字段
func TestApp_Bootstrap_InjectsDEK_Success(t *testing.T) {
	// env 注入 privkey 走 Env 分支 + 临时 CWD 避免 config/ 目录污染测试环境
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", testPrivkeyB64URL)
	dir := t.TempDir()
	mustChdir(t, dir)

	app := New("bootstrap-success")
	cfg := NewConfigOn(app, ConfigSpec[handlerTestCfg]{})

	app.Bootstrap()

	if !cfg.hasDEK() {
		t.Error("Bootstrap 后 dek 字段仍为 nil")
	}
	if cfg.persistPath == "" {
		t.Error("Bootstrap 后 persistPath 字段为空")
	}
	if cfg.dekPath == "" {
		t.Error("Bootstrap 后 dekPath 字段为空")
	}
	// 默认 persistPath 应在 config/mcp-config.enc
	expectedPersist := filepath.Join("config", "mcp-config.enc")
	if cfg.persistPath != expectedPersist {
		t.Errorf("persistPath = %q, 期望 %q", cfg.persistPath, expectedPersist)
	}
	// DEK 文件应已生成
	if _, err := os.Stat(cfg.dekPath); err != nil {
		t.Errorf(".local-dek 未生成: %v", err)
	}
}

func TestApp_Bootstrap_UsesManagedConfigDir(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	managedDir := filepath.Join(dir, "managed-config")
	t.Setenv("KS_APP_CONFIG_DIR", managedDir)

	app := New("bootstrap-managed-config-dir")
	cfg := NewConfigOn(app, ConfigSpec[handlerTestCfg]{})

	app.Bootstrap()

	expectedPersist := filepath.Join(managedDir, "mcp-config.enc")
	expectedDEK := filepath.Join(managedDir, ".local-dek")
	if cfg.persistPath != expectedPersist {
		t.Errorf("persistPath = %q, 期望 %q", cfg.persistPath, expectedPersist)
	}
	if cfg.dekPath != expectedDEK {
		t.Errorf("dekPath = %q, 期望 %q", cfg.dekPath, expectedDEK)
	}
	if _, err := os.Stat(expectedDEK); err != nil {
		t.Errorf("托管 config 目录下 .local-dek 未生成: %v", err)
	}
	if _, err := os.Stat(filepath.Join(managedDir, ".mcp-key")); err != nil {
		t.Errorf("托管 config 目录下 .mcp-key 未生成: %v", err)
	}
}

// TestApp_Bootstrap_NoConfigHandle_NoOp 覆盖 Bootstrap 优化路径：
// 未注册 Config handle 时 Bootstrap 不应尝试加载 keystore，不应写任何文件。
func TestApp_Bootstrap_NoConfigHandle_NoOp(t *testing.T) {
	// 不 Setenv，让 keystore.Load 若被误调会写 config/.mcp-key → 我们可断言无
	dir := t.TempDir()
	mustChdir(t, dir)

	app := New("bootstrap-noop")
	app.Bootstrap()

	// config/.mcp-key 不应存在（Bootstrap 直接 return，未触发 keystore.Load）
	if _, err := os.Stat(filepath.Join("config", ".mcp-key")); !os.IsNotExist(err) {
		t.Errorf("无 Config handle 时不应写 config/.mcp-key, got err=%v", err)
	}
}

// TestApp_Bootstrap_Idempotent 覆盖 Bootstrap 幂等语义：重复调用只执行一次。
// 判定方式：第一次 Bootstrap 后手动置 dek=nil；第二次调用因 sync.Once 不应重新注入。
func TestApp_Bootstrap_Idempotent(t *testing.T) {
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", testPrivkeyB64URL)
	dir := t.TempDir()
	mustChdir(t, dir)

	app := New("bootstrap-idempotent")
	cfg := NewConfigOn(app, ConfigSpec[handlerTestCfg]{})

	app.Bootstrap()
	if !cfg.hasDEK() {
		t.Fatal("第一次 Bootstrap 未注入 dek")
	}
	cfg.dek = nil // 手动清空模拟"如果 Bootstrap 再跑一次就会重新注入"
	app.Bootstrap()
	if cfg.hasDEK() {
		t.Error("Bootstrap 应幂等 — 第二次不应再次注入 dek")
	}
}

// TestApp_BootstrapConfigHandles_MissingDEK_Panics 覆盖 panic 路径：
// 直接调内部 bootstrapConfigHandles 但不注入 dek（模拟 hypothetical bug：
// LoadOrGenerateDEK 返回 nil 且无 error 的不可能路径），handle.hasDEK()=false → panic。
func TestApp_BootstrapConfigHandles_MissingDEK_Panics(t *testing.T) {
	t.Parallel()
	app := New("bootstrap-panic")
	_ = NewConfigOn(app, ConfigSpec[handlerTestCfg]{})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 bootstrapHandles 在 handle dek 仍 nil 时 panic")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "dek") {
			t.Errorf("panic 信息不含 \"dek\": %v", r)
		}
	}()
	// 只校验但不注入 dek（传 nil dek + 空 persistPath 模拟 bug）
	app.verifyConfigHandlesHaveDEK()
}

// ---- test helpers --------------------------------------------------------

// mustChdir 切到 dir 并在 test 结束后回到原 CWD。不加 t.Parallel()！
func mustChdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// keysOf 返回 map 的键切片，仅用于测试失败时的错误信息。
func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// ---- 端到端测试（/ks-config/save + /ks-config/validate）--------------

// encryptPayload 前端视角加密 helper：
//   - 生成 ephemeral X25519 key
//   - X25519(ephPriv, serverPub) → HKDF → kek
//   - 构造 AAD canonical
//   - AES-GCM encrypt plaintext + aad
//
// 返回可直接 POST 的 EncryptedConfigPayload。
func encryptPayload(t *testing.T, mcpID string, cfgVer uint64, serverPub []byte, fp string,
	plaintext []byte, idempKey string) kstypes.EncryptedConfigPayload {
	t.Helper()
	ephPriv, ephPub, err := crypto.GenerateX25519()
	if err != nil {
		t.Fatalf("GenerateX25519: %v", err)
	}
	shared, err := crypto.X25519(ephPriv, serverPub)
	if err != nil {
		t.Fatalf("X25519: %v", err)
	}
	kek, err := crypto.DeriveKEK(shared)
	if err != nil {
		t.Fatalf("DeriveKEK: %v", err)
	}
	aad := kstypes.AADCanonicalBytes(mcpID, cfgVer, fp)
	ct, nonce, err := crypto.EncryptAESGCM(kek, plaintext, aad)
	if err != nil {
		t.Fatalf("EncryptAESGCM: %v", err)
	}
	return kstypes.EncryptedConfigPayload{
		Algorithm:       "x25519-ecdh-aes256gcm-v1",
		EphemeralPubkey: base64.StdEncoding.EncodeToString(ephPub),
		Nonce:           base64.StdEncoding.EncodeToString(nonce),
		AADFields: map[string]any{
			"mcp_server_id":  mcpID,
			"config_version": float64(cfgVer), // JSON 数字默认 float64，模拟反序列化后
			"fingerprint":    fp,
		},
		AADCanonical:   base64.StdEncoding.EncodeToString(aad),
		Ciphertext:     base64.StdEncoding.EncodeToString(ct),
		IdempotencyKey: idempKey,
	}
}

// encodePayload 把 EncryptedConfigPayload 序列化为 JSON（request body）。
func encodePayload(t *testing.T, p kstypes.EncryptedConfigPayload) []byte {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal payload: %v", err)
	}
	return b
}

// bootstrapAppForSaveTest 准备一个已 Bootstrap 的 App：
//   - env 注入固定 privkey
//   - CWD 切到临时目录避免污染
//   - 返回 App / cfg handle / serverPub / fingerprint，便于构造 payload
func bootstrapAppForSaveTest(t *testing.T, id string, spec ConfigSpec[handlerTestCfg]) (*App, *Config[handlerTestCfg], []byte, string) {
	t.Helper()
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", testPrivkeyB64URL)
	dir := t.TempDir()
	mustChdir(t, dir)

	app := New(id)
	cfg := NewConfigOn(app, spec)
	app.Bootstrap()

	ks := app.getOrLoadKeystore()
	return app, cfg, ks.Primary.Pubkey, ks.Primary.Fingerprint
}

// validUUID4 一个固定合法的 uuid-v4，测试用。
const validUUID4 = "123e4567-e89b-42d3-a456-426614174000"

// TestConfigSave_EndToEnd_Success 覆盖 /ks-config/save 成功路径：
//   - 前端加密 → POST → 200 + code=0 + applied_at + version
//   - cfg.Get() 返回新值
//   - mcp-config.enc 已生成
func TestConfigSave_EndToEnd_Success(t *testing.T) {
	var applyCalled atomic.Int32
	app, cfg, serverPub, fp := bootstrapAppForSaveTest(t, "save-success", ConfigSpec[handlerTestCfg]{
		OnApply: func(ctx context.Context, c *handlerTestCfg) error {
			applyCalled.Add(1)
			return nil
		},
	})

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "sk-end-to-end"})
	payload := encryptPayload(t, "ks-mcp-test", 2, serverPub, fp, plaintext, validUUID4)
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code    any            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if f, _ := resp.Code.(float64); f != 0 {
		t.Errorf("code = %v, 期望 0", resp.Code)
	}
	if resp.Message != "配置已更新" {
		t.Errorf("message = %q, 期望 \"配置已更新\"", resp.Message)
	}
	if v, _ := resp.Data["version"].(float64); v != 2 {
		t.Errorf("version = %v, 期望 2", resp.Data["version"])
	}
	if at, _ := resp.Data["applied_at"].(string); !strings.HasSuffix(at, "Z") {
		t.Errorf("applied_at = %q, 期望 RFC3339 UTC (Z 后缀)", at)
	}
	// cfg.Get 返回新值
	got := cfg.Get()
	if got == nil || got.APIKey != "sk-end-to-end" {
		t.Errorf("cfg.Get() = %+v, 期望 APIKey sk-end-to-end", got)
	}
	if applyCalled.Load() != 1 {
		t.Errorf("OnApply 调用次数 = %d, 期望 1", applyCalled.Load())
	}
	// mcp-config.enc 已生成
	if _, err := os.Stat(cfg.persistPath); err != nil {
		t.Errorf("mcp-config.enc 未生成: %v", err)
	}
	// Content-Type 应带 charset
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "charset=utf-8") {
		t.Errorf("Content-Type = %q 缺 charset=utf-8", ct)
	}
}

func TestConfigSave_EndToEnd_DoesNotRunOnTest(t *testing.T) {
	var localValidated atomic.Int32
	var providerTested atomic.Int32
	var savedValidated atomic.Int32
	var applied atomic.Int32
	app, _, serverPub, fp := bootstrapAppForSaveTest(t, "save-no-provider-test", ConfigSpec[handlerTestCfg]{
		OnValidate: func(ctx context.Context, c *handlerTestCfg) error {
			localValidated.Add(1)
			return nil
		},
		OnTest: func(ctx context.Context, c *handlerTestCfg) error {
			providerTested.Add(1)
			return nil
		},
		OnSaveValidate: func(ctx context.Context, c *handlerTestCfg) error {
			savedValidated.Add(1)
			return nil
		},
		OnApply: func(ctx context.Context, c *handlerTestCfg) error {
			applied.Add(1)
			return nil
		},
	})

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "sk-save-only"})
	payload := encryptPayload(t, "ks-mcp-test", 2, serverPub, fp, plaintext, validUUID4)
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if localValidated.Load() != 0 {
		t.Fatalf("OnValidate calls=%d, want 0 when OnSaveValidate is present", localValidated.Load())
	}
	if providerTested.Load() != 0 {
		t.Fatalf("OnTest calls=%d, want 0", providerTested.Load())
	}
	if savedValidated.Load() != 1 {
		t.Fatalf("OnSaveValidate calls=%d, want 1", savedValidated.Load())
	}
	if applied.Load() != 1 {
		t.Fatalf("OnApply calls=%d, want 1", applied.Load())
	}
}

// TestConfigSave_IdempotencyHit 覆盖幂等语义：同 payload 发两次，第二次复用
// 缓存的 response；OnApply 只应被调用一次。
func TestConfigSave_IdempotencyHit(t *testing.T) {
	var applyCalled atomic.Int32
	app, _, serverPub, fp := bootstrapAppForSaveTest(t, "save-idemp", ConfigSpec[handlerTestCfg]{
		OnApply: func(ctx context.Context, c *handlerTestCfg) error {
			applyCalled.Add(1)
			return nil
		},
	})

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "sk-idemp"})
	payload := encryptPayload(t, "ks-mcp-test", 3, serverPub, fp, plaintext, validUUID4)
	body := encodePayload(t, payload)

	// 第一次
	req1 := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("第一次 status = %d, body = %s", rec1.Code, rec1.Body.String())
	}

	// 第二次：相同 body 相同 idempKey
	req2 := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("第二次 status = %d, body = %s", rec2.Code, rec2.Body.String())
	}

	// 两次 response body 应字节级一致（缓存快照）
	if !strings.EqualFold(rec1.Body.String(), rec2.Body.String()) {
		t.Errorf("幂等命中时两次 body 应一致:\n first=%s\nsecond=%s", rec1.Body.String(), rec2.Body.String())
	}

	// OnApply 只能被调用一次（第二次走 LRU 快照，完全 bypass handleSave）
	if applyCalled.Load() != 1 {
		t.Errorf("OnApply 调用次数 = %d, 期望 1 (幂等命中不应触发 OnApply)", applyCalled.Load())
	}
}

// TestConfigSave_InvalidIdempotencyKey 覆盖 uuid-v4 格式校验：
// idempotency_key 非合法 uuid-v4 → 400 + ERR_SCHEMA。
func TestConfigSave_InvalidIdempotencyKey(t *testing.T) {
	app, _, serverPub, fp := bootstrapAppForSaveTest(t, "save-bad-idemp", ConfigSpec[handlerTestCfg]{})

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "sk-1"})
	// 用合法 uuid-v1 格式（version != 4）
	payload := encryptPayload(t, "ks-mcp-test", 1, serverPub, fp, plaintext,
		"123e4567-e89b-12d3-a456-426614174000")
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, 期望 400, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != "ERR_SCHEMA" {
		t.Errorf("code = %q, 期望 ERR_SCHEMA", resp.Code)
	}
}

// TestConfigSave_AADMismatch 覆盖 AAD 篡改：篡改 aad_canonical 不动 aad_fields
// → ERR_DECRYPT / 400。
func TestConfigSave_AADMismatch(t *testing.T) {
	app, _, serverPub, fp := bootstrapAppForSaveTest(t, "save-aad-mismatch", ConfigSpec[handlerTestCfg]{})

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "sk-1"})
	payload := encryptPayload(t, "ks-mcp-test", 1, serverPub, fp, plaintext, validUUID4)
	// 篡改 aad_canonical：base64 后取第一个字符换成不同字符；保持合法 base64
	aadBytes, _ := base64.StdEncoding.DecodeString(payload.AADCanonical)
	if len(aadBytes) > 0 {
		aadBytes[0] ^= 0xff
	}
	payload.AADCanonical = base64.StdEncoding.EncodeToString(aadBytes)
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, 期望 400, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != "ERR_DECRYPT" {
		t.Errorf("code = %q, 期望 ERR_DECRYPT", resp.Code)
	}
}

// TestConfigSave_FingerprintMismatch 覆盖 fingerprint 不匹配：aad_fields.fingerprint
// 与 server Primary/Old 均不一致 → ERR_DECRYPT / 400。
func TestConfigSave_FingerprintMismatch(t *testing.T) {
	app, _, serverPub, _ := bootstrapAppForSaveTest(t, "save-fp-mismatch", ConfigSpec[handlerTestCfg]{})

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "sk-1"})
	// 用伪造 fingerprint（格式合法但不匹配任何密钥）
	fakeFp := "ffff:eeee:dddd:cccc:bbbb:aaaa:9999:8888"
	payload := encryptPayload(t, "ks-mcp-test", 1, serverPub, fakeFp, plaintext, validUUID4)
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, 期望 400, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != "ERR_DECRYPT" {
		t.Errorf("code = %q, 期望 ERR_DECRYPT", resp.Code)
	}
}

// TestConfigSave_OnValidateError 覆盖 OnValidate 返 error → 422 + ERR_VALIDATE。
func TestConfigSave_OnValidateError(t *testing.T) {
	app, cfg, serverPub, fp := bootstrapAppForSaveTest(t, "save-validate-err", ConfigSpec[handlerTestCfg]{
		OnValidate: func(ctx context.Context, c *handlerTestCfg) error {
			return fmt.Errorf("api_key 太短")
		},
	})

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "x"})
	payload := encryptPayload(t, "ks-mcp-test", 1, serverPub, fp, plaintext, validUUID4)
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, 期望 422, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != "ERR_VALIDATE" {
		t.Errorf("code = %q, 期望 ERR_VALIDATE", resp.Code)
	}
	if cfg.Get() != nil {
		t.Errorf("OnValidate 失败不应切内存 ptr, got %+v", cfg.Get())
	}
}

// TestConfigSave_OnApplyError_Rollback 覆盖 OnApply 失败回滚：
//   - 500 + ERR_APPLY
//   - cfg.Get() 回到 oldCfg（nil，首次 save）
//   - mcp-config.enc 被删除
func TestConfigSave_OnApplyError_Rollback(t *testing.T) {
	app, cfg, serverPub, fp := bootstrapAppForSaveTest(t, "save-apply-rollback", ConfigSpec[handlerTestCfg]{
		OnApply: func(ctx context.Context, c *handlerTestCfg) error {
			return fmt.Errorf("apply boom")
		},
	})

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "sk-fail"})
	payload := encryptPayload(t, "ks-mcp-test", 1, serverPub, fp, plaintext, validUUID4)
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, 期望 500, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != "ERR_APPLY" {
		t.Errorf("code = %q, 期望 ERR_APPLY", resp.Code)
	}
	if cfg.Get() != nil {
		t.Errorf("OnApply 失败应回滚内存 ptr 到 nil, got %+v", cfg.Get())
	}
	if _, err := os.Stat(cfg.persistPath); !os.IsNotExist(err) {
		t.Errorf("首次 save OnApply 失败应删除 mcp-config.enc, got err=%v", err)
	}
}

// TestConfigSave_SchemaUnmarshalError 覆盖 plaintext 非合法 JSON：
// 解密成功但内容无法 Unmarshal 到 T → 422 + ERR_SCHEMA。
func TestConfigSave_SchemaUnmarshalError(t *testing.T) {
	app, _, serverPub, fp := bootstrapAppForSaveTest(t, "save-schema-err", ConfigSpec[handlerTestCfg]{})

	// plaintext 是非法 JSON（不是 {…} 也不是合法 JSON 值）
	plaintext := []byte("not-json-at-all {[")
	payload := encryptPayload(t, "ks-mcp-test", 1, serverPub, fp, plaintext, validUUID4)
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, 期望 422, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != "ERR_SCHEMA" {
		t.Errorf("code = %q, 期望 ERR_SCHEMA", resp.Code)
	}
}

// TestConfigSave_BadPayloadJSON 覆盖 request body JSON 解析失败：
// 整个 body 不是合法 JSON → 400 + ERR_SCHEMA。
func TestConfigSave_BadPayloadJSON(t *testing.T) {
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", testPrivkeyB64URL)
	dir := t.TempDir()
	mustChdir(t, dir)
	app := New("save-bad-json")
	_ = NewConfigOn(app, ConfigSpec[handlerTestCfg]{})
	app.Bootstrap()

	req := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader("not-json{"))
	rec := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, 期望 400, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != "ERR_SCHEMA" {
		t.Errorf("code = %q, 期望 ERR_SCHEMA", resp.Code)
	}
}

// TestConfigSave_NoConfigHandle_Returns404 覆盖 /ks-config/save 无 Config handle：
// 合法 payload 但 App 没注册任何 handle → 404 + ERR_NO_CONFIG_HANDLE。
func TestConfigSave_NoConfigHandle_Returns404(t *testing.T) {
	t.Parallel()
	app := New("save-no-handle")
	// 直接构造 payload 不需要真实加密：先过 uuid 校验，才会命中 handle 分支
	payload := kstypes.EncryptedConfigPayload{
		Algorithm:       "x25519-ecdh-aes256gcm-v1",
		EphemeralPubkey: base64.StdEncoding.EncodeToString(make([]byte, 32)),
		Nonce:           base64.StdEncoding.EncodeToString(make([]byte, 12)),
		AADFields:       map[string]any{"mcp_server_id": "x", "config_version": float64(1), "fingerprint": "f"},
		AADCanonical:    base64.StdEncoding.EncodeToString([]byte("x")),
		Ciphertext:      base64.StdEncoding.EncodeToString([]byte("x")),
		IdempotencyKey:  validUUID4,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, 期望 404, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != "ERR_NO_CONFIG_HANDLE" {
		t.Errorf("code = %q, 期望 ERR_NO_CONFIG_HANDLE", resp.Code)
	}
}

// TestConfigValidate_EndToEnd_Success 覆盖 /ks-config/validate 成功路径：
//   - 返回 200 + code=0 + message="连接正常"
//   - cfg.Get() 仍是 nil（未落盘不切换）
//   - mcp-config.enc 不应生成
func TestConfigValidate_EndToEnd_Success(t *testing.T) {
	var applyCalled atomic.Int32
	app, cfg, serverPub, fp := bootstrapAppForSaveTest(t, "validate-success", ConfigSpec[handlerTestCfg]{
		OnValidate: func(ctx context.Context, c *handlerTestCfg) error { return nil },
		OnApply: func(ctx context.Context, c *handlerTestCfg) error {
			applyCalled.Add(1)
			return nil
		},
	})

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "sk-validate"})
	payload := encryptPayload(t, "ks-mcp-test", 1, serverPub, fp, plaintext, validUUID4)
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/validate", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	app.configValidateHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if f, _ := resp.Code.(float64); f != 0 {
		t.Errorf("code = %v, 期望 0", resp.Code)
	}
	if resp.Message != "连接正常" {
		t.Errorf("message = %q, 期望 \"连接正常\"", resp.Message)
	}
	if cfg.Get() != nil {
		t.Errorf("validate 不应切换内存 ptr, got %+v", cfg.Get())
	}
	if applyCalled.Load() != 0 {
		t.Errorf("validate 不应触发 OnApply, called=%d", applyCalled.Load())
	}
	if _, err := os.Stat(cfg.persistPath); !os.IsNotExist(err) {
		t.Errorf("validate 不应落盘 mcp-config.enc, err=%v", err)
	}
}

// TestConfigValidate_OnValidateError 覆盖 /ks-config/validate OnValidate 失败：
// 422 + ERR_VALIDATE。
func TestConfigValidate_OnValidateError(t *testing.T) {
	app, _, serverPub, fp := bootstrapAppForSaveTest(t, "validate-err", ConfigSpec[handlerTestCfg]{
		OnValidate: func(ctx context.Context, c *handlerTestCfg) error {
			return fmt.Errorf("api_key 无效")
		},
	})

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "bad"})
	// /validate 不校验 idempotency_key 格式，但我们仍传合法值以免干扰
	payload := encryptPayload(t, "ks-mcp-test", 1, serverPub, fp, plaintext, validUUID4)
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/validate", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	app.configValidateHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, 期望 422, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != "ERR_VALIDATE" {
		t.Errorf("code = %q, 期望 ERR_VALIDATE", resp.Code)
	}
}

// TestConfigValidate_AllowsMissingIdempotencyKey 覆盖如下约定：
// /validate idempotency_key 可选，空字符串不应触发 ERR_SCHEMA。
func TestConfigValidate_AllowsMissingIdempotencyKey(t *testing.T) {
	app, _, serverPub, fp := bootstrapAppForSaveTest(t, "validate-nokey", ConfigSpec[handlerTestCfg]{
		OnValidate: func(ctx context.Context, c *handlerTestCfg) error { return nil },
	})

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "sk-1"})
	payload := encryptPayload(t, "ks-mcp-test", 1, serverPub, fp, plaintext, "")
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/validate", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	app.configValidateHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, 期望 200 (idempotency_key 可选), body=%s", rec.Code, rec.Body.String())
	}
}

// TestConfigSave_OldKeyRotation 覆盖密钥轮换场景：aad_fields.fingerprint 指向
// Old 密钥时应成功解密。用 LoadOptions 直接塞 Old。
func TestConfigSave_OldKeyRotation(t *testing.T) {
	t.Setenv("KSAPP_MCP_PRIVKEY_B64", testPrivkeyB64URL)
	dir := t.TempDir()
	mustChdir(t, dir)

	// 生成独立的 Old keypair
	oldPriv, oldPub, err := crypto.GenerateX25519()
	if err != nil {
		t.Fatalf("GenerateX25519: %v", err)
	}
	oldFp := kstypes.Fingerprint(oldPub)

	// 手工构造 Keystore 注入到 App：Primary 走 env，Old 手工构造
	app := New("save-old-rotation")
	cfg := NewConfigOn(app, ConfigSpec[handlerTestCfg]{})
	app.Bootstrap() // 正常加载 Primary
	// Bootstrap 后手动加入 Old
	app.keystore.Old = &keystore.Keypair{
		Privkey:     oldPriv,
		Pubkey:      oldPub,
		Fingerprint: oldFp,
	}

	plaintext, _ := json.Marshal(handlerTestCfg{APIKey: "sk-old"})
	// 用 Old pubkey 加密 + Old fingerprint 做 AAD
	payload := encryptPayload(t, "ks-mcp-test", 1, oldPub, oldFp, plaintext, validUUID4)
	body := encodePayload(t, payload)

	req := httptest.NewRequest("POST", "/ks-config/save", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	app.configSaveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Old key 轮换路径 status = %d, 期望 200, body=%s", rec.Code, rec.Body.String())
	}
	got := cfg.Get()
	if got == nil || got.APIKey != "sk-old" {
		t.Errorf("cfg.Get() = %+v, 期望 APIKey sk-old", got)
	}
}
