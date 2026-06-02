# Troubleshooting

按"症状 → 根因 → 命令级修复"组织。

## `ks dev` 启动失败 / 镜像拉取卡住

**症状**：`ks dev` 挂起或报 docker 错误；拉 `ghcr.io/wuhanyuhan/keystone-dev` 失败。

**根因**：Docker daemon 没起；或镜像拉取需网络 / ghcr 认证。

**修复**：
```bash
docker info                      # 确认 daemon 在跑
docker login ghcr.io             # 私有镜像需登录
docker compose -f ~/.ks/runtime/docker-compose.yaml pull   # 手动拉取看真实错误
```

## 端口被占用

**症状**：启动报 `address already in use`（app 8080；keystone 9988；MySQL 3306；Redis 6379）。

**根因**：端口被别的进程占了。

**修复**：
```bash
lsof -i :8080                    # 找占用进程
kill <pid>
KS_APP_PORT=18080 go run .       # 或改 app 监听端口（默认 8080，非法值回退 8080）
```

## 启动 panic：`KEYSTONE_JWKS_URL 未配置`

**症状**：app 启动直接 panic，提示 "auth_mode=keystone_jwks 但 KEYSTONE_JWKS_URL 未配置"。

**根因**：secure-by-default 下 effective `auth.mode` 是 `keystone_jwks`，但本地没有 keystone 签发的 JWT，也没设 JWKS 端点。

**修复**：本地裸跑用逃生阀；生产确认平台注入了 `KEYSTONE_JWKS_URL`。
```bash
KS_APP_AUTH_MODE=insecure go run .    # 本地降级为 none
```
详见 [auth-and-security.md](auth-and-security.md)。

## 工具没被发现 / `/meta` 看不到你的工具

**症状**：`curl localhost:8080/meta` 或 `/mcp/tools/list` 里没有你的工具；或启动报 "既无已注册 @app.tool 也无 RegisterCapability handler"。

**根因**：
- manifest `provides.capabilities[].name`（裸名）与代码 `RegisterCapability("...")` 不一致；
- 声明了 capability 却既无 handler 也无同名 `app.Tool`（orphan→error，启动期报错）。

**修复**：让 manifest 裸名与 `RegisterCapability` 裸名严格一致；删掉无承载的 capability 声明。见 [decision-guide.md](decision-guide.md) 复用四象限。

## `ks register` 成功但能力网格调不通（本地）

**症状**：`ks register` / `ks refresh-meta` 返回成功，但经 dispatcher 调能力不生效。

**根因**：`ks dev` 自带的 `keystone-dev:0.3.0` 是 pre-clean-break 镜像，不带能力网格注册语义。

**修复**：本地暂走裸 MCP tool 路径（直接 curl app 的 `/mcp`，见 [quickstart.md](quickstart.md)）；能力网格联调待 clean-break 镜像发布。详见 [developer-guide.md](developer-guide.md) 第 6 节。

## 工具没有被调用（联调）

**症状**：能力网格起来了但你的工具没被触发。

**修复**：逐项排查
```bash
curl http://localhost:8080/healthz                       # 1. app 在跑？
curl http://localhost:8080/mcp/tools/list                # 2. 工具已注册？
docker compose -f ~/.ks/runtime/docker-compose.yaml logs -f keystone-dev   # 3. keystone 日志
```

## Linux 上 Keystone 容器连不到你的 app

**症状**：容器内 keystone 连不上宿主机上跑的 app（`ks register` 的 endpoint 不通）。

**根因**：Linux 上 Docker 容器靠 `host.docker.internal` 访问宿主机，默认不解析。

**修复**：`ks dev` 释放的 `docker-compose.yaml` 已含 `extra_hosts: "host.docker.internal:host-gateway"`；自定义 compose 时记得加。

## `ks test` 卡在运行时探测 / 没有 docker

**症状**：`ks test` 在运行时探测阶段卡住或失败（CI / 无 docker 环境）。

**根因**：运行时探测要起 app 实例。

**修复**：只要静态校验（manifest + 权限）时跳过探测：
```bash
ks test --skip-runtime
```

## 自定义本地 Agent / MCP Server 配置

本地 seed 为快速调试设计。自定义走 Keystone Admin API：
```bash
TOKEN=$(curl -s -X POST http://localhost:9988/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin"}' | jq -r .data.access_token)
curl -H "Authorization: Bearer $TOKEN" http://localhost:9988/v1/admin/agents/list
```
