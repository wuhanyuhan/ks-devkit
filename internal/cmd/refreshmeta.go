package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/keystoneadmin"
)

var refreshMetaCmd = &cobra.Command{
	Use:   "refresh-meta",
	Short: "改 manifest/代码后重新同步注册到本地 keystone（幂等）",
	Long:  "重新读 manifest.yaml 并幂等重同步——免去 docker compose down -v 重来。\n联调 loop：改码 → ks refresh-meta → 调能力验证。",
	RunE:  runRefreshMeta,
}

func init() {
	rootCmd.AddCommand(refreshMetaCmd)
	refreshMetaCmd.Flags().String("keystone-url", defaultKeystoneURL, "本地 keystone admin 地址")
	refreshMetaCmd.Flags().String("endpoint", defaultDevEndpoint, "你的 app MCP 端点（keystone 容器视角）")
	refreshMetaCmd.Flags().String("user", defaultAdminUser, "admin 用户名")
	refreshMetaCmd.Flags().String("password", defaultAdminPass, "admin 密码")
}

func runRefreshMeta(cmd *cobra.Command, _ []string) error {
	spec, err := loadLocalManifest()
	if err != nil {
		return err
	}
	url, _ := cmd.Flags().GetString("keystone-url")
	endpoint, _ := cmd.Flags().GetString("endpoint")
	user, _ := cmd.Flags().GetString("user")
	pass, _ := cmd.Flags().GetString("password")

	c := keystoneadmin.New(url)
	if err := c.Login(user, pass); err != nil {
		return err
	}
	// keystone install 对已存在 app 非幂等（返回 ErrAppAlreadyInstalled）。
	// 幂等重同步 = 先尽力卸载（首次运行时 app 尚未安装，该错误忽略），再以新 manifest 重装。
	// （选 uninstall+install 而非 upgrade：dev 同版本改 manifest 重同步，upgrade 走版本升级语义不贴合。）
	_ = c.UninstallApp(spec.ID)
	if err := c.InstallApp(keystoneadmin.InstallReq{
		AppID:            spec.ID,
		Version:          spec.Version,
		ExternalEndpoint: endpoint,
	}); err != nil {
		return err
	}
	fmt.Printf("✓ 已重新同步 %s 的 manifest 到本地 keystone\n", spec.ID)
	return nil
}
