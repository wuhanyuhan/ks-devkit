package ksapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/keystore"
	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/ksconfig"
)

// ConfigSpec[T] 是 NewConfigOn 的入参；承载配置校验、显式测试和应用回调。
type ConfigSpec[T any] struct {
	// OnValidate 是本地/业务校验回调。
	// 兼容性：
	//   - OnTest 为空时，/ks-config/validate 只调用 OnValidate。
	//   - OnSaveValidate 为空时，/ks-config/save 继续用 OnValidate 做保存前校验。
	OnValidate func(ctx context.Context, cfg *T) error
	// OnTest 只由 /ks-config/validate 在 OnValidate 成功后调用。
	OnTest func(ctx context.Context, cfg *T) error
	// OnSaveValidate 只由 /ks-config/save 在持久化前调用。
	OnSaveValidate func(ctx context.Context, cfg *T) error
	OnApply        func(ctx context.Context, cfg *T) error
}

// Config[T] 是类型化配置 handle；内部用 atomic.Pointer 保证热路径 Get() 无锁。
type Config[T any] struct {
	ptr      atomic.Pointer[T]
	spec     ConfigSpec[T]
	name     string // 注册时记录的 Go 类型名（如 "main.Config"），暴露给接口 typeName()
	schema   ksconfig.JSONSchema
	uiSchema ksconfig.UISchema
	writeMu  sync.Mutex
	// 独立 DEK 落盘机制
	// persistPath / dekPath / dek 由 ksapp.Bootstrap 注入；
	// 单元测试可直接赋值。
	// dek 为 32 字节对称密钥，与 X25519 私钥**完全无关**。
	persistPath string // mcp-config.enc 完整路径（含目录 + 文件名）
	dekPath     string // .local-dek 完整路径
	dek         []byte // 32 字节 DEK；handleSave / loadPersisted 调用前必须由
	// Bootstrap（或单元测试）注入；nil 时这两个方法直接 panic（fail-fast）。
	// /ks-config/save 的幂等 LRU（作用域 per handle）
	// 懒初始化，idempLRUOnce 保护；容量 64 / TTL 10min。
	idempLRU     *idempotencyLRU
	idempLRUOnce sync.Once
}

// anyConfigHandle 是非类型化接口，让 App 能通过 []anyConfigHandle 遍历。
// 方法小写保持 SDK 包私有；handler 通过包内调用这些方法。
type anyConfigHandle interface {
	typeName() string
	schemaJSON() (ksconfig.JSONSchema, ksconfig.UISchema)
	currentRedacted() (map[string]any, map[string]any, bool)
	// bootstrapPersistence 由 App.Bootstrap 在启动期调用，注入持久化所需的
	// persistPath / dekPath / dek 字段。
	bootstrapPersistence(persistPath, dekPath string, dek []byte)
	// hasDEK 校验 bootstrapPersistence 是否已注入 dek；false → App.Bootstrap
	// panic fail-fast（校验入口）。
	hasDEK() bool
	// applySaveFromBytes 被 /ks-config/save handler 调用。
	// 输入：plaintext = 解密后 JSON 字节；aadFields = request 原 aad_fields（取 config_version）。
	// 返回：appliedVer = 成功写入的 config_version；httpStatus 0 + errCode "" 表示成功；
	//   否则 httpStatus / errCode / errMsg 承载错误响应。
	applySaveFromBytes(ctx context.Context, plaintext []byte, aadFields map[string]any) (uint64, int, string, string)
	// validateFromBytes 被 /ks-config/validate handler 调用。
	// 仅走 Schema 反序列化 + OnValidate/OnTest；不落盘、不切换、不触发 OnApply。
	// 返回：errCode "" 表示成功；否则承载错误响应（httpStatus 由 handler 按 code 映射）。
	validateFromBytes(ctx context.Context, plaintext []byte) (string, string)
	restorePersisted(ctx context.Context) (bool, error)
	// ensureIdempLRU 返回 per-handle 的幂等 LRU（作用域 per handle）；懒初始化。
	ensureIdempLRU() *idempotencyLRU
}

