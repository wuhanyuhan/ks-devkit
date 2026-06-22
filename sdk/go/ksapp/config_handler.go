package ksapp

// config_handler.go — MCP 配置 Schema HTTP 端点：
//
//   - GET /config-schema + GET /config-pubkey
//   - POST /ks-config/save + POST /ks-config/validate
//
// /ks-config/save 完整实现 9 步解密 + handleSave + 幂等 LRU；
// /ks-config/validate 仅走 1-5 步（解密 + schema 反序列化 + OnValidate/OnTest）。

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/crypto"
	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/keystore"
	kstypes "github.com/wuhanyuhan/ks-types"
)

// Result 是 Keystone 生态标准的 JSON 响应包装器。
//
// 成功：{"code": 0, "message": "", "data": <业务数据>}
// 失败：{"code": "ERR_XXX", "message": "人类可读描述", "data": null}
//
// Code 用 any 以兼容"成功时 int 0 / 失败时 string 错误码"两种形态；与 Keystone
// 后端 Gin 风格 core.Result 保持语义一致（而非类型一致，Gin 那侧 code 永远是 int，
// SDK 侧允许 string 错误码便于 client 侧直接 switch）。
type Result struct {
	Code    any    `json:"code"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// writeJSON 写 Content-Type + status + JSON 编码 Result。
// Content-Type 加 charset=utf-8。
// 编码失败只记录到服务端，不再改状态码（避免 header already written 风险）。
func writeJSON(w http.ResponseWriter, status int, body Result) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeErr 写错误响应的便捷封装：status + string code + message + 可选 data。
// 现有 code 约定：成功 0 / 失败 string enum。
func writeErr(w http.ResponseWriter, status int, code, message string, data any) {
	writeJSON(w, status, Result{Code: code, Message: message, Data: data})
}

// configSchemaHandler 返回 GET /config-schema 的 http.Handler。
//
// 契约：
//   - 200 + data.{schema, ui_schema, version="1.0.0"}
//   - 无 Config handle → 404 + ERR_NO_CONFIG_HANDLE
//
// Version 字段语义：这里返回的 "1.0.0" 是 MCP 声明的 schema 版本（即 SDK 生成的
// JSON Schema 结构版本），非配置协议版本。当 MCP 升级 schema 结构时应升版本。
func (a *App) configSchemaHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(a.configHandles) == 0 {
			writeErr(w, http.StatusNotFound, "ERR_NO_CONFIG_HANDLE",
				"当前 App 未注册任何 Config handle（调用 ksapp.NewConfigOn[T] 注册）", nil)
			return
		}
		// MVP：单 Config handle；多 handle 场景留待未来支持
		// （预计通过 query 参数或路径前缀区分）。这里直接取第一个。
		h := a.configHandles[0]
		schema, uiSchema := h.schemaJSON()

		resp := kstypes.ConfigSchemaResponse{
			Schema:   schema,
			UISchema: uiSchema,
			Version:  "1.0.0",
		}
		writeJSON(w, http.StatusOK, Result{Code: 0, Data: resp})
	})
}

// configCurrentHandler 返回 GET /ks-config/current 的 http.Handler。
//
// 只返回安全视图：非敏感字段回显真实值；敏感字段只返回 configured/masked 状态，
// 不返回明文。保存/校验时 mergeSecretActions 会处理 keep/clear/replace 语义。
func (a *App) configCurrentHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(a.configHandles) == 0 {
			writeErr(w, http.StatusNotFound, "ERR_NO_CONFIG_HANDLE",
				"当前 App 未注册任何 Config handle（调用 ksapp.NewConfigOn[T] 注册）", nil)
			return
		}
		handle := a.configHandles[0]
		values, secrets, configured := handle.currentRedacted()
		data := map[string]any{
			"configured": configured,
			"values":     values,
			"secrets":    secrets,
		}
		writeJSON(w, http.StatusOK, Result{Code: 0, Data: data})
	})
}

// configPubkeyHandler 返回 GET /config-pubkey 的 http.Handler。
//
// 契约：
//   - 200 + data.{pubkey (base64-std), fingerprint, algorithm="x25519-ecdh-aes256gcm-v1", created_at (RFC 3339 UTC)}
//
// keystore 加载失败属于 SDK programmer-error（部署错配 = env 未设 + secret 文件缺
// + fallback 目录不可写），getOrLoadKeystore 已 panic fail-fast，这里不需要额外
// error 分支。
func (a *App) configPubkeyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ks := a.getOrLoadKeystore()
		kp := ks.Primary
		resp := kstypes.ConfigPubkeyResponse{
			Pubkey:      base64.StdEncoding.EncodeToString(kp.Pubkey),
			Fingerprint: kp.Fingerprint,
			Algorithm:   "x25519-ecdh-aes256gcm-v1",
			CreatedAt:   kp.CreatedAt.UTC().Format(time.RFC3339),
		}
		writeJSON(w, http.StatusOK, Result{Code: 0, Data: resp})
	})
}

// configSaveHandler 返回 POST /ks-config/save 的 http.Handler。
//
// 流程（步骤 1-9，其中 6-9 由 handle.applySaveFromBytes 完成）：
//  1. decode payload → 失败 → 400 + ERR_SCHEMA（无效 request JSON）
//  2. IsValidIdempotencyKey 校验 → 不合法 → 400 + ERR_SCHEMA
//  3. 无 Config handle → 404 + ERR_NO_CONFIG_HANDLE
//  4. 幂等 LRU 命中 → 直接返回缓存 body（200 + 同 Content-Type）
//  5. decryptPayload（AAD 重建对比 + X25519 + HKDF + AES-GCM）→ 失败 → 400 + ERR_DECRYPT
//  6. handle.applySaveFromBytes → handleSave 全流程 + 错误码映射
//  7. 成功时把 response body 存入 LRU；失败不缓存
//
// 关于 ERR_NO_CONFIG_HANDLE：标准错误码 enum 未定义此 code；本 SDK 在
// 实现侧新增（与 /config-schema handler 保持一致）。
func (a *App) configSaveHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload kstypes.EncryptedConfigPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeErr(w, http.StatusBadRequest, "ERR_SCHEMA",
				"request body JSON 解析失败: "+err.Error(), nil)
			return
		}

		if !IsValidIdempotencyKey(payload.IdempotencyKey) {
			writeErr(w, http.StatusBadRequest, "ERR_SCHEMA",
				"idempotency_key 不是合法 uuid-v4 格式", nil)
			return
		}

		if len(a.configHandles) == 0 {
			writeErr(w, http.StatusNotFound, "ERR_NO_CONFIG_HANDLE",
				"当前 App 未注册任何 Config handle（调用 ksapp.NewConfigOn[T] 注册）", nil)
			return
		}
		handle := a.configHandles[0]

		// 幂等 LRU 命中：直接返回缓存的 success response
		lru := handle.ensureIdempLRU()
		if cached, ok := lru.Get(payload.IdempotencyKey); ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cached)
			return
		}

		// 解密（步骤 1-3）
		plaintext, err := a.decryptPayload(&payload)
		if err != nil {
			// ERR_DECRYPT 覆盖 AAD 不匹配 + fingerprint 不匹配 + GCM tag 失败
			// （不细分以防 oracle 攻击）。error message 只带通用描述，
			// 不要暴露 plaintext / privkey / dek 字节（安全规范）。
			writeErr(w, http.StatusBadRequest, "ERR_DECRYPT", err.Error(), nil)
			return
		}

		// 交由 handle 走完 handleSave（步骤 4-9）并映射错误码
		appliedVer, httpStatus, errCode, errMsg := handle.applySaveFromBytes(r.Context(), plaintext, payload.AADFields)
		if errCode != "" {
			writeErr(w, httpStatus, errCode, errMsg, nil)
			return
		}

		// 成功：构造 response body 并缓存到 LRU（成功才缓存）
		data := kstypes.ConfigApplyResult{
			AppliedAt: time.Now().UTC().Format(time.RFC3339),
			Version:   appliedVer,
		}
		body, err := json.Marshal(Result{Code: 0, Message: "配置已更新", Data: data})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "ERR_INTERNAL",
				"response 序列化失败: "+err.Error(), nil)
			return
		}
		lru.Put(payload.IdempotencyKey, body)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}

// configValidateHandler 返回 POST /ks-config/validate 的 http.Handler。
//
// 流程（仅走 save 步骤 1-5：AAD 对比 + X25519 + AES-GCM + Schema 反序列化 + OnValidate/OnTest）：
//  1. decode payload → 失败 → 400 + ERR_SCHEMA
//  2. idempotency_key 可选（明确不强制）
//  3. 无 Config handle → 404 + ERR_NO_CONFIG_HANDLE
//  4. decryptPayload → ERR_DECRYPT / 400
//  5. handle.validateFromBytes → ERR_SCHEMA / 422 或 ERR_VALIDATE / 422
//  6. 成功 → 200 + Result{code:0, message:"连接正常"}
func (a *App) configValidateHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload kstypes.EncryptedConfigPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeErr(w, http.StatusBadRequest, "ERR_SCHEMA",
				"request body JSON 解析失败: "+err.Error(), nil)
			return
		}

		// /validate 不校验 idempotency_key（明确可选）

		if len(a.configHandles) == 0 {
			writeErr(w, http.StatusNotFound, "ERR_NO_CONFIG_HANDLE",
				"当前 App 未注册任何 Config handle（调用 ksapp.NewConfigOn[T] 注册）", nil)
			return
		}
		handle := a.configHandles[0]

		plaintext, err := a.decryptPayload(&payload)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "ERR_DECRYPT", err.Error(), nil)
			return
		}

		errCode, errMsg := handle.validateFromBytes(r.Context(), plaintext)
		if errCode != "" {
			// 两种 code 都 422（ERR_SCHEMA 来自 plaintext JSON Unmarshal；ERR_VALIDATE 来自 OnValidate）
			writeErr(w, http.StatusUnprocessableEntity, errCode, errMsg, nil)
			return
		}

		writeJSON(w, http.StatusOK, Result{Code: 0, Message: "连接正常"})
	})
}

// decryptPayload 执行解密流程的步骤 1-3：
//
//  1. 按 aad_fields 三字段 kstypes.AADCanonicalBytes 重构 canonical；与 payload.AADCanonical
//     base64 解码后字节级比对，不一致 → error（handler 包装 ERR_DECRYPT）
//  2. 按 aad_fields.fingerprint 选 Primary / Old 密钥对（轮换支持）
//  3. X25519 + HKDF-SHA256 → kek
//  4. AES-256-GCM.decrypt(kek, nonce, ciphertext, aad=wantAAD)
//
// 任何步骤失败一律返回 error — 由调用方（handler）统一包装为 ERR_DECRYPT。
// error message 不含敏感字节（plaintext / privkey / dek），只带通用描述。
func (a *App) decryptPayload(p *kstypes.EncryptedConfigPayload) ([]byte, error) {
	// 1) 从 aad_fields 重建 canonical
	mcpID, _ := p.AADFields["mcp_server_id"].(string)
	verFloat, _ := p.AADFields["config_version"].(float64)
	fp, _ := p.AADFields["fingerprint"].(string)
	wantAAD := kstypes.AADCanonicalBytes(mcpID, uint64(verFloat), fp)

	gotAAD, err := base64.StdEncoding.DecodeString(p.AADCanonical)
	if err != nil {
		return nil, fmt.Errorf("aad_canonical base64 解码失败: %w", err)
	}
	if !bytes.Equal(wantAAD, gotAAD) {
		return nil, errors.New("aad_canonical 与 aad_fields 重建的 canonical 字节不一致")
	}

	// 2) 按 fingerprint 选 Primary / Old（轮换支持）
	ks := a.getOrLoadKeystore()
	var kp *keystore.Keypair
	switch {
	case ks.Primary != nil && ks.Primary.Fingerprint == fp:
		kp = ks.Primary
	case ks.Old != nil && ks.Old.Fingerprint == fp:
		kp = ks.Old
	default:
		return nil, fmt.Errorf("fingerprint %q 不匹配任何已加载的密钥", fp)
	}

	// 3) X25519 + HKDF → kek
	ephPub, err := base64.StdEncoding.DecodeString(p.EphemeralPubkey)
	if err != nil {
		return nil, fmt.Errorf("ephemeral_pubkey base64 解码失败: %w", err)
	}
	shared, err := crypto.X25519(kp.Privkey, ephPub)
	if err != nil {
		return nil, fmt.Errorf("X25519 ECDH 失败: %w", err)
	}
	kek, err := crypto.DeriveKEK(shared)
	if err != nil {
		return nil, fmt.Errorf("HKDF 派生 KEK 失败: %w", err)
	}

	// 4) AES-GCM 解密（AAD 用重建后的 canonical bytes）
	nonce, err := base64.StdEncoding.DecodeString(p.Nonce)
	if err != nil {
		return nil, fmt.Errorf("nonce base64 解码失败: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(p.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("ciphertext base64 解码失败: %w", err)
	}
	pt, err := crypto.DecryptAESGCM(kek, nonce, ct, wantAAD)
	if err != nil {
		return nil, fmt.Errorf("AES-GCM 解密失败: %w", err)
	}
	return pt, nil
}

// getOrLoadKeystore 懒加载 keystore（一次性 sync.Once 保护）。
//
// 失败时 panic：keystore.Load 失败意味着 env 未配 + secret 文件缺 + fallback 目录
// 不可写 —— 这是 SDK programmer-error 级的部署错配（SDK
// programmer-error 允许 panic fail-fast）。
//
// 不是 ksapp.Bootstrap 的一部分：/config-pubkey handler 可能在 Bootstrap 之前被
// 外部工具探测；这里的懒加载保证按需触发 + 缓存。
func (a *App) getOrLoadKeystore() *keystore.Keystore {
	a.keystoreOnce.Do(func() {
		configDir := bootstrapConfigDirPath()
		ks, err := keystore.Load(&keystore.LoadOptions{
			FallbackFile: filepath.Join(configDir, ".mcp-key"),
			FallbackOld:  filepath.Join(configDir, ".mcp-key.old"),
		})
		if err != nil {
			panic(fmt.Sprintf("ksapp: keystore 加载失败 — 部署错配（env/secret/fallback 均不可用）: %v", err))
		}
		a.keystore = ks
	})
	return a.keystore
}
