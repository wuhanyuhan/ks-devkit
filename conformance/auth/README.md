# MCP Service Auth Conformance

跨语言契约测试套件。运行它，证明你的 MCP service 实现遵守
`service-auth-convention.md` 定义的 wire-level 行为。

**Version:** 见 `VERSION`

**契约文字权威:** 见 `SPEC.md`

---

## 如何让你的实现通过

### 1. 实现要求

- 参考 `SPEC.md` 各节
- 用任何语言、任何框架实现即可

### 2. 提供一个 claimant

在你的 SDK 仓内创建 `conformance-claimant/` 子目录，含一个最小可启动的
service：

- 注册一个名为 `echo` 的 MCP 工具
- 接受 `{"message": string}` 参数，返回 `{"echoed": message}`
- `auth_mode` 设为 `keystone_jwks`
- 版本号设为 `conformance-v1.0.0`

参考实现：
- `sdk/go/conformance-claimant/`（Go，ksapp SDK）
- `sdk/python/conformance-claimant/`（Python，ks_app SDK）
- `sdk/typescript/conformance-claimant/`（TypeScript，ks-app SDK）
- `ks-squad-framework/conformance-claimant/`（Go，squad framework）

### 3. 跑 conformance

```bash
cd conformance/auth
./orchestrate.sh \
  --claimant-cmd="cd /path/to/your/claimant && <start command>" \
  --claimant-port=<your port>
```

全绿即合规。

### 4. 在 SDK README 里声明

```markdown
Claims compliance with: conformance-v1.0.0
```

---

## 目录索引

| 文件/目录 | 职责 |
|----------|------|
| `SPEC.md` | 契约文字权威 |
| `VERSION` | conformance 自己的版本号 |
| `lib.sh` | bash 辅助函数 |
| `cases/` | 测试用例（每个一个独立 .sh） |
| `mock-jwks/` | 本地 JWKS server + 运行时测试 RSA 密钥生成器 |
| `run.sh` | 跑所有 cases |
| `orchestrate.sh` | 起 mock-jwks + claimant + run.sh |

---

## 本地开发

```bash
# 起 mock-jwks + claimant + 跑 cases
./orchestrate.sh --claimant-cmd="..." --claimant-port=8080

# orchestrate.sh 会在临时目录生成测试 RSA key pair 和 jwks.json，
# 运行结束后自动清理；仓库不跟踪任何 PEM 私钥 fixture。

# 只跑某些 case（调试）
./orchestrate.sh --claimant-cmd="..." --claimant-port=8080
# 修改 orchestrate.sh 内部的 run.sh 调用加 --only=04,05

# 失败时保留 30s 让你 curl 调试
./orchestrate.sh ... --keep-alive-on-fail
```

---

## 治理

PR 新增 case 必须满足：

1. SPEC.md 相应条款先更新（契约先于代码）
2. 四个现有 claimant（Go ksapp / Python ks_app / TS ks-app / squad framework）都通过
3. 详见 `Keystone conformance governance docs`
