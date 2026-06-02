# Homebrew Tap 发布配置指南

本文档说明如何配置 Homebrew Tap，使用户可以通过 `brew install` 安装 `ks` CLI。

## 前置条件

- 拥有 GitHub 账号 `wuhanyuhan`
- `ks-devkit` 仓库已推送到 GitHub

## 操作步骤

### 第一步：创建 homebrew-tap 仓库

1. 打开 https://github.com/new
2. 仓库名填 `homebrew-tap`（必须以 `homebrew-` 开头，Homebrew 约定）
3. Owner 选 `wuhanyuhan`
4. 设为 **Public**（Homebrew tap 必须公开）
5. **不要**勾选 "Add a README file"，保持空仓库即可
6. 点击 Create repository

### 第二步：创建 Personal Access Token (PAT)

GoReleaser 发版时需要往 `homebrew-tap` 仓库推送 formula 文件，默认的 `GITHUB_TOKEN` 只有当前仓库权限，因此需要一个跨仓库的 PAT。

1. 打开 https://github.com/settings/tokens?type=beta （Fine-grained tokens）
2. 点击 **Generate new token**
3. 填写：
   - **Token name**：`goreleaser-homebrew-tap`
   - **Expiration**：按需选择（建议 90 天或更长，过期后需重新生成）
   - **Repository access**：选 **Only select repositories** → 勾选 `wuhanyuhan/homebrew-tap`
   - **Permissions** → **Repository permissions**：
     - `Contents`：**Read and write**（GoReleaser 需要推送文件）
     - 其余保持默认（No access）
4. 点击 **Generate token**
5. **立即复制 token**（页面关闭后无法再查看）

### 第三步：在 ks-devkit 仓库配置 Secret

1. 打开 https://github.com/wuhanyuhan/ks-devkit/settings/secrets/actions
2. 点击 **New repository secret**
3. 填写：
   - **Name**：`HOMEBREW_TAP_TOKEN`
   - **Secret**：粘贴第二步复制的 PAT
4. 点击 **Add secret**

### 第四步：推送配置变更

本仓库中已完成以下配置修改：

- `.goreleaser.yaml`：新增 `brews` 段，指定 formula 推送到 `wuhanyuhan/homebrew-tap`
- `.github/workflows/release.yaml`：`GITHUB_TOKEN` 改为优先使用 `HOMEBREW_TAP_TOKEN`

将这些变更提交并推送：

```bash
git add .goreleaser.yaml .github/workflows/release.yaml
git commit -m "feat: 配置 Homebrew Tap 自动发布"
git push origin master
```

### 第五步：发版验证

打一个 tag 触发 release workflow：

```bash
git tag v0.1.0-alpha
git push origin v0.1.0-alpha
```

发版完成后验证：

1. 检查 GitHub Actions：打开 https://github.com/wuhanyuhan/ks-devkit/actions ，确认 Release workflow 成功
2. 检查 formula 文件：打开 https://github.com/wuhanyuhan/homebrew-tap ，应该能看到 `Formula/ks.rb` 文件已自动生成
3. 本地测试安装：

```bash
brew tap wuhanyuhan/tap
brew install ks
ks --version   # 应输出 v0.1.0-alpha
```

## 后续发版

配置完成后，以后每次发版只需打 tag 推送，formula 会自动更新：

```bash
git tag v0.2.0
git push origin v0.2.0
# GoReleaser 自动构建 + 自动更新 homebrew-tap 中的 formula
```

用户侧更新：

```bash
brew upgrade ks
```

## 用户安装方式汇总

配置完成后，用户有以下安装方式：

| 方式 | 命令 | 适用场景 |
|------|------|----------|
| Homebrew | `brew install wuhanyuhan/tap/ks` | macOS / Linux 桌面 |
| 安装脚本 | `curl -fsSL https://raw.githubusercontent.com/wuhanyuhan/ks-devkit/master/install.sh \| bash` | Linux 服务器 / CI |
| GitHub Releases | 手动下载二进制 | 任意平台 |

## 常见问题

### Q: PAT 过期了怎么办？

重新生成一个 PAT（第二步），然后更新 ks-devkit 仓库的 `HOMEBREW_TAP_TOKEN` secret（第三步）。

### Q: Release workflow 报权限错误？

检查：
- `HOMEBREW_TAP_TOKEN` secret 是否已配置
- PAT 是否已过期
- PAT 的 Repository access 是否包含 `homebrew-tap` 仓库
- PAT 的 Contents 权限是否为 Read and write

### Q: 想改 formula 的描述或安装行为？

修改 `.goreleaser.yaml` 中的 `brews` 段，下次发版时 formula 会自动更新。
