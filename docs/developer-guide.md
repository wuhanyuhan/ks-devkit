# Keystone 应用开发指南

本指南面向 Keystone 生态的第三方应用开发者。你将学会如何使用 `ks` 命令行工具创建、开发、调试和发布一个 Keystone 应用。

## 前置条件

- **Docker**：运行本地 Keystone 开发环境
- **Go 1.26+**（Go 应用）或 **Python 3.11+**（Python 应用）
- 一个终端

## 1. 安装 ks CLI

**macOS（Homebrew）：**

```bash
brew tap wuhanyuhan/tap
brew install ks-devkit
```

**Linux / macOS（手动）：**

```bash
curl -fsSL https://raw.githubusercontent.com/wuhanyuhan/ks-devkit/master/install.sh | bash
```

验证安装：

```bash
ks --version
```

## 2. 启动本地开发环境

```bash
ks dev
```

首次运行会自动拉取 Keystone 镜像并启动完整的开发环境（Keystone API + MySQL + Redis），约需 1-2 分钟。

启动完成后你会看到：

```
✓ 开发环境已启动
  Keystone API:  http://localhost:9988
  MySQL:         localhost:3306
  Redis:         localhost:6379

  管理后台账号:  admin / admin（首次登录需改密）
  API Key:       ks_deadbeefdeadbeefdeadbeefdeadbeef
```

> 本地环境预配置了一个测试 Agent（id=9001）和一个指向 `localhost:8080` 的 MCP Server，方便你立即开始调试。

## 3. 创建应用项目

```bash
ks init my-app --type=app --lang=go
cd my-app
```

生成的项目结构（app/squad × 语言）：

```
my-app/
  main.go                    # 应用入口（capability mesh：RegisterCapability + 复用四象限）
  manifest.yaml              # 应用清单（裸名 provides、auth、权限声明等）
  Dockerfile                 # 容器构建文件
  go.mod                     # Go 依赖（已引入 ksapp SDK）
  .ks/manifest.schema.json   # IDE 校验用 schema（ks init 自动写）
  .vscode/settings.json      # VS Code YAML 扩展接线（自动写）
```

### 应用类型

四类型怎么选（含 tool vs capability、复用四象限）见 [decision-guide.md](decision-guide.md)。简表：

| 类型 | 说明 | 命令 |
|------|------|------|
| `app` | 外部 MCP service，对外供给一批能力（合并旧 service + extension） | `ks init my-app --type=app --lang=go` |
| `squad` | 有 LLM 负责人编排的专家团队（外部 service） | `ks init my-squad --type=squad --lang=go` |
| `agent` | 平台内角色助手（runtime none、无独立进程，旧 assistant 改名） | `ks init my-agent --type=agent` |
| `skill` | 可被语义召回的技能资源（挂载） | `ks init my-skill --type=skill` |

app/squad 支持 `--lang=go|python|ts`；agent/skill 为 langless（无独立进程，忽略 `--lang`）。

## 4. 理解项目结构

### main.go（Go）

```go
package main

import "github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp"

func main() {
    // WithKeystoneAuth：secure-by-default 入站校验。WithManifest：读 provides 做四象限 wiring。
    app := ksapp.New("my-app",
        ksapp.WithManifest("manifest.yaml"),
        ksapp.WithKeystoneAuth(),
        ksapp.WithVersion("0.1.0"),
    )

    // RegisterCapability 收【裸名】（与 manifest provides[].name 一致），canonical 由平台派生。
    // Run() 内 finalize 触发复用四象限（生成/复用/冲突/orphan→error）。
    app.RegisterCapability("hello", func(ctx ksapp.CapabilityContext, args map[string]any) (any, error) {
        name, _ := args["name"].(string)
        if name == "" {
            name = "world"
        }
        return map[string]any{"message": "Hello, " + name + "!"}, nil
    })

    app.Run()
}
```

### main.py（Python）

