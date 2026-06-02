package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/keystoneadmin"
	kstypes "github.com/wuhanyuhan/ks-types"
)

const (
	defaultKeystoneURL = "http://localhost:9988"
	defaultDevEndpoint = "http://host.docker.internal:8080/mcp"
	defaultAdminUser   = "admin"
	defaultAdminPass   = "admin"
)

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "把本地 manifest 注册进本地 keystone（能力网格联调）",
	Long: "读取当前目录 manifest.yaml，登录本地 keystone-dev（ks dev 拉起），\n" +
		"以 external_endpoint 模式 install——keystone 接管 manifest 注册（派生 canonical、\n" +
		"注册能力、挂 mcp_server/反代），你的 app 容器自己跑（默认 localhost:8080）。",
	RunE: runRegister,
}

func init() {
	rootCmd.AddCommand(registerCmd)
	registerCmd.Flags().String("keystone-url", defaultKeystoneURL, "本地 keystone admin 地址")
	registerCmd.Flags().String("endpoint", defaultDevEndpoint, "你的 app MCP 端点（keystone 容器视角）")
	registerCmd.Flags().String("user", defaultAdminUser, "admin 用户名")
	registerCmd.Flags().String("password", defaultAdminPass, "admin 密码")
}

func runRegister(cmd *cobra.Command, _ []string) error {
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
	if err := c.InstallApp(keystoneadmin.InstallReq{
		AppID:            spec.ID,
		Version:          spec.Version,
		ExternalEndpoint: endpoint,
	}); err != nil {
		return fmt.Errorf("%w\n（若提示已安装，用 ks refresh-meta 重新同步）", err)
	}
	fmt.Printf("✓ 已注册 %s 到本地 keystone（端点 %s）\n", spec.ID, endpoint)
	fmt.Println("  改 manifest/代码后用 ks refresh-meta 重新同步。")
	return nil
}

// loadLocalManifest 读 + 解析当前目录 manifest.yaml（register / refresh-meta 共用）。
func loadLocalManifest() (*kstypes.AppSpec, error) {
	raw, err := os.ReadFile(filepath.Join(".", "manifest.yaml"))
	if err != nil {
		return nil, fmt.Errorf("读取 manifest.yaml 失败（在 app 项目目录内运行）: %w", err)
	}
	spec, err := kstypes.ParseAppSpec(raw)
	if err != nil {
		return nil, fmt.Errorf("manifest.yaml 解析失败: %w", err)
	}
	return spec, nil
}
