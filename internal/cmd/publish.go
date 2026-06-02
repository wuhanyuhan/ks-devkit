package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/build"
	"github.com/wuhanyuhan/ks-devkit/internal/cmd/exitcode"
	"github.com/wuhanyuhan/ks-devkit/internal/config"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
	manifestutil "github.com/wuhanyuhan/ks-devkit/internal/manifest"
)

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "构建并发布应用到 Keystone Hub",
	Long:  "智能一键发布：构建 → 检测应用（不存在则自动创建）→ 上传版本 → 提交审核",
	RunE:  runPublish,
}

func init() {
	rootCmd.AddCommand(publishCmd)
	publishCmd.Flags().String("changelog", "", "版本变更说明")
	publishCmd.Flags().Bool("no-wait", false, "fast-track 路径也立即返回，不阻塞等审核结果")
	publishCmd.Flags().Duration("wait-manual", 0, "manual 路径阻塞等待时长（如 30m），默认立即返回")
	publishCmd.Flags().Bool("dry-run", false, "执行 preflight + build 但不上传，打印将提交内容")
	publishCmd.Flags().Bool("allow-secrets", false, "跳过 preflight 第三层 secret 扫描（仅本地手动验证用）")
	publishCmd.Flags().Bool("json", false, "机器可读 NDJSON 输出（每行一个事件，最后一行含 review_path）")
}

