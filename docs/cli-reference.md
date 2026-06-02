# ks CLI 命令参考

## 全局选项

| 选项 | 说明 |
|------|------|
| --config | 配置文件路径（默认 ~/.ks/config.yaml） |
| --hub-url | ks-hub 服务地址 |
| --version | 显示版本号 |

## 核心命令

### ks init <name>

创建新项目。

| 选项 | 默认值 | 说明 |
|------|--------|------|
| --type | app | 应用类型（app/squad/agent/skill），怎么选见 decision-guide.md |
| --lang | go | 编程语言（go/python/ts）；agent/skill 为 langless，忽略 --lang |
| --publisher | | Publisher slug |

> `ks init` 同时写出 `.ks/manifest.schema.json` + `.vscode/settings.json`，IDE 即得 manifest 校验/补全。

### ks build

在当前目录构建应用，输出 tarball 到 dist/。含 manifest 校验和权限校验。

### ks dev

启动本地开发环境（docker-compose up）。

### ks test

本地验证应用。

| 选项 | 默认值 | 说明 |
|------|--------|------|
| --skip-runtime | false | 跳过运行时探测 |
| --port | 18080 | 运行时探测端口 |
| --timeout | 30s | 超时时间 |

### ks register

读当前目录 manifest.yaml，登录本地 keystone-dev 并以 external_endpoint 模式 install——把 manifest 注册进本地 keystone（派生 canonical、注册能力、挂 mcp_server/反代），你的 app 容器自己跑。用于能力网格联调。

| 选项 | 默认值 | 说明 |
|------|--------|------|
| --keystone-url | http://localhost:9988 | 本地 keystone admin 地址 |
| --endpoint | http://host.docker.internal:8080/mcp | 你的 app MCP 端点（keystone 容器视角） |
| --user | admin | admin 用户名 |
| --password | admin | admin 密码 |

> ⚠️ 能力网格的本地注册需要 clean-break 版 keystone-dev 镜像；当前自带 `keystone-dev:0.3.0` 尚不支持，详见 [developer-guide.md](developer-guide.md) 第 6 节告警。

### ks refresh-meta

改 manifest/代码后幂等重同步到本地 keystone（先卸载再重装），免 `docker compose down -v`。flag 同 `ks register`。

### ks schema

打印内嵌的 manifest JSON Schema（由 ks-types 字段注释生成，喂 IDE 校验/补全）。

| 选项 | 默认值 | 说明 |
|------|--------|------|
| --write | false | 写入当前项目 .ks/manifest.schema.json |

### ks migrate [manifest.yaml]

把旧 manifest（`type:service/extension/assistant`、带前缀 canonical_name、cost_hint/input_nl 等废字段、旧 dependencies）机械迁移到 clean-break 声明形态。默认 **dry-run**：打印迁移后内容到 stdout，差异交 `git diff` 看。

| 选项 | 默认值 | 说明 |
|------|--------|------|
| --type | （自动判定） | 强制目标 type；**squad 仓必须传 `--type=squad`**（store.team 自动判定不可靠） |
| --write | false | 写回文件（默认只打印到 stdout） |

### ks doctor

检查开发环境是否就绪。逐项检查 Go / Python / Node / bun / Docker(client) / 模板目录 / 登录凭证，任一缺失给出修复提示。无 flag。

### ks publish

智能一键发布：构建 → 检测应用（不存在则自动创建）→ 上传版本 → 提交审核。

| flag | 说明 |
|------|------|
| `--changelog` | 版本变更说明 |
| `--no-wait` | fast-track 路径也立即 exit 0，不阻塞等审核结果 |
| `--wait-manual=<dur>` | manual 路径也阻塞等待，最长 30m（默认立即退） |
| `--dry-run` | 只跑 preflight + build，不上传 |
| `--allow-secrets` | 跳过 preflight secret 扫描（仅本地手动验证用，CI 严禁） |
| `--json` | 机器可读 NDJSON 输出（每行一个事件，最后一行含 `review_path` 字段） |

`--json` 输出示例（fast-track approved 路径）：

    {"event":"upload_done","app_id":"my-app","version":"v0.5.2"}
    {"event":"submit_done","review_id":42,"review_path":"fast-track","app_id":"...","version":"..."}
    {"event":"terminal","status":"approved","review_path":"fast-track","ksp_sha256":"...","ksp_size_bytes":1234}

CI 通过 `tail -1 publish.out | jq -r '.review_path'` 提取 review_path。

## 认证命令

### ks auth register

注册开发者账号。

| flag | 说明 |
|------|------|
| `--email` | 邮箱地址 |
| `--display-name` | 显示名称 |

### ks auth login

登录 Keystone Hub。亦可用顶层别名 `ks login`。默认打开浏览器完成授权（device flow），CLI 轮询获取 user JWT。

| flag | 说明 |
|------|------|
| `--email` | 邮箱地址 |
| `--password` | 用终端邮箱 + 密码登录（兼容旧流程） |
| `--token` | Personal Access Token（`ksh_pat_*`），CI 非交互登录用，与 `--email` 互斥 |

非交互注入 Personal Access Token（CI 场景）：

    ks auth login --token ksh_pat_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

调 hub `GET /v1/developer/auth/whoami` 验证 token，将 publisher_slug + scopes 写入 ~/.ks/credentials.json（auth_type=pat）。

### ks auth logout

登出。亦可用顶层别名 `ks logout`。

### ks auth whoami

查看当前登录身份。

### 环境变量 KS_HUB_TOKEN

设置后 `ks publish` / `ks app status` 等命令直接使用，无需先 `ks auth login`。仅支持 PAT 形态（`ksh_pat_*` 前缀）；user JWT 不支持 env 注入。

    export KS_HUB_TOKEN=ksh_pat_xxxxx
    ks publish      # 不需要 ~/.ks/credentials.json

## Publisher 管理

### ks publisher create

| 选项 | 说明 |
|------|------|
| --slug | Publisher 唯一标识（必需） |
| --name | 显示名称（必需） |

### ks publisher list

列出我的 Publisher。

## 应用管理

### ks app create

| 选项 | 说明 |
|------|------|
| --publisher | Publisher slug（必需） |
| --id | 应用 ID（必需） |
| --name | 应用名称（必需） |
| --type | 应用类型（app/squad/agent/skill，默认 app） |
| --summary | 简介 |

### ks app list

| 选项 | 说明 |
|------|------|
| --publisher | 按 publisher ID 过滤 |

### ks app versions <app_id>

查看版本列表。

### ks app submit <app_id> <version>

提交版本审核。

### ks app status <slug>[@<version>]

查询应用版本状态：

    ks app status my-app                  # 列表（最新 10 个）
    ks app status my-app@v0.5.2           # 单版本详情
    ks app status my-app --json           # JSON 输出

## 退出码

| Exit | 类别 |
|---|---|
| 0 | 成功 |
| 1 | 通用失败（panic / 未分类业务异常） |
| 2 | 认证 / 权限错误（token 无效 / scope 缺失 / publisher 错配） |
| 3 | 客户端配置错误（manifest / preflight / build 失败） |
| 4 | 网络 / 上传错误（建议 retry） |
| 5 | 重复版本（VersionAlreadyExists） |
| 6 | review rejected（fast-track 路径下被拒） |