```python
from ks_app import App, CapabilityContext

# keystone_auth=True：secure-by-default 入站校验。manifest_path：读 provides 做四象限 wiring。
app = App(id="my-app", keystone_auth=True, version="0.1.0", manifest_path="manifest.yaml")

@app.capability("hello")
async def hello(ctx: CapabilityContext, args: dict) -> dict:
    name = args.get("name") or "world"
    return {"message": f"Hello, {name}!"}

if __name__ == "__main__":
    app.run()
```

### manifest.yaml

```yaml
id: my-app
name: my-app
version: 0.1.0
type: app
summary: my-app 应用
compatibility:
  keystone: ">=1.0.0"
# 鉴权 secure-by-default；本地裸跑 export KS_APP_AUTH_MODE=insecure
auth:
  mode: keystone_jwks
runtime:
  mode: container          # 容器内固定监听 8080，不声明 port
  health_check: /healthz
  health_check_url: "http://localhost:8080/mcp"
  image: "my-app:0.1.0"
# provides 写【裸名】name，全局 canonical=<app_id>.<name> 由平台派生（别写前缀）
provides:
  capabilities:
    - name: hello
      display_name: 示例能力
      description: 向调用方问好。
      execution_mode: sync
      side_effect_level: none        # decision_mode 默认由它派生，别手填
      backend:
        kind: mcp_tool
        tool_name: hello
      input_schema:
        type: object
        properties:
          name: { type: string }
      output_schema:
        type: object
        properties:
          message: { type: string }
permissions:                     # 最小权限：只声明用到的维度；filesystem/user_context 是高风险维度，用到再加
  network: { level: none }       # none | restricted | unrestricted
  llm: { level: none }           # none | host_proxy | self_managed
```

> 不再有 `mount.service.auto_register_mcp`/`mcp_endpoint`、`runtime.port`、`canonical_name` 前缀、`cost_hint`/`typical_latency_ms`/`intent_summary`/`input_nl` 等字段——clean-break（一次不向后兼容的契约重构）已砍。字段含义与最佳实践见 [decision-guide.md](decision-guide.md) + [best-practices.md](best-practices.md)。

### IDE

`ks init` 已在项目里写好 `.ks/manifest.schema.json` + `.vscode/settings.json`。装 VS Code「Red Hat YAML」扩展后，编辑 manifest.yaml 即得字段校验 + 悬浮文档 + 自动补全（schema 由 ks-types 字段注释生成，单一真值源）。手动刷新：`ks schema --write`；其他编辑器手动接 schema 时指向 `.ks/manifest.schema.json` 即可。

## 5. 开发你的工具

编辑 `main.go`，添加自己的工具。每个工具由三部分组成：名称、描述和处理函数。

```go
app.Tool("查天气", "查询指定城市的天气", func(ctx context.Context, params map[string]any) (any, error) {
    city, _ := params["city"].(string)
    if city == "" {
        return nil, fmt.Errorf("请提供城市名称")
    }

    // 你的业务逻辑：调用天气 API、查数据库等
    weather := fetchWeather(city)

    return map[string]any{
        "city":        city,
        "temperature": weather.Temp,
        "condition":   weather.Condition,
    }, nil
})
```

### 链式注册

```go
app.Tool("工具A", "描述A", handlerA).
    Tool("工具B", "描述B", handlerB).
    Tool("工具C", "描述C", handlerC)
```

### 中间件

```go
app.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        slog.Info("请求", "method", r.Method, "path", r.URL.Path)
        next.ServeHTTP(w, r)
    })
})
```

### 自定义健康检查

```go
app.HealthCheck("database", func() error {
    return db.Ping()
})
```

### 自定义路由

```go
app.Handle("GET /api/items", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.Write([]byte(`{"items": []}`))
}))
```

### 调用大模型（LLM Relay）

如果你的工具需要调用大模型，用 SDK 的 LLM relay 客户端——Keystone 自动注入 relay token，你无需管理 API key。

**启用方式**：在 manifest 的 `permissions.llm` 维度声明 `level: host_proxy`：

```yaml
permissions:
  llm: { level: host_proxy }   # 走 Keystone relay 代理大模型（none=不调用；self_managed=自带 key 自管）
```

