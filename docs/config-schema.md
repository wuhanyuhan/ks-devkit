# 类型化配置（config-schema）

> `install.yaml` 退役后的官方替代。**任何需要 API Key / OAuth Secret / 连接串的 app，上架前都要用它声明配置。**

## 1. 为什么有它

旧的 `install.yaml` 让开发者把"安装时要填什么"写成一份静态清单，平台照单渲染。它的问题：明文、改了要重装、没有结构校验、没有连接测试。

config-schema 改用**代码声明配置**，换来四件事：

- **端到端加密下发**：管理员在安装向导里填的值，用 app 的公钥信封加密后才下发；SDK 用本地 DEK（AES-GCM）落盘，磁盘上不存明文。
- **运行时可改不重启**：保存即热切换（atomic 快照），无需重启容器。
- **自带连接测试**：`OnValidate` 回调在管理员点"测试连接"时触发——用填入的密钥真去探活下游，失败当场报错。
- **结构化校验**：字段类型 / 必填 / 范围 / 枚举由 SDK 反射成 JSON Schema，安装向导照此渲染表单并前置校验。

## 2. 心智模型

```
作者声明配置结构（带字段约束 + 标签）
        │  SDK 反射
        ▼
JSON Schema + UI Schema ──GET /config-schema──▶ 平台安装向导渲染表单
                                                       │ 管理员填
   app 公钥 ──GET /config-pubkey──▶ 向导用公钥信封加密 │
                                                       ▼
                       POST /ks-config/save（密文）◀───┘
                              │ SDK 解密
                              ▼
         OnValidate → 落盘(DEK+AES-GCM) → atomic 热切换 → OnApply
                              │
                              ▼
        业务代码用 cfg.Get() 读当前快照 —— 不再读 env
```

要点：**app 从 config handle 读，不再读环境变量**。配置的真相在加密文件 `mcp-config.enc`（DEK 存 `.local-dek`），由 SDK 管理。

## 3. Go 用法

> 下面片段与 [`sdk/go/examples/config-demo`](../sdk/go/examples/config-demo) 一致，已编译验证。

```go
type Config struct {
    APIKey     string `ksconfig:"required,type:password,label_zh:API 密钥,label_en:API Key,hint:从控制台获取"`
    Region     string `ksconfig:"enum:cn|us|eu,default:cn,label:区域"`
    MaxRetries int    `ksconfig:"default:3,min:1,max:10,label:最大重试次数"`
}

cfg := ksapp.NewConfigOn(app, ksapp.ConfigSpec[Config]{
    OnValidate: func(ctx context.Context, c *Config) error {
        // 连接测试：用 c.APIKey 探活下游服务；返回 error → 安装向导显示校验失败
        return nil
    },
    OnApply: func(ctx context.Context, c *Config) error {
        // 应用新配置（如重建下游 client）；失败会内存 + 磁盘双回滚
        return nil
    },
})

// 业务里读当前快照（未配置时为 nil）：
if c := cfg.Get(); c != nil {
    use(c.APIKey)
}
```

API（`sdk/go/ksapp/config_handle.go`）：

- `NewConfigOn[T any](app *App, spec ConfigSpec[T]) *Config[T]` —— 注册配置 handle；同一 app 同一 `T` 只能调一次（重复 panic）。
- `ConfigSpec[T]{ OnValidate, OnApply func(ctx context.Context, cfg *T) error }` —— 两个回调都可选。
- `(*Config[T]).Get() *T` —— 当前快照；未 save 时返回 `nil`。

## 4. Python 用法

