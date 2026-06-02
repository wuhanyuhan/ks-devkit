# 怎么选：app / squad / agent / skill + tool vs capability

第一个问题答不上来就起不了步。这页帮你 30 秒定位。

## 一、四种应用类型怎么选

| 你的情况 | 选 | 运行位置 |
|---|---|---|
| 纯工具集，无 LLM 负责人编排，只对外提供一批可调用能力 | **app** | 外部进程/容器 |
| 有 LLM 负责人编排多个专家，以一个"前门"对外供给 | **squad** | 外部进程/容器 |
| 平台内角色化助手，自身不跑 server、只调别人的能力 | **agent** | keystone 内（runtime none）|
| 单一可被语义召回的技能/方法资源 | **skill** | keystone 内（挂载）|

决策树：
- 要对外【供给能力】吗？
  - 否 → 平台内只【消费】能力做对话 → **agent**；只是一段可召回的方法 → **skill**。
  - 是 → 有 LLM 负责人编排一个团队吗？ 有 → **squad**；没有、就是一批工具 → **app**。

> app 合并了旧 `service` + `extension`（两者安装路径相同，区分是伪命题）；`agent` 是旧 `assistant` 改名。

脚手架：`ks init <name> --type=app|squad --lang=go|python|ts`（agent/skill 无语言，省略 `--lang`）。

## 二、tool vs capability

- **tool**（`app.Tool` / `@app.tool`）：裸 MCP tool，编排官 tools/list 能看到、能调。
- **capability**（`RegisterCapability` / `@app.capability`）：进【能力网格】，有 canonical 名、可被 dispatcher 路由、支持 on-behalf-of / chain / long_running task。

什么时候只用 tool、什么时候升级成 capability？

| 需求 | tool 够 | 要 capability |
|---|---|---|
| 仅供本 app 的 agent 直接调 | ✅ | |
| 要被【别的 app/agent】跨应用调用、纳入网格路由 | | ✅ |
| 要 long_running task / 进度上报 / 续跑 | | ✅ |
| 要按 side_effect/decision_mode 走审批闸 | | ✅ |

## 三、复用四象限（mcp_tool backend）

声明了 `backend.kind: mcp_tool` 的 capability，SDK 在 finalize 时按"有无独立 handler × 是否命中已有同名 `app.Tool`"四象限裁决：

| 有独立 handler | 命中已有 tool | 行为 |
|---|---|---|
| 是 | 是 | **报错**（真冲突）|
| 是 | 否 | 生成新 MCP tool（带 input_schema）|
| 否 | 是 | **复用**（join，不新增）|
| 否 | 否 | **报错（orphan→error）**——声明了却无承载 |

> orphan→error 是 clean-break 的 BREAKING：声明 capability 却既无 handler 也无同名 tool，启动期直接报错（替代旧的 warn）。

下一步：字段怎么填见 [best-practices.md](best-practices.md)；IDE 校验/补全见 [developer-guide.md](developer-guide.md#ide)。