Keystone 安装应用时若检测到 `llm.host_proxy`，会自动注入 `KS_RELAY_TOKEN`（及兼容别名 `KEYSTONE_RELAY_TOKEN`）环境变量，`app.LLM()` 据此打 relay 端点。本地联调时自行 `export KS_RELAY_TOKEN=<token>` 与 `export KS_GATEWAY_URL=<keystone 地址>` 即可。

> 注：旧的 `mount.service.llm_mode: keystone_relay` 声明已随 clean-break 移除（`mount.service` 整段已废），改由 `permissions.llm: host_proxy` 表达；下方运行时 API（`app.LLM()` 等）不变。

代码示例：

**Go**

```go
llm := app.LLM()
resp, err := llm.Chat(ctx, ksapp.ChatRequest{
    Model:    "gpt-4o-mini",
    Messages: []ksapp.Message{{Role: "user", Content: "请总结以下内容..."}},
})
// 流式
err = llm.StreamChat(ctx, req, func(chunk ksapp.Chunk) {
    fmt.Print(chunk.DeltaContent)
})
```

**Python**

```python
llm = app.llm()
resp = await llm.chat(ChatRequest(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "请总结以下内容..."}],
))
# 流式
async for chunk in llm.stream_chat(req):
    print(chunk.delta_content, end="", flush=True)
```

**TypeScript**

```typescript
const llm = app.llm();
const resp = await llm.chat({ model: "gpt-4o-mini", messages: [{ role: "user", content: "请总结以下内容..." }] });
// 流式
for await (const chunk of llm.streamChat(req)) {
    process.stdout.write(chunk.delta_content ?? "");
}
```