> Python 用 pydantic `BaseModel` 约束配置结构，字段元信息走 `Field(...)`（**不是** Go 的 struct tag——映射见 [§5](#5-字段声明文法)）。回调签名也与 Go 不同：**只收 `cfg` 一个参数，且必须是 `async`**。

```python
from typing import Literal
from pydantic import BaseModel, Field
from ks_app import App, ConfigSpec, new_config

class MyConfig(BaseModel):
    api_key: str = Field(
        title="API 密钥",
        description="从控制台获取",
        json_schema_extra={"ui:widget": "password"},   # 等价 Go type:password
    )
    region: Literal["cn", "us", "eu"] = Field("cn", title="区域")  # 等价 Go enum + default
    max_retries: int = Field(3, ge=1, le=10, title="最大重试次数") # 等价 Go min/max

async def on_validate(cfg: MyConfig) -> None:   # 连接测试；抛异常 → 校验失败
    ...

async def on_apply(cfg: MyConfig) -> None:       # 应用新配置
    ...

cfg = new_config(app, MyConfig, ConfigSpec(on_validate=on_validate, on_apply=on_apply))
current = cfg.get()   # MyConfig | None
```

API（`sdk/python/src/ks_app/config_handle.py`，从 `ks_app` 顶层导出 `Config` / `ConfigSpec` / `new_config`）：

- `new_config(app, model_cls, spec=None) -> Config[T]` —— `model_cls` 必须是 pydantic `BaseModel` 子类（否则 `TypeError`）；同一 app 同一 model 只能调一次（重复 `ValueError`）。
- `ConfigSpec(on_validate=None, on_apply=None)` —— 两个回调都是 `async (cfg) -> None`，都可选。
- `(Config[T]).get() -> T | None` —— 当前快照；未 save 时返回 `None`。

## 5. 字段声明文法

Go 用 `ksconfig:"..."` struct tag：`,` 分隔规则，`:` 分隔 `key:value`，`|` 在 enum 内表 OR。完整 key（源：`sdk/go/ksapp/ksconfig/tag.go`）与 Python（pydantic）对照：

| key | 作用 | Go 示例 | Python 等价 |
|---|---|---|---|
| `required` | 必填（bool flag，不接值） | `required` | 字段无默认值即必填 |
| `sensitive` | 敏感字段（bool flag） | `sensitive` | `Field(json_schema_extra={"ks:sensitive": True})` |
| `type` | UI 控件：`password`/`textarea`/`radio`/`select` | `type:password` | `Field(json_schema_extra={"ui:widget": "password"})` |
| `label` | 通用标签 | `label:区域` | `Field(title="区域")` |
| `label_zh` / `label_en` | 中 / 英标签 | `label_zh:区域` | pydantic 用单一 `title` |
| `hint` | 帮助文案 | `hint:从控制台获取` | `Field(description="从控制台获取")` |
| `default` | 默认值 | `default:cn` | `Field("cn")` |
| `enum` | 枚举（`\|` 分隔） | `enum:cn\|us\|eu` | `Literal["cn","us","eu"]` |
| `min` / `max` | 数值范围 | `min:1,max:10` | `Field(ge=1, le=10)` |
| `minLen` / `maxLen` | 字符串长度 | `minLen:8` | `Field(min_length=8, max_length=64)` |
| `pattern` | 正则 | `pattern:^sk-` | `Field(pattern="^sk-")` |
| `item_schema` | 数组元素类型名 | `item_schema:Server` | （MVP 仅 `list[原始类型]`） |
| `show_when` | 条件显示 DSL | `show_when:mode==advanced` | 见 `ksconfig/show_when.py` |

> ⚠️ `label` / `label_zh` / `label_en` / `hint` 的值里不能含 `,` `:` `|`（解析期 panic）；`min`/`max`/`minLen`/`maxLen` 必须是整数。

> **MVP 范围**：两端都只支持顶层扁平字段（string / int / bool / float / enum / `list[原始类型]`）。嵌套结构体 / `Optional` / `Union` 会在反射期报错（Python 抛 `NotImplementedError`）。

## 6. 敏感字段

标了 `type:password`（→ `ui:widget=password`）或 `sensitive`（→ `ks:sensitive=true`）的字段：

- **平台 mask 显示**：`GET /ks-config/current` 回读时只返回 `{configured, masked}`，不回明文（`config_handle.go` `maskSecret`）。
- **保存时的 secret action**：管理员不重填密钥时，向导发 `{"__ks_secret_action": "keep"}` 保留旧值；发 `{"__ks_secret_action": "clear"}` 清空。SDK 在 `mergeSecretActions` 里据此合并（`config_handle.go:354`）。

## 7. 加密与落盘

- **信封加密**：向导先 `GET /config-pubkey` 拿 app 的配置公钥（x25519），用它加密配置后才 `POST /ks-config/save`。
- **本地落盘**：SDK 解密后用本地 **DEK + AES-GCM** 重新加密写 `mcp-config.enc`（DEK 存 `.local-dek`，由 `keystore.EncryptConfigToFile` 落盘）。两个文件都不含明文配置。
- **保存流程**（`POST /ks-config/save`）：解密 → `OnValidate` → 落盘 → atomic 切换内存 → `OnApply`；`OnApply` 失败 → 内存 + 磁盘双回滚。
- **校验流程**（`POST /ks-config/validate`）：解密 → `OnValidate`，**不落盘、不切换、不触发 OnApply**。

端点一览（SDK 检测到 config handle 时自动挂载）：

| 端点 | 用途 |
|---|---|
| `GET /config-schema` | 拉 JSON Schema + UI Schema（向导渲染表单） |
| `GET /config-pubkey` | 拉 app 公钥（向导做信封加密） |
| `POST /ks-config/save` | 下发加密配置（校验 → 落盘 → 热切换 → OnApply） |
| `POST /ks-config/validate` | 仅校验（OnValidate，不落盘） |
| `GET /ks-config/current` | 回读当前快照（敏感字段 mask）；Go 提供 |

## 8. TypeScript 现状（诚实标注）

⚠️ **TS SDK 暂无类型化 config-schema。** `sdk/typescript/src/config.ts` 只有内部的 `AppConfig` / `resolveConfig`，index 不导出 `ConfigSpec` / `newConfig`。需要管理员配置的 TS app 当前走 `managed_resources` / `platform_services` 的注入或自管 env。这是已知能力缺口，待补齐。

## 9. 与 managed_resources / platform_services 的边界

config-schema 管的是**管理员要在安装向导里填的值**（密钥、区域、阈值……）。平台托管的资源 / 服务地址走另两个 manifest 段。哪类字段进哪里，见 [migration-checklist.md](migration-checklist.md) 的「`install.yaml` 配置职责退役 → 按性质归位」表。
