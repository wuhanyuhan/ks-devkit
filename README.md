# ks-devkit

Keystone 开放生态平台的开发者工具链，面向第三方应用开发者。

## 包含内容

| 目录 | 产物 | 说明 |
|------|------|------|
| `cmd/ks/`、`internal/` | `ks` 命令行工具 | 应用创建、构建、测试、迁移、注册、发布 |
| `sdk/go/` | Go SDK | 包名 `ksapp` |
| `sdk/python/` | Python SDK | 包名 `ks_app` |
| `sdk/typescript/` | TypeScript SDK | 包名 `@wuhanyuhan/ks-app` |
| `conformance/` | 协议一致性测试套件 | 三语言 wire-compat、config-schema 加密、auth |
| `internal/resources/templates/` | 项目模板 | `ks init` 使用的脚手架（8 个） |
| `internal/resources/runtime/` | 本地开发环境 | docker-compose（keystone-dev + MySQL + Redis） |

## 安装

### 从 GitHub Releases 下载

前往 [Releases](https://github.com/wuhanyuhan/ks-devkit/releases) 下载对应平台的二进制。

### 从源码构建

```bash
go build -o bin/ks ./cmd/ks
```

要求：Go 1.26+

## 快速上手

> 想 5 分钟端到端跑通（含 curl 实测）？见 [docs/quickstart.md](docs/quickstart.md)。

```bash
# 1. 注册并登录
ks auth register
ks auth login

# 2. 创建项目
ks init my-app --type=app --lang=go
cd my-app

# 3. 构建
ks build

# 4. 发布（自动创建应用 + 上传 + 提交审核）
ks publish --changelog "初始版本"
```

## CLI 命令

| 命令 | 说明 |
|------|------|
| `ks init <name>` | 创建新项目 |
| `ks build` | 构建打包 |
| `ks dev` | 启动本地开发环境 |
| `ks register` | 把本地 manifest 注册进本地 keystone（联调能力网格） |
| `ks refresh-meta` | 幂等重同步 manifest 到本地 keystone |
| `ks migrate [manifest.yaml]` | 旧 manifest 迁移到 clean-break 声明 |
| `ks schema --write` | 刷新 IDE 校验用 manifest schema |
| `ks publish` | 智能一键发布 |
| `ks test` | 静态校验 + 运行时探测 |
| `ks doctor` | 环境诊断 |
| `ks auth register` | 注册开发者账号 |
| `ks auth login` | 登录 |
| `ks auth logout` | 登出 |
| `ks auth whoami` | 查看当前身份 |
| `ks publisher create` | 创建 Publisher |
| `ks publisher list` | 查看 Publisher 列表 |
| `ks app create` | 创建应用 |
| `ks app list` | 查看应用列表 |
| `ks app versions` | 查看版本历史 |
| `ks app status` | 查看应用 / 版本状态 |
| `ks app submit` | 提交审核 |

## SDK

### Go

```bash
go get github.com/wuhanyuhan/ks-devkit/sdk/go@v0.13.0
```

```go
package main

import "github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp"

func main() {
    app := ksapp.New("my-app",
        ksapp.WithManifest("manifest.yaml"),
        ksapp.WithKeystoneAuth(),
        ksapp.WithVersion("0.1.0"),
    )

    // RegisterCapability 收裸名（与 manifest provides[].name 一致），canonical 由平台注册期派生。
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

### Python

```bash
pip install ks-app-sdk
```

```python
from ks_app import App, CapabilityContext

app = App(id="my-app", keystone_auth=True, version="0.1.0", manifest_path="manifest.yaml")

@app.capability("hello")
async def hello(ctx: CapabilityContext, args: dict) -> dict:
    name = args.get("name") or "world"
    return {"message": f"Hello, {name}!"}

if __name__ == "__main__":
    app.run()
```

### TypeScript

```bash
bun add @wuhanyuhan/ks-app zod @modelcontextprotocol/sdk
```

```typescript
import { createApp } from "@wuhanyuhan/ks-app";

const app = createApp({ id: "my-app" });
app.registerCapability("hello", async (ctx, args) => ({ message: `Hello, ${args.name ?? "world"}` }));
app.run();
```

## 文档

完整开发者文档见 **[docs/](docs/README.md)**——快速上手、架构、SDK API、鉴权与安全、类型化配置、版本兼容、迁移、故障排查。

## 开发

```bash
# Go 测试
go test ./...            # CLI（仓库根模块 github.com/wuhanyuhan/ks-devkit）
cd sdk/go && go test ./...

# Python 测试
cd sdk/python && pytest
```

贡献指南见 [CONTRIBUTING.md](CONTRIBUTING.md)；维护者约定见 [AGENTS.md](AGENTS.md)。

## License

MIT — 见 [LICENSE](LICENSE)。