func runPublish(cmd *cobra.Command, args []string) error {
	jsonOut, _ := cmd.Flags().GetBool("json")

	// 1. 加载凭证
	cred, err := auth.LoadFromEnvOrFile(auth.DefaultCredentialsPath())
	if err != nil {
		return exitcode.Wrap(
			fmt.Errorf("请先运行 ks auth login 或设置 KS_HUB_TOKEN: %w", err),
			exitcode.AuthOrPermission,
		)
	}

	// 2. preflight + 构建
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	allowSecrets, _ := cmd.Flags().GetBool("allow-secrets")
	buildOpts := &build.BuildOptions{
		DryRun:    dryRun,
		Preflight: &build.PreflightOptions{AllowSecrets: allowSecrets},
	}
	result, report, err := build.BuildWithOptions(".", filepath.Join(".", "dist"), buildOpts)
	if err != nil {
		return exitcode.Wrap(printPreflightFailure(err, report), exitcode.ClientConfig)
	}

	// dry-run：打印结果后立即返回
	if dryRun {
		if err := manifestutil.ValidateStoreQuality(result.AppSpec); err != nil {
			return exitcode.Wrap(fmt.Errorf("store 展示字段不完整: %w", err), exitcode.ClientConfig)
		}
		if jsonOut {
			emitJSONEvent("dry_run_done", map[string]any{
				"app_id":      result.AppID,
				"version":     result.Version,
				"file_count":  report.FileCount,
				"total_size":  report.TotalSize,
				"manifest":    result.AppSpec,
				"review_path": "",
			})
		} else {
			printDryRunReport(result, report)
		}
		return nil
	}

	// 3. 双源校验：PAT 模式下 token_publisher vs manifest.publisher
	if err := verifyPublisherMatch(cred, result.AppSpec.Publisher); err != nil {
		return err
	}

	// 4. 加载配置
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	client := hub.NewClient(cfg.HubURL, cred.AccessToken)

	// 5. manifest fallback chain：缺字段时引导式补齐（LLM → CHANGELOG.md → inline editor）
	//    JSON 模式跳过（NDJSON 不能与 stdin prompt 混用）；dry-run 已在前面 return
	fbCtx := cmd.Context()
	if fbCtx == nil {
		fbCtx = context.Background()
	}
	manifestPath := filepath.Join(".", "manifest.yaml")
	changed, err := runPublishFallback(fbCtx, client, os.Stdin, os.Stdout, result.AppSpec, ".", manifestPath, jsonOut)
	if err != nil {
		return exitcode.Wrap(err, exitcode.Generic)
	}
	if changed {
		fmt.Println("已更新 manifest.yaml；请 git diff 检查后提交")
	}

	if err := manifestutil.ValidateStoreQuality(result.AppSpec); err != nil {
		return exitcode.Wrap(fmt.Errorf("store 展示字段不完整: %w", err), exitcode.ClientConfig)
	}

	// 6. 同步 app metadata：不存在则 CreateApp（含 metadata），存在则 UpdateApp（PUT 全量）。
	//    metadata 来自 fallback chain 后的 AppSpec（作者填的 + LLM 中文兜底）。
	//    历史 bug：旧版只在 app 不存在时调 CreateApp 写 app 的 summary，已存在时
	//    永远不更新 → store 详情页 author-source metadata 永远空，依赖后端 AI metadata
	//    认领才能补上。新逻辑让每次 publish 都把 manifest 同步到后端。
	metaSummary := result.AppSpec.Summary.Get("zh-CN")
	metaDescription := result.AppSpec.Description.Get("zh-CN")
	metaCategory := result.AppSpec.Category
	metaTags := result.AppSpec.Tags.Get("zh-CN")
	metaPricingType := string(result.AppSpec.Pricing.Type)

	_, err = client.GetApp(result.AppID)
	if err != nil {
		if !hub.IsNotFound(err) {
			return mapHubErrorToExit(fmt.Errorf("查询应用 %s 失败: %w", result.AppID, err))
		}
		if result.AppSpec.Publisher == "" {
			return fmt.Errorf("应用 %s 不存在且 manifest.yaml 中未指定 publisher，无法自动创建", result.AppID)
		}

		emitText(jsonOut, "应用 %s 不存在，正在自动创建...\n", result.AppID)

		pub, pubErr := client.GetPublisher(result.AppSpec.Publisher)
		if pubErr != nil {
			return mapHubErrorToExit(fmt.Errorf("查询 publisher %q 失败: %w（请先通过 ks publisher create 创建）", result.AppSpec.Publisher, pubErr))
		}

		_, createErr := client.CreateApp(&hub.CreateAppRequest{
			PublisherID: pub.ID,
			AppID:       result.AppID,
			Name:        result.AppSpec.Name,
			Type:        string(result.AppSpec.Type),
			Summary:     metaSummary,
			Description: metaDescription,
			Category:    metaCategory,
			Tags:        metaTags,
			PricingType: metaPricingType,
		})
		if createErr != nil {
			return mapHubErrorToExit(fmt.Errorf("创建应用失败: %w", createErr))
		}
		if jsonOut {
			emitJSONEvent("app_created", map[string]any{"app_id": result.AppID})
		} else {
			fmt.Printf("✓ 已创建应用 %s\n", result.AppID)
		}
	} else {
		// 已存在：同步 metadata 到后端（PUT 全量）。
		// 失败仅 warn 不中断：version 已经构建好，更重要的是上传成功；
		// metadata 缺失可后续手动 PUT 或下次 publish 重试。
		_, updateErr := client.UpdateApp(result.AppID, &hub.UpdateAppRequest{
			Name:        result.AppSpec.Name,
			Summary:     metaSummary,
			Description: metaDescription,
			Category:    metaCategory,
			Tags:        metaTags,
			PricingType: metaPricingType,
		})
		if updateErr != nil {
			emitText(jsonOut, "⚠ 同步 app metadata 失败（version 仍会继续上传）: %v\n", updateErr)
		} else if jsonOut {
			emitJSONEvent("app_metadata_synced", map[string]any{"app_id": result.AppID})
		} else {
			fmt.Printf("✓ 已同步 %s metadata\n", result.AppID)
		}
	}

	// 7. 序列化 manifest 为 JSON 用于上传
	manifestJSON, err := manifestutil.MarshalManifestJSONForUpload(manifestPath, result.AppSpec)
	if err != nil {
		return fmt.Errorf("序列化 manifest 失败: %w", err)
	}

	// 权限声明随版本一起上传，便于 Hub 侧直接查询
	var permissionsJSON []byte
	if len(result.AppSpec.Permissions) > 0 {
		permissionsJSON, err = json.Marshal(result.AppSpec.Permissions)
		if err != nil {
			return fmt.Errorf("序列化 permissions 失败: %w", err)
		}
	}

	// install_spec 可选
	var installSpecJSON []byte
	if result.InstallSpec != nil {
		installSpecJSON, err = json.Marshal(result.InstallSpec)
		if err != nil {
			return fmt.Errorf("序列化 install_spec 失败: %w", err)
		}
	}

	changelog, _ := cmd.Flags().GetString("changelog")

	// 8. 上传版本
	emitText(jsonOut, "正在上传 %s v%s ...\n", result.AppID, result.Version)
	if err := client.UploadVersion(&hub.UploadVersionRequest{
		AppID:          result.AppID,
		Version:        result.Version,
		TarballPath:    result.TarballPath,
		Manifest:       manifestJSON,
		Permissions:    permissionsJSON,
		InstallSpec:    installSpecJSON,
		Changelog:      changelog,
		CompatKeystone: result.AppSpec.Compatibility.Keystone,
	}); err != nil {
		return mapHubErrorToExit(fmt.Errorf("上传失败: %w", err))
	}
	if jsonOut {
		emitJSONEvent("upload_done", map[string]any{
			"app_id":  result.AppID,
			"version": result.Version,
		})
	}

	// 9. 提交审核
	submitResp, err := client.SubmitVersion(result.AppID, result.Version)
	if err != nil {
		return mapHubErrorToExit(fmt.Errorf("提交审核失败: %w", err))
	}
	if jsonOut {
		emitJSONEvent("submit_done", map[string]any{
			"app_id":      result.AppID,
			"version":     result.Version,
			"review_id":   submitResp.ReviewID,
			"review_path": submitResp.ReviewPath,
		})
	} else {
		fmt.Printf("✓ 已提交 %s v%s（review_id=%d, path=%s）\n",
			result.AppID, result.Version, submitResp.ReviewID, submitResp.ReviewPath)
	}

	noWait, _ := cmd.Flags().GetBool("no-wait")
	waitManual, _ := cmd.Flags().GetDuration("wait-manual")

	if noWait {
		// JSON 模式下保证 tail -1 含 review_path
		if jsonOut {
			emitJSONEvent("no_wait", map[string]any{"review_path": submitResp.ReviewPath})
		}
		return nil
	}

	// cmd.Context() 在 Cobra 1.2+ 可用；为兼容旧版本 fallback 到 Background。
	parentCtx := cmd.Context()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, 60*time.Second)
	defer cancel()

	switch submitResp.ReviewPath {
	case "fast-track":
		emitText(jsonOut, "⏳ Waiting for fast-track decision...\n")
		v, werr := waitForReview(ctx, client, result.AppID, result.Version, fastTrackPollDelays)
		if werr != nil {
			if werr == context.DeadlineExceeded {
				fmt.Fprintln(os.Stderr, "⚠ fast-track 60s 超时仍 pending（理论异常），exit 0 + 警告")
				if jsonOut {
					emitJSONEvent("timeout", map[string]any{"review_path": "fast-track", "status": "pending"})
				}
				return nil
			}
			return werr
		}
		return interpretTerminal(v, jsonOut)
	case "manual":
		if waitManual <= 0 {
			if jsonOut {
				emitJSONEvent("manual_pending", map[string]any{
					"app_id":      result.AppID,
					"version":     result.Version,
					"review_path": "manual",
				})
			} else {
				fmt.Printf("ℹ Submitted to manual review queue.\n  Use 'ks app status %s@%s' to check progress.\n  Or pass --wait-manual=30m to block until reviewed.\n",
					result.AppID, result.Version)
			}
			return nil
		}
		emitText(jsonOut, "⏳ Waiting up to %s for manual review decision...\n", waitManual)
		ctxM, cancelM := context.WithTimeout(parentCtx, waitManual)
		defer cancelM()
		delays := manualPollDelays(waitManual)
		v, werr := waitForReview(ctxM, client, result.AppID, result.Version, delays)
		if werr != nil {
			if werr == context.DeadlineExceeded {
				fmt.Fprintln(os.Stderr, "⚠ manual 阻塞超时，仍 pending；可用 ks app status 跟进。exit 0 + 警告")
				if jsonOut {
					emitJSONEvent("timeout", map[string]any{"review_path": "manual", "status": "pending"})
				}
				return nil
			}
			return werr
		}
		return interpretTerminal(v, jsonOut)
	default:
		fmt.Fprintf(os.Stderr, "⚠ unknown review_path=%q，跳过等待\n", submitResp.ReviewPath)
		if jsonOut {
			emitJSONEvent("unknown_review_path", map[string]any{"review_path": submitResp.ReviewPath})
		}
		return nil
	}
}

