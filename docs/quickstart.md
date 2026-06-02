# Quickstart（5 分钟跑通第一个 app）

从零到本地调通一个 MCP 工具。下面每条命令都已实测。

## 前置

- Go 1.26+
- `ks` CLI（见 [安装](../README.md#安装)）

> 这条 happy path **不需要 docker / keystone-dev**——直接裸跑 app 自己的 MCP server 再 curl。能力网格联调（dispatcher / 跨 app）另见 [developer-guide.md](developer-guide.md)（本地镜像的能力网格限制见其第 6 节）。

## 1. 创建项目

```bash
ks init my-app --type=app --lang=go
cd my-app
go mod tidy
```

生成的 `main.go` 注册了一个 `hello` 能力，`manifest.yaml` 的 `provides` 已声明它（backend `mcp_tool`），SDK 启动时自动把它接成 MCP 工具。

## 2. 本地裸跑

脚手架默认 secure-by-default（`auth.mode: keystone_jwks`）。本地没有 keystone 签发的 JWT，用逃生阀降级运行：

```bash
KS_APP_AUTH_MODE=insecure go run .
```

启动日志：

```
INFO starting app id=my-app addr=:8080
```

## 3. 另开一个终端调用它

```bash
# 存活探针
curl -s http://localhost:8080/healthz
# → {"status":"ok"}

# 应用元信息（能看到 hello 工具）
curl -s http://localhost:8080/meta
# → {"name":"my-app","version":"0.1.0","auth_mode":"none",
#    "tools":[{"name":"hello","description":"capability my-app.hello"}],"protocol_version":"1.0"}

# 调用 hello 工具（MCP JSON-RPC 端点）
curl -s -X POST http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hello","arguments":{"name":"Keystone"}}}'
# → {"jsonrpc":"2.0","id":1,"result":{"content":[{"text":"{\"message\":\"Hello, Keystone!\"}","type":"text"}]}}
```

也有 legacy 简化端点：

```bash
curl -s -X POST http://localhost:8080/mcp/tools/call \
  -H 'Content-Type: application/json' \
  -d '{"name":"hello","params":{"name":"Keystone"}}'
# → {"result":{"message":"Hello, Keystone!"}}
```

## 下一步

- 给能力加类型化配置（API Key 等管理员填的值）：[config-schema.md](config-schema.md)
- 怎么选四类型 / tool vs capability：[decision-guide.md](decision-guide.md)
- 鉴权与安全模型：[auth-and-security.md](auth-and-security.md)
- 能力网格联调（dispatcher / 跨 app / long_running）：[developer-guide.md](developer-guide.md)（注意当前本地镜像 `0.3.0` 的能力网格限制）
- 发布上架：`ks publish`（见 [cli-reference.md](cli-reference.md)）
