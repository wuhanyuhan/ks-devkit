# managed-resources Conformance 套件

验证应用对 keystone `/v1/apps/self/resources` 端点客户端实现的协议兼容性。

适用于：
- `ks-devkit/sdk/go/ksapp/keystoneclient/SelfClient`
- `ks-squad-framework/core/managed/SelfClient`
- 任何未来想接入 keystone 托管资源的 SDK / 框架

## 套件范围

**验证协议侧行为**：
- HTTP 调用格式（method / path / header / 鉴权）
- env 注入语义（setdefault：已存在不覆盖）
- 错误响应处理（4xx / 5xx / 超时 / 非 JSON 响应）

**不验证实现侧策略**（claimant 自行决定）：
- 失败时是 panic / 返回 error / 仅 warn 不阻塞
- 是否做重试
- 超时具体值

## 当前 spec 版本

- `spec/v1/managed-resources-conformance.md`

## 已通过 claimants

- ks-devkit/sdk/go/ksapp（参考实现，net/http）
- ks-squad-framework（gin 栈，独立实现）