// emitText 在非 JSON 模式下输出人面文本；JSON 模式下静默。
func emitText(jsonOut bool, format string, args ...any) {
	if !jsonOut {
		fmt.Printf(format, args...)
	}
}

// emitJSONEvent 输出一行 NDJSON 事件到 stdout。
// yaml 调用方靠 `tail -1 publish.out | jq -r '.review_path'` 提取 review_path，
// 因此每条退出路径都应保证最后一条事件含 review_path 字段（即使为空字符串）。
func emitJSONEvent(event string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["event"] = event
	data, err := json.Marshal(fields)
	if err != nil {
		// JSON 序列化对纯 string/int 字段不会失败；走兜底以防万一
		fmt.Fprintf(os.Stderr, "emitJSONEvent: marshal failed: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

// mapHubErrorToExit 把 hub 客户端错误映射到 ks-devkit 7 值退出码。
// 调用方法：if err := client.UploadVersion(...); err != nil { return mapHubErrorToExit(err) }
func mapHubErrorToExit(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case hub.IsUnauthorized(err) || hub.IsForbidden(err):
		return exitcode.Wrap(err, exitcode.AuthOrPermission)
	case hub.IsConflict(err):
		return exitcode.Wrap(err, exitcode.DuplicateVersion)
	case hub.IsServerError(err):
		return exitcode.Wrap(err, exitcode.Network)
	case hub.IsNotFound(err):
		return exitcode.Wrap(err, exitcode.Generic)
	default:
		// APIError 业务错且未识别归类 → Generic；ks-devkit 内部错也归 Generic
		return exitcode.Wrap(err, exitcode.Generic)
	}
}

// fastTrackPollDelays fast-track 路径默认轮询节奏：1+2+5+10+20+20=58s ≤ 60s 上限。
var fastTrackPollDelays = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	20 * time.Second,
	20 * time.Second,
}

// reviewTerminalStatuses 表示已经走完 review 流程的状态。
func isReviewTerminal(status string) bool {
	switch status {
	case "approved", "rejected", "available":
		return true
	}
	return false
}

// waitForReview 阻塞轮询版本状态直到 terminal 或 ctx 超时。
// delays 是每次轮询前的等待时间序列；用尽后停止轮询。
// 返回最后看到的 Version；若 ctx 超时，返回 ctx.Err()。
func waitForReview(ctx context.Context, client *hub.Client, appID, version string, delays []time.Duration) (*hub.Version, error) {
	var last *hub.Version
	for _, d := range delays {
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(d):
		}
		v, err := client.GetVersion(appID, version)
		if err != nil {
			return last, mapHubErrorToExit(err)
		}
		last = v
		if isReviewTerminal(v.Status) {
			return v, nil
		}
	}
	return last, nil // 未达 terminal：调用方解读为 timeout
}

