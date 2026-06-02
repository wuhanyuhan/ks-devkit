package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/cmd/exitcode"
	"github.com/wuhanyuhan/ks-devkit/internal/config"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

var statusCmd = &cobra.Command{
	Use:   "status <slug>[@<version>]",
	Short: "查询应用版本状态（列表 / 详情）",
	Long: `查询应用版本状态。

  ks app status <slug>             列出应用最新 10 个版本
  ks app status <slug>@<version>   单版本详情
  --json                            机器可读 JSON 输出
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cred, err := auth.LoadFromEnvOrFile(auth.DefaultCredentialsPath())
		if err != nil {
			return exitcode.Wrap(fmt.Errorf("请先运行 ks auth login 或设置 KS_HUB_TOKEN: %w", err), exitcode.AuthOrPermission)
		}
		cfg, err := config.Load(config.DefaultConfigPath())
		if err != nil {
			return exitcode.Wrap(fmt.Errorf("加载配置失败: %w", err), exitcode.Generic)
		}
		client := hub.NewClient(cfg.HubURL, cred.AccessToken)

		slug, version := parseSlugAndVersion(args[0])
		jsonOut, _ := cmd.Flags().GetBool("json")
		return runStatus(client, cred, slug, version, jsonOut, os.Stdout)
	},
}

func init() {
	Cmd.AddCommand(statusCmd)
	statusCmd.Flags().Bool("json", false, "机器可读 JSON 输出")
}

// parseSlugAndVersion 拆分 "<slug>" 或 "<slug>@<version>"。
func parseSlugAndVersion(s string) (string, string) {
	at := strings.Index(s, "@")
	if at < 0 {
		return s, ""
	}
	return s[:at], s[at+1:]
}

// runStatus 执行 status 查询并写入 out。
// version 为空 → 列表模式；非空 → 详情模式。
func runStatus(client *hub.Client, cred *auth.Credentials, slug, version string, jsonOut bool, out io.Writer) error {
	if version == "" {
		page, err := client.ListVersionsPaged(slug, 10, 0)
		if err != nil {
			return mapStatusErrorToExit(err)
		}
		if jsonOut {
			return writeJSON(out, map[string]any{
				"app":      map[string]string{"slug": slug, "publisher_slug": cred.PublisherSlug},
				"versions": page.Items,
				"total":    page.Total,
			})
		}
		writeListTable(out, slug, cred.PublisherSlug, page)
		return nil
	}
	v, err := client.GetVersion(slug, version)
	if err != nil {
		return mapStatusErrorToExit(err)
	}
	if jsonOut {
		return writeJSON(out, v)
	}
	writeDetail(out, slug, v)
	return nil
}

func writeJSON(out io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = out.Write(append(data, '\n'))
	return err
}

func writeListTable(out io.Writer, slug, publisher string, page *hub.ListVersionsPage) {
	fmt.Fprintf(out, "%s (publisher: %s)\n", slug, publisher)
	fmt.Fprintln(out, strings.Repeat("─", 60))
	fmt.Fprintf(out, "  %-12s %-12s %-22s %s\n", "Version", "Status", "Submitted", "Review Path")
	for _, v := range page.Items {
		fmt.Fprintf(out, "  %-12s %-12s %-22s %s\n", v.Version, v.Status, v.SubmittedAt, v.ReviewPath)
		if v.ReviewReason != "" {
			fmt.Fprintf(out, "                                              Reason: %s\n", v.ReviewReason)
		}
	}
	fmt.Fprintf(out, "\n(showing latest %d of %d)\n", len(page.Items), page.Total)
}

func writeDetail(out io.Writer, slug string, v *hub.Version) {
	fmt.Fprintf(out, "%s %s\n", slug, v.Version)
	fmt.Fprintln(out, strings.Repeat("─", 60))
	fmt.Fprintf(out, "  Status:       %s\n", v.Status)
	fmt.Fprintf(out, "  Review Path:  %s\n", v.ReviewPath)
	if v.SubmittedAt != "" {
		fmt.Fprintf(out, "  Submitted:    %s\n", v.SubmittedAt)
	}
	if v.ReviewedAt != "" {
		fmt.Fprintf(out, "  Reviewed:     %s\n", v.ReviewedAt)
	}
	if v.BuiltAt != "" {
		fmt.Fprintf(out, "  Built:        %s\n", v.BuiltAt)
	}
	if v.KSPSha256 != "" {
		fmt.Fprintf(out, "  KSP SHA256:   %s\n", v.KSPSha256)
		fmt.Fprintf(out, "  KSP Size:     %d bytes\n", v.KSPSizeBytes)
	}
	fmt.Fprintf(out, "  Available:    %v\n", v.Available)
	if v.Changelog != "" {
		fmt.Fprintf(out, "  Changelog:\n    %s\n", v.Changelog)
	}
	if v.ReviewReason != "" {
		fmt.Fprintf(out, "  Review Reason: %s\n", v.ReviewReason)
	}
}

// mapStatusErrorToExit 把 hub 错误映射到 status 命令的退出码（与 publish 共用规则）。
func mapStatusErrorToExit(err error) error {
	switch {
	case hub.IsUnauthorized(err) || hub.IsForbidden(err):
		return exitcode.Wrap(err, exitcode.AuthOrPermission)
	case hub.IsNotFound(err):
		return exitcode.Wrap(err, exitcode.Generic)
	case hub.IsServerError(err):
		return exitcode.Wrap(err, exitcode.Network)
	default:
		return exitcode.Wrap(err, exitcode.Generic)
	}
}
