# Squad Tool UI Quickstart

> 在 squad 上接入 widgets-protocol-v1，让 LLM 调 tool 的结果以 widget 形式直接渲染在对话流气泡里。

**适用读者**：要给 squad 加 widget UI 的开发者。本文按"用什么 SDK → 几行代码 → 跑通"组织，覆盖 ksapp Go SDK（`github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp`）、ks-app Python SDK（PyPI `ks-app-sdk`，导入名 `ks_app`）两条公开路径，以及 ks-squad-framework（⚠️ 该框架尚未开源，外部开发者请用前两条公开路径；§1.3 仅供参考）。协议层契约（URI 形态、字段含义、版本规则、错误码）见 [`ks-types/docs/widgets-protocol-v1.md`](https://github.com/wuhanyuhan/ks-types/blob/master/docs/widgets-protocol-v1.md)；本文只讲 squad 端 SDK 调用。

**目录**：[§1 三种 SDK 完整示例](#1-三种-sdk-完整示例review_draft) · [§2 5 个共享 widget 用法速查](#2-5-个共享-widget-用法速查) · [§3 Escape hatch：自定义 widget](#3-escape-hatch自定义-widget) · [§4 测试 squad widget](#4-测试-squad-widget) · [§5 常见错误排查](#5-常见错误排查)

---

## §1. 三种 SDK 完整示例（review_draft）

三种 SDK 的语义完全等价：声明 widget binding（manifest 阶段）+ 在 tool handler 里返回 widget data（runtime 阶段）。生产代码任选一种。

### 1.1 Go ksapp（适合纯 MCP service 形态）

```go
import (
    "context"
    "github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp"
    kstypes "github.com/wuhanyuhan/ks-types"
)

func main() {
    app := ksapp.New("squad-marketing")
    app.RegisterTool(ksapp.NewTool("review_draft").
        WithDescription("审阅指定 draft").
        WithInputSchema(map[string]any{
            "type":       "object",
            "properties": map[string]any{"draft_id": map[string]any{"type": "integer"}},
            "required":   []string{"draft_id"},
        }).
        WithToolUI(kstypes.ToolUIBinding{Widget: "ks://widgets/diff-review@v1"}).
        WithHandler(reviewDraftHandler))
    app.Run()
}

func reviewDraftHandler(ctx context.Context, params map[string]any) (any, error) {
    draftID := int(params["draft_id"].(float64))
    diff, annotations := computeDiff(ctx, draftID) // 业务略
    return ksapp.NewToolResult().
        WithText("已审阅 draft，有 3 处建议改动").
        WithUIData(kstypes.WidgetDiffReviewV1{
            Title:       "5月营销月报 — 修改建议",
            Diff:        diff,
            Annotations: annotations,
            Actions: []kstypes.WidgetActionDescriptor{
                {ID: "approve", Label: "批准", Variant: "primary"},
                {ID: "request_changes", Label: "要求修改"},
                {ID: "reject", Label: "拒绝", Variant: "destructive"},
            },
        }), nil
}
```

关键 API：`ksapp.NewTool(name)` 返链式 `*ToolBuilder`；`WithToolUI` 注入 manifest binding 并自动让 `capabilities.ui.enabled = true`；`NewToolResult().WithText(...).WithUIData(data)` 序列化时自动调 `data.Validate()`，失败 MarshalJSON 返 error。

### 1.2 Python ks_app（适合异步 / FastAPI 风格的 Python squad）

```python
from ks_app import App
from ks_app.tool_ui import ToolResult, ToolUIBinding
from ks_app.tool_ui_widgets import WidgetActionDescriptor, WidgetDiffReviewV1

app = App("squad-marketing")


@app.tool(
    "review_draft",
    "审阅指定 draft",
    input_schema={"type": "object", "properties": {"draft_id": {"type": "integer"}}, "required": ["draft_id"]},
    ui_binding=ToolUIBinding(widget="ks://widgets/diff-review@v1"),
)
async def review_draft(draft_id: int):
    diff, annotations = await compute_diff(draft_id)  # 业务略
    return (
        ToolResult()
        .with_text("已审阅 draft，有 3 处建议改动")
        .with_ui_data(WidgetDiffReviewV1(
            title="5月营销月报 — 修改建议",
            diff=diff, annotations=annotations,
            actions=[
                WidgetActionDescriptor(id="approve", label="批准", variant="primary"),
                WidgetActionDescriptor(id="request_changes", label="要求修改"),
                WidgetActionDescriptor(id="reject", label="拒绝", variant="destructive"),
            ],
        ))
        .to_json()
    )
```

关键 API：`@app.tool(name, description, input_schema=..., ui_binding=ToolUIBinding(widget=...))` — `ui_binding` 必须**关键字参数**传（位置参数会撞 `input_schema`）；handler 必须 `async def`（同步会 `TypeError`）；`ToolResult().with_text(...).with_ui_data(data).to_json()` 返回 JSON 字符串，`with_ui_data` 接受带 `validate()` / `to_dict()` 的对象或裸 `dict`。

### 1.3 ks-squad-framework（适合复杂 squad，含 pipelines/skills/roles）

> ⚠️ **ks-squad-framework 目前尚未开源（仅内部）。** 外部开发者请用 §1.1 ksapp（Go）或 §1.2 ks_app（Python）——两者语义等价。以下示例仅供未来参考。

```go
import (
    "context"
    "github.com/mark3labs/mcp-go/mcp"
    mcpserver "github.com/mark3labs/mcp-go/server"
    kstypes "github.com/wuhanyuhan/ks-types"
    "github.com/wuhanyuhan/ks-squad-framework/bootstrap"
)

func main() {
    app := bootstrap.NewApp(bootstrap.AppConfig{Name: "squad-marketing", Version: "0.6.0", Port: 9100})
    app.OnSetup(func(squadCtx *bootstrap.Context) {
        squadCtx.RegisterToolUIBinding("review_draft", kstypes.ToolUIBinding{
            Widget: "ks://widgets/diff-review@v1",
        })
        squadCtx.AddTool(
            mcp.NewTool("review_draft",
                mcp.WithDescription("审阅指定 draft"),
                mcp.WithNumber("draft_id", mcp.Required(), mcp.Description("草稿 ID"))),
            reviewDraftHandler(squadCtx),
        )
    })
    if err := app.Run(); err != nil {
        panic(err)
    }
}

func reviewDraftHandler(squadCtx *bootstrap.Context) mcpserver.ToolHandlerFunc {
    return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        // squadCtx.DB / PipelineEngine / KeystoneClient 均可用
        data := kstypes.WidgetDiffReviewV1{
            Title: "5月营销月报 — 修改建议", Diff: buildDiff(),
            Actions: []kstypes.WidgetActionDescriptor{
                {ID: "approve", Label: "批准", Variant: "primary"},
                {ID: "reject", Label: "拒绝", Variant: "destructive"},
            },
        }
        if err := data.Validate(); err != nil {
            return nil, err
        }
        result := mcp.NewToolResultText("已审阅 draft，有 3 处建议改动")
        result.Meta = &mcp.Meta{AdditionalFields: map[string]any{
            "ui":       map[string]any{"widget": "ks://widgets/diff-review@v1"},
            "keystone": map[string]any{"ui": map[string]any{"data": data}},
        }}
        return result, nil
    }
}
```

关键 API：`bootstrap.NewApp(...)` + `app.OnSetup(func(*bootstrap.Context))` 注册回调。`squadCtx.RegisterToolUIBinding(name, binding)` 写 manifest binding（同名以最后一次为准）；`squadCtx.AddTool(mcp.Tool, ToolHandlerFunc)` 注册 mcp-go 风格 tool；另有 `GetToolUIBinding` / `AllToolUIBindings` 读取。OnSetup 后由 bootstrap.Run 自动注入 `/meta.tools[]._meta.ui` 并设 `capabilities.ui.enabled = true`（见 `bootstrap/app_meta_with_ui_test.go`）。framework v0.8.0 暂未提供 ToolResult 包装——构造 `_meta.keystone.ui.data` 走 mcp-go `mcp.Meta.AdditionalFields`，`data.Validate()` 自行调（建议抽成 squad 内 helper）。

---

## §2. 5 个共享 widget 用法速查

每个 widget 给一个 squad 真实业务示例：从业务数据 → widget data 的映射。完整字段表见 `ks-types/docs/widgets-protocol-v1.md` §3。

### 2.1 list-actions — 草稿列表（marketing `list_drafts`）

```go
return ksapp.NewToolResult().
    WithText("待审 4 篇，已审 12 篇").
    WithUIData(kstypes.WidgetListActionsV1{
        Title: "待审阅草稿",
        Items: []kstypes.WidgetListItem{
            {ID: "42", Title: "5月营销月报", Subtitle: "2 小时前", Icon: "file-text",
             Badges: []kstypes.WidgetBadge{{Label: "待审", Variant: "warning"}},
             RowActions: []kstypes.WidgetActionDescriptor{
                 {ID: "review", Label: "审阅", Variant: "primary"},
                 {ID: "discard", Label: "丢弃", Variant: "destructive"}}},
            {ID: "43", Title: "新品发布稿", Subtitle: "1 天前", Icon: "file-text"},
        },
        Empty: &kstypes.WidgetEmptyState{Icon: "inbox", Title: "暂无草稿"},
    }), nil
```

### 2.2 diff-review — PR diff 审阅（dev squad `review_pr`）

```python
return (
    ToolResult()
    .with_text(f"PR #{pr_id} 已审阅，3 处建议")
    .with_ui_data(WidgetDiffReviewV1(
        title=f"PR #{pr_id} — refactor llm router",
        subtitle="LLM reviewer @ 2026-05-04",
        diff=[
            WidgetDiffSegment(type="context", text="func Route(ctx context.Context, req *Request) {"),
            WidgetDiffSegment(type="delete",  text="    log.Println(req)"),
            WidgetDiffSegment(type="insert",  text='    slog.Info("route", "scene", req.Scene)'),
        ],
        annotations=[
            WidgetDiffAnnotation(anchor_index=2, severity="info",
                                  message="建议加 trace_id 字段")],
        actions=[
            WidgetActionDescriptor(id="approve", label="批准合并", variant="primary"),
            WidgetActionDescriptor(id="request_changes", label="要求修改"),
        ],
    ))
    .to_json()
)
```

### 2.3 timeline — pipeline 状态（dev squad `get_ci_status`）

```go
return ksapp.NewToolResult().
    WithText("流水线 #1234：build/test 已成功，deploy 进行中").
    WithUIData(kstypes.WidgetTimelineV1{
        Title: "CI 流水线 — feature/widget-poc",
        Events: []kstypes.WidgetTimelineNode{
            {ID: "build", Time: "2026-05-04T08:00:00Z", Title: "build", Status: "success", Subtitle: "12s"},
            {ID: "test",  Time: "2026-05-04T08:00:12Z", Title: "test",  Status: "success", Subtitle: "1m24s"},
            {ID: "deploy", Time: "2026-05-04T08:01:36Z", Title: "deploy", Status: "running",
             Actions: []kstypes.WidgetActionDescriptor{{ID: "cancel", Label: "取消", Variant: "destructive"}}},
        },
    }), nil
```

注意：`time` 必须 RFC3339（结尾 `Z` 或 `±HH:MM`）。Python 推荐 `datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")`。

### 2.4 card-grid — 知识检索结果（knowledge squad `search_kb`）

```python
from ks_app.tool_ui_widgets import WidgetBadge, WidgetCard, WidgetCardGridV1

return (
    ToolResult()
    .with_text(f'检索 "{query}" 得 4 条结果')
    .with_ui_data(WidgetCardGridV1(
        title="检索结果（top 4）", columns=2,
        cards=[
            WidgetCard(id="kb-1", title="新员工入职流程", excerpt="onboarding checklist...",
                       image_url="https://cdn.example.com/onboarding.png",  # 必须 https
                       source_label="HR 知识库",
                       source_url="https://kb.example.com/onboarding",
                       score=0.92, badges=[WidgetBadge(label="官方")]),
            WidgetCard(id="kb-2", title="工卡申请流程",
                       source_url="https://kb.example.com/badge", score=0.78),
        ],
    ))
    .to_json()
)
```

注意：`image_url` 严格 `https://`（防 inline XSS）；`source_url` 允许 http/https；`score` 必须 `[0,1]`。

### 2.5 image-variants — 图片生成（marketing `generate_brand_assets`）

```go
return ksapp.NewToolResult().
    WithText("生成了 3 张 logo 候选").
    WithUIData(kstypes.WidgetImageVariantsV1{
        Title: "Logo 候选",
        Primary: kstypes.WidgetImageItem{
            ID: "v1", URL: "https://cdn.example.com/logo-v1.png",
            AltText: "蓝色简约 logo，方形", Width: 1024, Height: 1024,
        },
        Variants: []kstypes.WidgetImageItem{
            {ID: "v2", URL: "https://cdn.example.com/logo-v2.png", AltText: "蓝色简约 logo，圆形"},
            {ID: "v3", URL: "https://cdn.example.com/logo-v3.png", AltText: "深紫渐变 logo"},
        },
        Actions: []kstypes.WidgetActionDescriptor{
            {ID: "regenerate", Label: "再生成"},
            {ID: "save", Label: "保存", Variant: "primary"},
        },
    }), nil
```

注意：`url` 严格 `https://`；`alt_text` 必填（无障碍）。

---

## §3. Escape hatch：自定义 widget

5 个共享 widget 覆盖 80% 用例；剩 20%（富编辑器、专用可视化、既有 SPA 嵌段）走 iframe escape hatch。**优先看共享 widget 能否表达；不行再 RFC 加新共享 widget；都不行再走 escape hatch**——单 squad 自定义 widget 长期看是债。

### 3.1 如何 serve

squad 自己挂 HTTP 静态资源到 `/<path>/*`（ksapp 的 `app.HandleFunc(...)` 等），然后声明：

```go
squadCtx.RegisterToolUIBinding("get_brand_manual", kstypes.ToolUIBinding{
    Widget:       "ui://marketing/brand-editor",   // ui:// 形态
    SandboxHints: []string{"allow-downloads"},     // per-call sandbox 申请
})
```

**说明**：`sandbox_hints` 是 per-tool 字段（列在 `ToolUIBinding` 上），平台 mount 时按全局白名单（`mcp.tool_ui.sandbox_global_whitelist`，默认仅 `allow-downloads`）过滤，超出项被静默丢弃。基础集 `allow-scripts allow-forms` 不可关。`CapabilitiesUI.RequestedSandbox` 是 squad-level manifest 字段，ksapp / ks_app 两个 SDK 当前不直接暴露——squad 加 sandbox 走 `sandbox_hints` 即可；framework 用户可通过 `meta.Info.SetCapabilities(...)` 手动声明。

### 3.2 三层防御

iframe 路径强制三层防御，**对 squad 透明**——你按下面写代码，keystone proxy + 浏览器 sandbox 兜底：

1. **iframe sandbox 属性**（浏览器层）：基础集 `allow-scripts allow-forms`；额外 flag 经 `sandbox_hints` 申请。iframe 默认 null origin——不能访问 cookie / localStorage / fetch，所有状态走 postMessage
2. **CSP 反代注入**（HTTP 协议层）：keystone 反代强制注入 `connect-src 'none'`（禁 fetch/WebSocket）+ `frame-ancestors {keystone-origin}`（防被偷嵌）。`<script src>` 必须同源，外部 CDN 走 inline
3. **postMessage 协议**（应用层）：iframe 内 JS 通过 `parent.postMessage({jsonrpc:"2.0", method:"app.ready"}, "*")` 通信。方法名常量见 `kstypes.PMMethodAppReady` 等（`widget_postmessage.go`）

iframe 内 SDK 见本仓 `sdk/typescript/squad-widget-sdk/`（⚠️ 暂未发布到 npm，先按源码引入）；也可用裸 `postMessage`（方法名常量见上）。

---

## §4. 测试 squad widget

### 4.1 单元测试 — widget data validate

5 个 widget 类型自带 `Validate() error`（Go）/ `validate()`（Python），单测覆盖 happy path + 业务规则越界即可：

```go
// Go ksapp — 越界 case
func TestReviewDraft_AnnotationOutOfRange(t *testing.T) {
    data := kstypes.WidgetDiffReviewV1{
        Title:   "x",
        Diff:    []kstypes.WidgetDiffSegment{{Type: "context", Text: "a"}},
        Annotations: []kstypes.WidgetDiffAnnotation{{AnchorIndex: 5, Severity: "info", Message: "x"}},
        Actions: []kstypes.WidgetActionDescriptor{{ID: "ok", Label: "ok"}},
    }
    require.ErrorContains(t, data.Validate(), "anchor_index")
}
```

```python
# Python ks_app
def test_review_draft_invalid_segment_type():
    data = WidgetDiffReviewV1(
        title="x",
        diff=[WidgetDiffSegment(type="bogus", text="a")],
        actions=[WidgetActionDescriptor(id="ok", label="ok")],
    )
    with pytest.raises(ValueError, match="invalid segment type"):
        data.validate()
```

### 4.2 集成测试 — `/meta` 字段注入

ksapp / ks_app 两个 SDK 都自带"绑定后 `/meta` 该有 `_meta.ui` + `capabilities.ui.enabled`"模板：`sdk/go/ksapp/app_register_tool_test.go`、`sdk/python/tests/test_meta_with_ui.py`。

### 4.3 联调技巧

```bash
# 本地起 squad 后 curl /meta
curl -s http://localhost:9100/meta | jq '.capabilities, .tools[]._meta'
```

mock tool call 看 `_meta.keystone.ui.data` 序列化结果：在 handler 里打印 ToolResult JSON。端到端联调走 squad → keystone 平台 → keystone proxy 校验 → 前端 chat 渲染。

---

## §5. 常见错误排查

### 5.1 widget URI 格式错

| 症状 | 修法 |
|------|------|
| `invalid ks widget URI: "ks://widgets/diff-review"` | 缺 `@v1`，改为 `ks://widgets/diff-review@v1` |
| `unsupported widget URI scheme: "..."` | 共享 widget 用 `ks://`；自定义 widget 用 `ui://` |
| `invalid ks widget URI: "ks://widgets/Diff-Review@v1"` | name 必须全小写 + 连字符 |
| `widget URI must not contain query/fragment` | 动态参数走 widget data，不走 URI |

### 5.2 schema validation 失败

`UIResource.Error.Code = schema_mismatch`，`Message` 里有具体字段。常见错误：`items[0].title is required`（list-actions item 缺 title）；`diff requires at least 1 segment`（diff-review 传空 diff）；`annotations[0].anchor_index 5 out of range [0,3)`（annotation 越界）；`cards[0].image_url: must be https URL, got "http://..."`；`events[0]: invalid status "done"`（timeline status 不在 5 个枚举）；`primary.alt_text required`（image-variants 缺 alt_text，a11y 强制）。**排查顺序**：先在 squad 内调 `data.Validate()` / `data.validate()` 跑通，再走 keystone proxy。

### 5.3 HTTPS 校验失败

5 个 widget 里所有 `<img src>` inline 渲染的 URL（`WidgetCard.image_url` / `WidgetImageItem.url`）严格 `https://`，拒绝 `http`/`data`/`javascript`。`WidgetCard.source_url`（用户点击跳转）允许 `http://` 与 `https://`，因为不会 inline 渲染。

### 5.4 跨 squad widget 引用被拒

Mount 阶段日志 `cross-squad widget rejected, manifest reference: ui://squadB/x in squad A`，或 runtime 阶段 `UIResource.Error.Code = cross_squad_widget`，都说明 squad A 引用了 squad B 的自定义 widget。修法：自定义 widget 只能引用 `ui://{自己的squad-id}/...`；共享 widget（`ks://widgets/...`）不受此限。

### 5.5 Python RFC3339 naive datetime 拒绝

`ks_app` 严格按 Go `time.Parse(time.RFC3339, ...)` 校验：必须以 `Z` 或 `±HH:MM` 结尾、必须 `T` 分隔（拒绝空格）、拒绝 naive datetime。

```python
# 错：naive（无 timezone）
WidgetTimelineNode(time="2026-05-04T08:00:00", ...)        # ValueError
# 错：空格分隔（Python 3.11+ fromisoformat 接受，但 ks_app 拒绝）
WidgetTimelineNode(time="2026-05-04 08:00:00Z", ...)       # ValueError
# 对
WidgetTimelineNode(time="2026-05-04T08:00:00Z", ...)
WidgetTimelineNode(time="2026-05-04T16:00:00+08:00", ...)
# 推荐写法
t = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
```

校验细节：`ks_app/tool_ui_widgets.py:_validate_rfc3339`。

### 5.6 capability 未启用（widget 不渲染）

排查顺序：
1. `curl /meta` 看 `capabilities.ui.enabled = true` + `tools[].name=X` 有 `_meta.ui.widget`——三种 SDK 都在你声明 `WithToolUI` / `ui_binding` / `RegisterToolUIBinding` 后自动设 enabled，不必手动调
2. keystone 全局 feature flag：`mcp.tool_ui.enabled` 是否 true（运维侧）

---

## 参考资料

- 协议层契约：[`ks-types/docs/widgets-protocol-v1.md`](https://github.com/wuhanyuhan/ks-types/blob/master/docs/widgets-protocol-v1.md)；设计说明见公开 widget 协议文档
- SDK 源码：Go ksapp `sdk/go/ksapp/tool_builder.go` / `tool_result.go`；Python `sdk/python/src/ks_app/tool_ui.py` / `tool_ui_widgets.py`；framework `bootstrap/context.go`
- 测试模板：`sdk/go/ksapp/app_register_tool_test.go`、`sdk/python/tests/test_meta_with_ui.py`