// interpretTerminal 把 review terminal 状态映射为退出码。
// jsonOut=true 时同时发 NDJSON terminal 事件（保证含 review_path 用于 yaml tail -1 提取）。
func interpretTerminal(v *hub.Version, jsonOut bool) error {
	if v == nil {
		return nil
	}
	switch v.Status {
	case "approved", "available":
		if jsonOut {
			emitJSONEvent("terminal", map[string]any{
				"status":         v.Status,
				"review_path":    v.ReviewPath,
				"ksp_sha256":     v.KSPSha256,
				"ksp_size_bytes": v.KSPSizeBytes,
			})
		} else {
			fmt.Printf("✔ Approved (path=%s)\n", v.ReviewPath)
			if v.KSPSha256 != "" {
				fmt.Printf("✔ Built KSP (sha256: %s, %d bytes)\n", v.KSPSha256, v.KSPSizeBytes)
			}
		}
		return nil
	case "rejected":
		if jsonOut {
			emitJSONEvent("terminal", map[string]any{
				"status":        v.Status,
				"review_path":   v.ReviewPath,
				"review_reason": v.ReviewReason,
			})
		}
		return exitcode.Wrap(
			fmt.Errorf("review rejected (path=%s): %s", v.ReviewPath, v.ReviewReason),
			exitcode.ReviewRejected,
		)
	default:
		// pending / 未知：保守 exit 0 + 警告
		fmt.Fprintf(os.Stderr, "⚠ 状态 %q 不是预期的 terminal\n", v.Status)
		if jsonOut {
			emitJSONEvent("terminal", map[string]any{
				"status":      v.Status,
				"review_path": v.ReviewPath,
			})
		}
		return nil
	}
}

// manualPollDelays 根据 timeout 分摊轮询间隔；总和 ≤ timeout。
// 简化策略：均匀拆成 N 段，N = min(20, timeout/30s)，最少每 30s 一次。
func manualPollDelays(timeout time.Duration) []time.Duration {
	if timeout <= 30*time.Second {
		return []time.Duration{timeout}
	}
	step := 30 * time.Second
	n := int(timeout / step)
	if n > 20 {
		n = 20
		step = timeout / time.Duration(n)
	}
	out := make([]time.Duration, n)
	for i := range out {
		out[i] = step
	}
	return out
}

