# 架构总览

面向开发者的心智模型：能力怎么被调用、manifest 里写的名字何时变成全局身份、为什么 `provides` 和 `requires` 写法不对称。

## 1. 调用链总图（capability mesh）

```
用户 / agent ──意图──▶ keystone dispatcher
                          │  按 canonical_name 路由 + 鉴权（出站签短期 JWT）
                          ▼
                   目标 app 的能力
                     ├─ backend.kind=mcp_tool     → app 的 /mcp（MCP tool 路径）
                     └─ backend.kind=http_endpoint → app 的 HTTP 路由（scoped JWT 保护）
                          │
        app 需要别人的能力时：
        app ──CallCapability("other-app.cap")(全名)──▶ keystone dispatcher ──▶ 另一 app
```

要点：

- app 之间**不直连**——所有跨 app 调用都经 keystone dispatcher 路由 + 鉴权。
- dispatcher 按 **canonical_name**（全局唯一全名）寻址。
- 能力有两种 backend：`mcp_tool`（走 app 的 MCP 端点）与 `http_endpoint`（走 app 的 HTTP 路由，自动套 scoped JWT，见 [auth-and-security.md](auth-and-security.md)）。

## 2. manifest 生命周期 + canonical 派生时机

作者全程**只写裸名**；全局唯一的 `canonical_name = <app_id>.<name>` 由平台派生（纯函数 `kstypes.Canonical(app_id, name)`，即 `app_id + "." + name`）。派生发生在：

```
作者                          ks build / publish        平台 install / ks register        SDK 运行时
────                          ─────────────────         ─────────────────────────        ──────────
manifest provides:            校验 manifest +           keystone 读 provides，            RegisterCapability(裸名)
  - name: hello   (裸名)      打 tarball                派生 canonical=<app_id>.hello，   → finalize 复用四象限
代码 RegisterCapability                                 写入【全局能力注册表】（权威），  → 生成 / 复用 MCP tool
  ("hello")       (裸名)                                挂 mcp_server / 反代
```

- **权威派生**：keystone 在 install（生产）/ `ks register`（本地联调）时把裸名派生成 `canonical_name` 写入全局注册表。
- **本地派生**：SDK 运行时在 `RegisterCapability` / `finalize` 内用同一 `kstypes.Canonical` 派生，用于本地 wiring——结果与平台一致。
- 两处都用同一纯函数，所以"名字即身份"在全链路自洽。

> **名字即身份**（见 [best-practices.md](best-practices.md)）：要破坏性改一个能力的输入 / 输出，**铸新名**（`x.search` → `x.search_v2`），依赖方按名迁移；`requires` **永不带 version**。

## 3. 为什么 provides 写裸名、requires 写全名（不对称）

| 段 | 写法 | 为什么 |
|---|---|---|
| `provides.capabilities[].name` | **裸名**（如 `hello`） | 你拥有它，`app_id` 已在 manifest 顶层声明，平台据此派生 `<app_id>.hello`——再写前缀就是重复。 |
| `requires.capabilities[].canonical_name` | **全名**（如 `other-app.publish`） | 你依赖别人的能力，没有"你的 app_id"上下文可派生，必须写全局唯一全名。 |

这是有意的不对称：派生只在"有 app_id 上下文"的 provides 侧发生。

## 4. 复用四象限（mcp_tool backend）

声明了 `backend.kind: mcp_tool` 的 capability，SDK 在 finalize 时按"有无独立 handler × 是否命中已有同名 `app.Tool`"裁决（详见 [decision-guide.md](decision-guide.md)）：

| 有独立 handler | 命中同名 `app.Tool` | 结果 |
|:---:|:---:|---|
| 是 | 是 | **报错**（真冲突） |
| 是 | 否 | **生成** MCP tool |
| 否 | 是 | **复用**（join，不新增） |
| 否 | 否 | **报错（orphan→error）**——声明了却无承载 |

> `orphan→error` 是 clean-break 的 BREAKING：声明了 capability 却既无 handler 也无同名 tool，启动期直接报错（替代旧的静默 warn）。