// NewConfigOn 注册配置 handle 到 App。
// 同一 App 同一 T 只能调用一次，重复调 panic。
// app 传 nil 时 panic，给出友好错误。
func NewConfigOn[T any](app *App, spec ConfigSpec[T]) *Config[T] {
	if app == nil {
		panic("ksapp.NewConfigOn: app 不能为 nil")
	}
	var zero T
	tname := reflect.TypeOf(zero).String()
	app.registerConfigHandleSlot(tname) // panic on duplicate
	schema, uiSchema, err := ksconfig.ReflectConfigSchema[T]()
	if err != nil {
		panic(fmt.Sprintf("ksapp.NewConfigOn: schema 反射失败: %v", err))
	}
	h := &Config[T]{
		spec:     spec,
		name:     tname,
		schema:   schema,
		uiSchema: uiSchema,
	}
	app.configHandles = append(app.configHandles, h)
	return h
}

// typeName 返回注册时的 Go 类型名（如 "main.Config"），用于日志 / 路由分发。
// 包内私有：通过 anyConfigHandle 接口暴露给 App 遍历。
func (c *Config[T]) typeName() string { return c.name }

// schemaJSON 返回生成的 JSON Schema 与 UI Schema。供 /config-schema 端点序列化返回。
// 包内私有：通过 anyConfigHandle 接口暴露给 handler。
func (c *Config[T]) schemaJSON() (ksconfig.JSONSchema, ksconfig.UISchema) {
	return c.schema, c.uiSchema
}

func (c *Config[T]) currentRedacted() (map[string]any, map[string]any, bool) {
	current := c.ptr.Load()
	if current == nil {
		return map[string]any{}, c.emptySecretStates(), false
	}
	raw := map[string]any{}
	if b, err := json.Marshal(current); err == nil {
		_ = json.Unmarshal(b, &raw)
	}
	secrets := map[string]any{}
	for _, name := range c.sensitiveFieldNames() {
		value, _ := raw[name].(string)
		delete(raw, name)
		configured := value != ""
		secrets[name] = map[string]any{
			"configured": configured,
			"masked":     maskSecret(value),
		}
	}
	return raw, secrets, true
}

func (c *Config[T]) emptySecretStates() map[string]any {
	secrets := map[string]any{}
	for _, name := range c.sensitiveFieldNames() {
		secrets[name] = map[string]any{
			"configured": false,
			"masked":     "",
		}
	}
	return secrets
}

func (c *Config[T]) sensitiveFieldNames() []string {
	names := make([]string, 0)
	for name, rawUI := range c.uiSchema {
		ui, ok := rawUI.(map[string]any)
		if !ok {
			continue
		}
		if sensitive, _ := ui["ks:sensitive"].(bool); sensitive {
			names = append(names, name)
			continue
		}
		if widget, _ := ui["ui:widget"].(string); widget == "password" {
			names = append(names, name)
		}
	}
	return names
}

func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "********"
	}
	return value[:2] + "********" + value[len(value)-2:]
}

// bootstrapPersistence 在 App.Bootstrap 启动期调用，注入持久化所需的路径与密钥。
// 把"NewConfigOn 注册但 Bootstrap 漏调"的暴露
// 从"首次 save panic"提前到"启动完成前 panic"。
//
// 单元测试若要绕过 Bootstrap 直接设置字段也可以（字段私有但同包可访问）；
// 生产路径上始终由 App.Bootstrap 触发。
func (c *Config[T]) bootstrapPersistence(persistPath, dekPath string, dek []byte) {
	c.persistPath = persistPath
	c.dekPath = dekPath
	c.dek = dek
}

