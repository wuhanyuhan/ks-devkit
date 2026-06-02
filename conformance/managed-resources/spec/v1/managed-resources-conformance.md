# Managed Resources Conformance Spec v1

## 1. 协议契约

### 1.1 端点

`GET <KS_GATEWAY_URL>/v1/apps/self/resources`

### 1.2 鉴权

请求头：

```
Authorization: Bearer <KS_APP_TOKEN>
Accept: application/json
```

### 1.3 成功响应（200，envelope 格式）

keystone 返回统一 envelope 结构：

```json
{
  "code": 0,
  "message": "",
  "data": {
    "app_id": "...",
    "version": "...",
    "install_id": 0,
    "env": {
      "DB_HOST": "...",
      "...": "..."
    }
  }
}
```

- `code` 为 0 表示业务成功；非 0 视为业务错误（与 HTTP 状态无关，需作为错误处理）。
- `data.env` 必须存在（可空 map）。keystone 把 secrets 平铺在 `data.env` 里——按 `manifest.managed_secrets[i].Inject` 指定的 env key 注入，不在响应里单独分类。
- `data.env` 的值允许是 string、number、bool 等 JSON 类型；客户端必须把它们 coerce 成 string（`os.Setenv` 只吃 string）。

### 1.4 错误响应

任何非 2xx 状态码 → 客户端必须将其作为可观测的错误（具体处置由 claimant 决定）。
envelope `code != 0` → 同样作为可观测错误。
JSON 解析失败、网络错误、`data.env` 缺失 → 同样作为可观测错误。

## 2. claimant 必须实现的行为

### 2.1 env 注入 setdefault 语义

对于响应 `data.env` 中的每个 key/value：
- 如果进程 env 中**已存在**该 key（即 `os.Getenv(k) != ""`），**不覆盖**
- 如果进程 env 中**不存在**或为空字符串，**设置**为响应值

这保证本地 `.env` 文件 / 开发者手动 export 的 env 优先于 keystone 注入。

### 2.2 5s 总超时（建议值）

claimant 应有合理超时。本套件验证"超时后客户端不挂死"，不强制具体秒数。

### 2.3 错误响应可观测

claimant 在以下情况必须返回 / 报告错误（"返回"可以是 error / panic / warn 日志 —— 由实现决定）：

- HTTP 4xx
- HTTP 5xx
- JSON 解析失败
- envelope `code != 0`
- 网络错误（连接拒绝 / 超时）
- `data.env` 缺失

### 2.4 类型 coerce

`data.env` 的值可能是 string / number（如 `3306`）/ bool。客户端必须 coerce 成 string。

参考实现：见 `ks-devkit/sdk/go/ksapp/keystoneclient/self_client.go` 中的 `coerceString` 函数（squad 运行时框架另有独立实现）。

## 3. 套件运行方式

参考 `conformance/managed-resources/server/` 提供的 mock keystone server 与
`conformance/managed-resources/expectations/` 定义的断言。Claimant 需提供
一个入口程序连接到 mock server，由套件驱动验证。
