# 最佳实践 & 反模式

clean-break 已经帮你砍掉一堆不用填的字段。剩下要填的，照这几条填对。

## 1. 写一句真话，别堆砌
`description` 写清"这个能力做什么、何时用"一句真话即可。`aliases` / `user_utterances` / `use_cases` / `domain_terms` / `negative_examples` **不用手填**——由 LLM 从 description + schema 生成草稿、你审核。
- ❌ 反模式：硬凑 highlights/aliases 凑数（质量门禁逼出来的都是重复）。

## 2. 副作用诚实标
`side_effect_level`：`none`（只读）/ `soft_reversible`（可撤销写）/ `hard_irreversible`（不可逆）。`decision_mode` 默认从它派生，**别手填**，除非派生失效（如敏感读：side_effect=none 却要 user_only）。
- 危险/不可逆动作靠"人点确认"远比作者填白名单稳。

## 3. auth 别关
`auth.mode: keystone_jwks` 是 secure-by-default。`none` 只在你明确知道后果时用；本地裸跑用 `export KS_APP_AUTH_MODE=insecure`，**别把 `auth.mode` 改成 none 提交**。

## 4. 破坏性变更铸新名
能力契约是【名字即身份】。要破坏性改一个能力的输入/输出 → **铸新名**（`x.search` → `x.search_v2`），依赖方按名迁移。`requires` **永不加 version**。
- ❌ 反模式：就地改 `x.search` 的 schema 破坏已有调用方。

## 5. 派生字段别手填
`canonical`（=app_id.name，平台派生）、`presentation`（从 type 派生，squad→expert_team）、`decision_mode`（从 side_effect 派生）——这些留空，平台/SDK 算。provides 写【裸名】，只有 requires 写全名（不对称）。

## 6. 诚实理解 enforcement 状态（别以为填了就生效）
| 字段 | 现在 enforce 什么 | 还没接线的 |
|---|---|---|
| `permissions.network/filesystem` | 声明 + admin 安装期透明审查 | 运行时 egress/路径沙箱（规划中） |
| `version` | 格式校验 | 升级单调 + compat gate（已接） |
| `runtime.resources` | 格式/边界校验 | 拉起时应用（已接） |
| `protection` | — | 归平台：第三方 manifest 写了被忽略 |

填 `allowed_domains` **不等于**被沙箱拦截——当前只是声明 + 让 admin 安装时看见。

下一步：怎么选类型见 [decision-guide.md](decision-guide.md)。