// hasDEK 返回 dek 是否已注入。App.verifyConfigHandlesHaveDEK 在 Bootstrap 末尾
// 逐个校验，任一返回 false 直接 panic（fail-fast）。
func (c *Config[T]) hasDEK() bool { return c.dek != nil }

// Get 返回当前配置快照；未初始化返回 nil。
func (c *Config[T]) Get() *T {
	return c.ptr.Load()
}

// handleValidate 被 /ks-config/validate 端点调用。
func (c *Config[T]) handleValidate(ctx context.Context, newCfg *T) error {
	if c.spec.OnValidate != nil {
		if err := c.spec.OnValidate(ctx, newCfg); err != nil {
			return err
		}
	}
	if c.spec.OnTest != nil {
		return c.spec.OnTest(ctx, newCfg)
	}
	return nil
}

// handleSave 被 /ks-config/save 端点调用。完整流程：
//
//  1. OnSaveValidate 或 OnValidate 校验 newCfg —— 失败 → ERR_VALIDATE，不写盘、不切 atomic ptr
//  2. JSON 序列化 newCfg —— 失败 → ERR_SCHEMA
//  3. EncryptConfigToFile（DEK + AES-GCM）写盘 —— 失败 → ERR_STORE
//  4. atomic.Pointer 切换内存配置
//  5. OnApply（业务侧应用，如重建 LLM client）
//     失败 → 内存 + 磁盘双回滚到 oldCfg：
//     - 内存：c.ptr.Store(oldCfg)
//     - 磁盘：oldCfg != nil → 重新加密落盘；oldCfg == nil（首次 save）→ os.Remove
//
// 调用前提：c.dek 必须已注入（由 Bootstrap 或测试代码赋值）；nil 时直接 panic
// （fail-fast）。SDK 调用方漏注入是 programmer-error，
// 应在启动期立即暴露而非运行期静默丢失数据。
func (c *Config[T]) handleSave(ctx context.Context, newCfg *T) error {
	if c.dek == nil {
		panic("ksapp: handleSave 调用前未注入 dek（Bootstrap 缺陷）— 必须先 LoadOrGenerateDEK 后赋 c.dek")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.spec.OnSaveValidate != nil {
		if err := c.spec.OnSaveValidate(ctx, newCfg); err != nil {
			return errBizf("ERR_VALIDATE", err)
		}
	} else if c.spec.OnValidate != nil {
		if err := c.spec.OnValidate(ctx, newCfg); err != nil {
			return errBizf("ERR_VALIDATE", err)
		}
	}
	if err := c.persistEncrypted(newCfg); err != nil {
		return err
	}
	oldCfg := c.ptr.Load()
	c.ptr.Store(newCfg)
	if c.spec.OnApply != nil {
		if err := c.spec.OnApply(ctx, newCfg); err != nil {
			c.ptr.Store(oldCfg)
			c.rollbackPersisted(oldCfg)
			return errBizf("ERR_APPLY", err)
		}
	}
	return nil
}

// persistEncrypted 序列化 newCfg + DEK 加密落盘（handleSave 步骤 2-3）。
// 调用前提：c.dek != nil（handleSave 已 panic 校验，本方法不重复）。
func (c *Config[T]) persistEncrypted(newCfg *T) error {
	data, err := json.Marshal(newCfg)
	if err != nil {
		return errBizf("ERR_SCHEMA", err)
	}
	if err := keystore.EncryptConfigToFile(c.persistPath, c.dek, data); err != nil {
		return errBizf("ERR_STORE", err)
	}
	return nil
}

// rollbackPersisted OnApply 失败后把磁盘回滚到 oldCfg。
//   - oldCfg == nil → 删 persistPath（首次 save 失败）
//   - oldCfg != nil → 用旧值重新加密写盘（已成功过的重写）
//
// 任何错误吞掉但加注释说明状态影响：磁盘可能留新值、内存留旧值
// （或 nil），下次启动 loadPersisted 会拿回 newCfg，由用户重新触发 save 修复。
func (c *Config[T]) rollbackPersisted(oldCfg *T) {
	if oldCfg == nil {
		// 首次 save 失败：删除已写的新文件。失败的话磁盘留新值，
		// 下次启动 loadPersisted 会拿回 newCfg，由用户重新触发 save 修复。
		// TODO: 引入结构化 logger 后此处 Warn 记录删除失败。
		_ = os.Remove(c.persistPath)
		return
	}
	// 后续 save 失败：用旧值重写。失败的话磁盘留新值 / 内存留旧值，
	// 下次启动 loadPersisted 会拿回 newCfg，由用户重新触发 save 修复。
	oldData, mErr := json.Marshal(oldCfg)
	if mErr != nil {
		// oldCfg 来自上一次成功的 handleSave，理论上一定能 Marshal；
		// 真 marshal 失败属于不可恢复的程序状态，与 panic 等价但这里不 panic。
		// TODO: 引入结构化 logger 后此处 Warn 记录 marshal 失败。
		return
	}
	// TODO: 引入结构化 logger 后此处 Warn 记录回滚写盘失败。
	_ = keystore.EncryptConfigToFile(c.persistPath, c.dek, oldData)
}

// ensureIdempLRU 懒初始化 per-handle 的幂等 LRU（作用域约定）。
// 用 sync.Once 保证并发首次调用也只初始化一次；后续调用直接返回缓存。
//
// 容量 idempotencyLRUCapacity=64 / TTL idempotencyLRUTTL=10min。
func (c *Config[T]) ensureIdempLRU() *idempotencyLRU {
	c.idempLRUOnce.Do(func() {
		c.idempLRU = newIdempotencyLRU(idempotencyLRUCapacity, idempotencyLRUTTL)
	})
	return c.idempLRU
}

// applySaveFromBytes 从解密后的 plaintext JSON 字节恢复 newCfg 并走 handleSave
// 完整流程（save 步骤 4-9）。
//
// 返回值约定：
//   - 成功：(appliedVer, 0, "", "")
//   - 失败：(0, httpStatus, errCode, errMsg) — 由 handler 写 Result
//
// 错误分支：
//   - plaintext JSON 反序列化失败 → 422 + ERR_SCHEMA
//   - handleSave 返回 BizError：按 Code 映射 HTTP status（ERR_VALIDATE/ERR_SCHEMA→422，
//     ERR_STORE/ERR_APPLY→500）
//   - handleSave 返回非 BizError（理论上不应发生）→ 500 + ERR_INTERNAL
//
// appliedVer 从 aadFields["config_version"] 读取（JSON 数字默认 float64 → uint64）；
// 即 response.data.version。
func (c *Config[T]) applySaveFromBytes(ctx context.Context, plaintext []byte, aadFields map[string]any) (uint64, int, string, string) {
	plaintext, mergeErr := c.mergeSecretActions(plaintext)
	if mergeErr != nil {
		return 0, http.StatusUnprocessableEntity, "ERR_SCHEMA", mergeErr.Error()
	}
	var newCfg T
	if err := json.Unmarshal(plaintext, &newCfg); err != nil {
		return 0, http.StatusUnprocessableEntity, "ERR_SCHEMA", err.Error()
	}
	if err := c.handleSave(ctx, &newCfg); err != nil {
		var be *BizError
		if errors.As(err, &be) {
			status := http.StatusInternalServerError
			switch be.Code {
			case "ERR_VALIDATE", "ERR_SCHEMA":
				status = http.StatusUnprocessableEntity
			case "ERR_STORE", "ERR_APPLY":
				status = http.StatusInternalServerError
			}
			return 0, status, be.Code, be.Error()
		}
		return 0, http.StatusInternalServerError, "ERR_INTERNAL", err.Error()
	}
	ver, _ := aadFields["config_version"].(float64)
	return uint64(ver), 0, "", ""
}

// validateFromBytes 从解密后的 plaintext JSON 字节恢复 newCfg 并仅走 handleValidate。
// 不落盘、不切换 atomic ptr、不触发 OnApply。
//
// 返回值约定：
//   - 成功：("", "")
//   - 失败：(errCode, errMsg) — handler 按 code 映射 HTTP status
//     （ERR_SCHEMA→422 / ERR_VALIDATE→422）
func (c *Config[T]) validateFromBytes(ctx context.Context, plaintext []byte) (string, string) {
	var mergeErr error
	plaintext, mergeErr = c.mergeSecretActions(plaintext)
	if mergeErr != nil {
		return "ERR_SCHEMA", mergeErr.Error()
	}
	var newCfg T
	if err := json.Unmarshal(plaintext, &newCfg); err != nil {
		return "ERR_SCHEMA", err.Error()
	}
	if err := c.handleValidate(ctx, &newCfg); err != nil {
		return "ERR_VALIDATE", err.Error()
	}
	return "", ""
}

func (c *Config[T]) mergeSecretActions(plaintext []byte) ([]byte, error) {
	sensitive := c.sensitiveFieldNames()
	if len(sensitive) == 0 {
		return plaintext, nil
	}
	var incoming map[string]any
	if err := json.Unmarshal(plaintext, &incoming); err != nil {
		return plaintext, nil
	}

	current := map[string]any{}
	if old := c.ptr.Load(); old != nil {
		if b, err := json.Marshal(old); err == nil {
			_ = json.Unmarshal(b, &current)
		}
	}

	for _, name := range sensitive {
		raw, exists := incoming[name]
		oldValue, hasOld := current[name]
		switch v := raw.(type) {
		case map[string]any:
			action, _ := v["__ks_secret_action"].(string)
			switch action {
			case "keep":
				if hasOld {
					incoming[name] = oldValue
				} else {
					delete(incoming, name)
				}
			case "clear":
				incoming[name] = ""
			default:
				return nil, fmt.Errorf("敏感字段 %s 的操作不支持: %s", name, action)
			}
		case string:
			if v == "" && hasOld {
				incoming[name] = oldValue
			}
		default:
			if !exists && hasOld {
				incoming[name] = oldValue
			}
		}
		if !exists && hasOld {
			incoming[name] = oldValue
		}
	}
	return json.Marshal(incoming)
}

// loadPersisted 从 mcp-config.enc 加载并 JSON 反序列化为 *T；任一步失败返回 nil。
//
// 典型用法：进程启动期 ksapp.Bootstrap 调用，把磁盘上的最近一次 save 恢复到内存
// atomic ptr。
//
// 失败时返回 nil 而非 error：调用方只关心"有/无可恢复的持久化配置"，详细原因
// （文件不存在 / 损坏被备份 / DEK 不对）由 keystore 层负责日志。
//
// 调用前提：c.dek 必须已注入；nil 时直接 panic（fail-fast）。
func (c *Config[T]) loadPersisted() *T {
	if c.dek == nil {
		panic("ksapp: loadPersisted 调用前未注入 dek（Bootstrap 缺陷）— 必须先 LoadOrGenerateDEK 后赋 c.dek")
	}
	if c.persistPath == "" {
		return nil
	}
	data, err := keystore.DecryptConfigFromFile(c.persistPath, c.dek)
	if err != nil {
		return nil
	}
	var cfg T
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

func (c *Config[T]) restorePersisted(ctx context.Context) (bool, error) {
	cfg := c.loadPersisted()
	if cfg == nil {
		return false, nil
	}
	c.ptr.Store(cfg)
	if c.spec.OnApply != nil {
		if err := c.spec.OnApply(ctx, cfg); err != nil {
			c.ptr.Store(nil)
			return false, err
		}
	}
	return true, nil
}