// verifyPublisherMatch 在 PAT 模式下进行客户端 fail-fast 校验：
// credentials.publisher_slug 必须等于 manifest.publisher，否则返回 exit 2 错误。
//
// 三种 skip 情形：
//  1. AuthType != PAT（user JWT 由 hub 端 RequireMember 兜底）
//  2. cred.PublisherSlug 为空（env 模式首次加载，调用方应稍后调 whoami 填充再调一次）
//  3. manifestPublisher 为空（manifest 没声明 publisher，由 hub 端处理）
func verifyPublisherMatch(cred *auth.Credentials, manifestPublisher string) error {
	if cred.AuthType != auth.AuthTypePAT {
		return nil
	}
	if cred.PublisherSlug == "" || manifestPublisher == "" {
		return nil
	}
	if cred.PublisherSlug == manifestPublisher {
		return nil
	}
	return exitcode.Wrap(
		fmt.Errorf("publisher 错配：token 绑定 publisher=%s，manifest.yaml publisher=%s\n请确认 manifest 是否正确，或更换为对应 publisher 的 token",
			cred.PublisherSlug, manifestPublisher),
		exitcode.AuthOrPermission,
	)
}

// printPreflightFailure 把 preflight 失败信息渲染成可读 stderr 输出。
func printPreflightFailure(err error, report *build.PreflightReport) error {
	if report == nil {
		return err
	}
	if len(report.SecretMatches) > 0 {
		fmt.Fprintln(os.Stderr, "ks publish: secret detected in source files")
		for _, m := range report.SecretMatches {
			fmt.Fprintf(os.Stderr, "  %s:%d: matched %s\n", m.File, m.Line, m.Rule)
		}
		fmt.Fprintln(os.Stderr, "\n请将 secret 移出仓库或加入 .gitignore / .ksignore。")
		fmt.Fprintln(os.Stderr, "若确为误报，可用 --allow-secrets 跳过（仅供本地手动验证）。")
		return err
	}
	if report.SizeLimitHit || report.FileLimitHit {
		fmt.Fprintln(os.Stderr, "ks publish: tarball exceeds limit")
		fmt.Fprintf(os.Stderr, "  Total size:  %s\n", humanSize(report.TotalSize))
		fmt.Fprintf(os.Stderr, "  File count:  %d\n", report.FileCount)
		return err
	}
	return err
}

// printDryRunReport 渲染 --dry-run 输出（含文件清单）。
func printDryRunReport(result *build.BuildResult, report *build.PreflightReport) {
	fmt.Println("✔ Preflight passed")
	fmt.Printf("  Hard exclusions:   %d paths\n", len(report.ExcludedHard))
	fmt.Printf("  .gitignore/.ksignore: %d paths\n", len(report.ExcludedIgnore))
	fmt.Printf("  Secret scan:       %d files scanned\n", report.FileCount)
	fmt.Printf("  Size:              %s / %s\n", humanSize(report.TotalSize), humanSize(defaultDryRunSizeLimit()))
	fmt.Printf("  File count:        %d / %d\n", report.FileCount, defaultDryRunFileLimit())
	fmt.Println()
	fmt.Println("Tarball contents:")
	for _, f := range report.IncludedFiles {
		fmt.Printf("  %s\n", f)
	}
	fmt.Println()
	if result != nil {
		fmt.Println("Would submit:")
		fmt.Printf("  app:        %s\n", result.AppID)
		fmt.Printf("  version:    %s\n", result.Version)
		if result.AppSpec != nil {
			fmt.Printf("  publisher:  %s\n", result.AppSpec.Publisher)
		}
	}
	fmt.Println("\nExit 0 (dry-run, no upload)")
}

func humanSize(b int64) string {
	const k = 1024
	if b < k {
		return fmt.Sprintf("%dB", b)
	}
	if b < k*k {
		return fmt.Sprintf("%.1fKB", float64(b)/k)
	}
	if b < k*k*k {
		return fmt.Sprintf("%.1fMB", float64(b)/(k*k))
	}
	return fmt.Sprintf("%.1fGB", float64(b)/(k*k*k))
}

func defaultDryRunSizeLimit() int64 { return 100 * 1024 * 1024 }
func defaultDryRunFileLimit() int   { return 10000 }