Keystone 会自动注入 relay token，你无需管理 API key。完整 API 及错误类型说明见 [sdk-api-reference.md](sdk-api-reference.md#llm-relay-client-appllm--appllm)。

## 6. 本地调试

### 启动应用

```bash
# Go
go mod tidy
go run .

# Python
pip install ks-app-sdk
python main.py
```

应用默认监听 `localhost:8080`，自动注册以下端点：

| 端点 | 用途 |
|------|------|
| `GET /healthz` | 存活检查 |
| `GET /readyz` | 就绪检查 |
| `GET /meta` | 应用元信息 + 工具列表 |
| `POST /mcp` | MCP 协议端点（Streamable HTTP） |

### 发测试请求

本地 Keystone 的 `dev-agent`（id=9001）已经配置指向你的 `localhost:8080`。直接发请求让 Agent 调用你的工具：

```bash
curl -X POST http://localhost:9988/v1/open/chat/completions \
  -H "X-API-Key: ks_deadbeefdeadbeefdeadbeefdeadbeef" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": 9001,
    "messages": [{"role": "user", "content": "调用 hello 工具，name 是 Alice"}]
  }'
```

请求链路：

```
你的 curl 请求
  → Keystone Gateway (localhost:9988)
    → dev-agent 判断需要调用工具
      → MCP 协议调用你的应用 (localhost:8080/mcp)
        → 执行 hello 工具
      ← 返回工具结果
    ← Agent 组织回复
  ← 返回给你
```

### 调试技巧

```bash
# 查看 Keystone 调用日志
docker compose -f ~/.ks/runtime/docker-compose.yaml logs -f keystone-dev

# 验证你的工具是否被发现
curl http://localhost:8080/mcp/tools/list

# 验证应用健康状态
curl http://localhost:8080/healthz

# 查看应用元信息
curl http://localhost:8080/meta

# 重置 Keystone 数据库（清空所有数据重来）
cd ~/.ks/runtime && docker compose down -v && docker compose up -d
```

### 迭代流程

```
修改代码 → 重启 go run . → 发 curl 请求验证
```

Keystone 不需要重启，只需要重启你自己的应用。

### 能力网格联调（register / refresh-meta）

curl 经 dev-agent 调 MCP tool 开箱即用。要联调【能力网格】（dispatcher 调用、跨 app、long_running、on-behalf-of）需要把你的 manifest 注册进本地 keystone：

```bash
ks register       # 读 manifest，external_endpoint 模式注册进本地 keystone（你的容器自己跑）
# 改 manifest/代码后：
ks refresh-meta   # 幂等重同步（先卸载再重装），免 docker compose down -v
```

> ⚠️ **本地能力网格注册当前不可用（已知限制）**
>
> `ks dev` 自带的 `keystone-dev:0.3.0` 是 pre-clean-break 镜像，**不支持**能力网格的本地注册语义（去前缀派生 / 能力注册）。因此 `ks register` / `ks refresh-meta` 注册的能力**在本地不生效**。
>
> - **本地验证 capability**：暂直接调你的 app 的 MCP 端点（`ks test` 的运行时探测 / 手动 `curl`），不经能力网格。
> - **能力网格联调**：待 keystone 侧发布 clean-break 版 keystone-dev 镜像后打通（届时 `internal/resources/runtime/docker-compose.yaml` 的 image tag 会一并 bump）。
> - **生产不受此限**：模板里的 `compatibility.keystone: ">=1.0.0"` 指的是**生产 keystone 平台**的版本要求，与本地联调镜像 `0.3.0` 是两条独立的轨道（详见 [compatibility.md](compatibility.md)）。

## 7. 权限声明

`manifest.yaml` 中的 `permissions` 声明你的应用需要的权限。Keystone 在安装时会审核这些权限。

| 维度 | 级别 | 说明 |
|------|------|------|
| `network` | `none` / `restricted` / `unrestricted` | 网络访问 |
| `llm` | `none` / `host_proxy` / `self_managed` | 大模型调用（`host_proxy`=走 Keystone relay 代理；`self_managed`=自带 key 自管） |
| `filesystem` | `none` / `read_scoped` / `scoped` / `full` | 文件系统访问 |
| `user_context` | `none` / `read` / `write` | 用户上下文信息 |

**最小权限原则**：只申请你实际需要的权限。高权限会在审核时被重点检查。

## 8. 安装配置（可选）

> `install.yaml` 已废弃（clean-break 重构中退役）。应用配置（API Key、服务地址等）改用 SDK 的 **config-schema**：在代码里声明配置项（Go `ksapp` 的 `ConfigSpec` / Python `ks_app.Config`），平台在安装向导展示、加密下发并注入运行时。不再生成或读取 `install.yaml`。具体用法见 [config-schema.md](config-schema.md)。

## 9. 托管资源（可选）

如果你的 MCP 服务需要 MySQL，不建议在服务自己的 Docker Compose 里再启动一份 MySQL。可以在 `manifest.yaml` 中声明托管资源，由 Keystone 在安装时为该服务分配独立 database/user，并把连接信息注入为环境变量：

```yaml
managed_resources:
  mysql:
    retain_on_uninstall: true   # 卸载时保留数据库，避免误删业务数据
    inject:                     # 平台据此把分配好的连接信息注入容器（真载荷，必填）
      host: DB_HOST
      port: DB_PORT
      database: DB_NAME
      user: DB_USER
      password: DB_PASSWORD
```

应用启动后读取 `DB_HOST`、`DB_PORT`、`DB_NAME`、`DB_USER`、`DB_PASSWORD` 即可连接。每个应用拿到独立 database/user（名称由 Keystone 统一分配，数据在 MySQL 层隔离）。

> 字段瘦身：已砍 `required: true`（声明该块即=需要）和 `database: auto`/`user: auto`（平台无条件自动命名，填了是噪音）——只声明 `inject` 映射 + `retain_on_uninstall` 即可。

## 10. 构建

```bash
ks build
```

这会：
1. 校验 `manifest.yaml` 格式和权限声明
2. 审计高风险权限并警告
3. 打包为 `dist/<app-id>-<version>.tar.gz`
4. 生成 SHA-256 校验和

## 11. 测试

```bash
ks test
```

运行两轮检查：

- **静态检查**：manifest 格式、权限合法性
- **运行时探测**：启动容器，检测 MCP 端点和健康检查是否正常响应

## 12. 发布

### 首次发布前

```bash
# 注册开发者账号
ks auth register

# 登录
ks auth login

# 创建 Publisher（发布者身份）
ks publisher create my-org "我的组织"
```

### 发布应用

```bash
ks publish --changelog "初始版本"
```

一键完成：构建 → 创建应用（如不存在）→ 上传版本 → 提交审核。

### 管理应用

```bash
ks app list              # 查看我的应用
ks app versions my-app   # 查看版本历史
```

## 13. 环境诊断

如果遇到问题，运行：

```bash
ks doctor
```

它会检查：Go / Python / Docker 是否安装、模板是否可用、是否已登录等。

## 常见问题

常见错误的"症状 → 根因 → 命令级修复"已系统化整理到 [troubleshooting.md](troubleshooting.md)：`ks dev` 启动失败、镜像拉取、端口占用、JWKS 校验失败、`ks register` 本地不生效、工具未被发现、Linux `host.docker.internal`、`ks test --skip-runtime`、自定义本地配置等。

## SDK 参考

- **Go SDK**：`go get github.com/wuhanyuhan/ks-devkit/sdk/go`，[详细文档](../sdk/go/README.md)
- **Python SDK**：`pip install ks-app-sdk`，[详细文档](../sdk/python/README.md)
- **TypeScript SDK**：`bun add @wuhanyuhan/ks-app`，[详细文档](../sdk/typescript/README.md)
- **CLI 命令参考**：[cli-reference.md](cli-reference.md)


## 在 GitHub Actions 用 PAT 自动发版

ks-devkit 提供 reusable workflow，让你的应用仓库 ~8 行 yaml 就能接入 CI 自动发版。

### 一次性设置（首次接入）

1. 在 ks-hub web UI（Publisher Settings → Tokens）创建一个 PAT，name 标注用途（如 `<publisher>-ci-publish`），scope 选 `publish:apps`，复制明文 token。
2. 在 GitHub 仓库或 organization 设置 `KS_HUB_TOKEN` secret，值为上一步 token。
3. 在仓库添加 `.github/workflows/publish.yml`：

```yaml
name: Publish
on:
  push:
    tags: ['v*']

jobs:
  call-publish:
    uses: wuhanyuhan/ks-devkit/.github/workflows/publish-callable.yml@v1
    with:
      ref: ${{ github.ref }}
    secrets:
      KS_HUB_TOKEN: ${{ secrets.KS_HUB_TOKEN }}
```

4. 推送 git tag（如 `git tag v0.1.0 && git push --tags`），workflow 自动跑：
   - checkout → 下载 ks 二进制 → ks publish
   - fast-track 路径阻塞最多 60s 等审核结果
   - manual 路径立即退出 0，用 `ks app status` 异步查进度

### 调用方常用配置

```yaml
jobs:
  call-publish:
    uses: wuhanyuhan/ks-devkit/.github/workflows/publish-callable.yml@v1
    with:
      ref: ${{ github.ref }}
      ks-version: 'v0.5.0'        # pin 具体版本，避免 latest 漂移
      wait-manual: '30m'           # manual 路径也阻塞等
    secrets:
      KS_HUB_TOKEN: ${{ secrets.KS_HUB_TOKEN }}
```

### 失败时通知

```yaml
  notify:
    needs: call-publish
    if: failure() && needs.call-publish.outputs.exit_code == '6'
    runs-on: ubuntu-latest
    steps:
      - run: |
          gh issue create \
            --title "Publish rejected: ${{ github.ref_name }}" \
            --body "Review path: ${{ needs.call-publish.outputs.review_path }}" \
            --label "publish-rejected"
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

### token 轮换 SOP

publisher 内多仓共用 PAT 时（推荐做法），轮换流程：

1. web UI 创建新 PAT（旧 token 不动）
2. 更新 GitHub org secret `KS_HUB_TOKEN`
3. 触发各仓库的 `workflow_dispatch` 重发或等下次 tag
4. web UI 撤销旧 PAT

### 排错

| exit code | 含义 | 行动 |
|---|---|---|
| 2 | token 无效 / publisher 错配 | 检查 secret + manifest.publisher |
| 3 | preflight 命中（含 secret 等） | 看 stderr 文件:行号 |
| 4 | 网络错 | retry |
| 5 | 版本号重复 | bump version |
| 6 | review rejected | 看 reason 修代码再发 |
