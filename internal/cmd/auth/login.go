package auth

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/buildinfo"
	"github.com/wuhanyuhan/ks-devkit/internal/cmd/exitcode"
	"github.com/wuhanyuhan/ks-devkit/internal/config"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
	"golang.org/x/term"
)

// patTokenLength PAT 的固定长度：前缀 8 字符 + base32 32 字符。
const patTokenLength = 40

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "登录 Keystone Hub",
	Long: `登录 Keystone Hub。两种模式：

  交互式（默认）：
    ks auth login
    打开浏览器完成授权，CLI 轮询获取 user JWT。

  密码模式（兼容旧流程）：
    ks auth login --password [--email <addr>]
    在终端输入邮箱 + 密码登录。

  非交互（CI 推荐）：
    ks auth login --token <ksh_pat_...>
    直接注入 PAT，调 whoami 验证后写入 credentials.json。
`,
	RunE: RunLogin,
}

func init() {
	Cmd.AddCommand(loginCmd)
	AddLoginFlags(loginCmd)
}

// AddLoginFlags 注册登录命令的 flags，供别名命令复用。
func AddLoginFlags(cmd *cobra.Command) {
	cmd.Flags().String("email", "", "邮箱地址")
	cmd.Flags().Bool("password", false, "使用终端邮箱 + 密码登录（兼容旧流程）")
	cmd.Flags().String("token", "", "Personal Access Token (ksh_pat_*)，与 --email 互斥")
	// --api-key 历史 flag 保留 + 标 deprecated；行为等价 --token
	cmd.Flags().String("api-key", "", "DEPRECATED：用 --token 代替")
	_ = cmd.Flags().MarkDeprecated("api-key", "use --token instead")
}

// RunLogin 执行登录逻辑（交互或 --token 非交互）。
func RunLogin(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return exitcode.Wrap(fmt.Errorf("加载配置失败: %w", err), exitcode.Generic)
	}

	token, _ := cmd.Flags().GetString("token")
	if token == "" {
		// 兼容历史 --api-key
		if legacy, _ := cmd.Flags().GetString("api-key"); legacy != "" {
			token = legacy
		}
	}
	email, _ := cmd.Flags().GetString("email")
	passwordMode, _ := cmd.Flags().GetBool("password")

	if token != "" && email != "" {
		return exitcode.Wrap(fmt.Errorf("--token 与 --email 互斥"), exitcode.ClientConfig)
	}

	if token != "" {
		return loginWithToken(cfg.HubURL, token)
	}
	if passwordMode || email != "" {
		return loginInteractive(cfg.HubURL, email)
	}
	return loginWithBrowser(cfg.HubURL)
}

var openBrowser = func(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

var sleep = time.Sleep

func loginWithBrowser(hubURL string) error {
	hostname, _ := os.Hostname()
	client := hub.NewClient(hubURL, "")
	start, err := client.StartDeviceAuth(hub.DeviceAuthStartRequest{
		ClientName: "ks CLI",
		Hostname:   hostname,
		CLIVersion: buildinfo.Version(),
	})
	if err != nil {
		if hub.IsServerError(err) {
			return exitcode.Wrap(fmt.Errorf("hub 不可用: %w", err), exitcode.Network)
		}
		return exitcode.Wrap(fmt.Errorf("发起浏览器登录失败: %w", err), exitcode.Network)
	}

	fmt.Printf("一次性验证码: %s\n", start.UserCode)
	fmt.Printf("正在打开浏览器: %s\n", start.VerificationURI)
	if err := openBrowser(start.VerificationURI); err != nil {
		fmt.Printf("无法自动打开浏览器，请手动访问: %s\n", start.VerificationURI)
	}
	fmt.Println("等待浏览器授权确认...")

	interval := time.Duration(start.Interval) * time.Second
	if interval < 0 {
		interval = 0
	}
	expiresIn := start.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 15 * 60
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	for {
		result, err := client.PollDeviceToken(start.DeviceCode)
		if err == nil {
			return saveUserLogin(hubURL, result, "")
		}
		var apiErr *hub.APIError
		if !errors.As(err, &apiErr) {
			return exitcode.Wrap(fmt.Errorf("浏览器登录失败: %w", err), exitcode.Network)
		}
		switch apiErr.Code {
		case hub.CodeDeviceAuthorizationPending:
			if time.Now().After(deadline) {
				return exitcode.Wrap(fmt.Errorf("设备授权已超时"), exitcode.AuthOrPermission)
			}
			sleep(interval)
		case hub.CodeDeviceAuthorizationSlowDown:
			interval += 5 * time.Second
			sleep(interval)
		case hub.CodeDeviceAuthorizationExpired, hub.CodeDeviceAuthorizationDenied:
			return exitcode.Wrap(fmt.Errorf("浏览器登录失败: %w", err), exitcode.AuthOrPermission)
		default:
			return exitcode.Wrap(fmt.Errorf("浏览器登录失败: %w", err), exitcode.Network)
		}
	}
}

// loginWithToken 用 PAT 非交互登录：校验前缀/长度 → whoami → 写盘。
func loginWithToken(hubURL, token string) error {
	if !strings.HasPrefix(token, "ksh_pat_") || len(token) != patTokenLength {
		return exitcode.Wrap(
			fmt.Errorf("PAT 格式错误：要求 ksh_pat_ 前缀 + 32 字符，实际长度=%d", len(token)),
			exitcode.AuthOrPermission,
		)
	}

	client := hub.NewClient(hubURL, token)
	whoami, err := client.Whoami()
	if err != nil {
		if hub.IsUnauthorized(err) {
			return exitcode.Wrap(
				fmt.Errorf("token 被 hub 拒绝（已撤销或过期）: %w", err),
				exitcode.AuthOrPermission,
			)
		}
		if hub.IsServerError(err) {
			return exitcode.Wrap(fmt.Errorf("hub 不可用: %w", err), exitcode.Network)
		}
		return exitcode.Wrap(err, exitcode.Network)
	}

	if whoami.AuthType != "pat" {
		return exitcode.Wrap(
			fmt.Errorf("whoami 返回 auth_type=%q，期望 pat", whoami.AuthType),
			exitcode.AuthOrPermission,
		)
	}

	cred := &auth.Credentials{
		AuthType:      auth.AuthTypePAT,
		AccessToken:   token,
		PublisherSlug: whoami.PublisherSlug,
		Scopes:        whoami.Scopes,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := auth.SaveCredentials(auth.DefaultCredentialsPath(), cred); err != nil {
		return exitcode.Wrap(fmt.Errorf("保存凭证失败: %w", err), exitcode.Generic)
	}

	fmt.Printf("✔ Token valid. Bound to publisher: %s\n", whoami.PublisherSlug)
	if len(whoami.Scopes) > 0 {
		fmt.Printf("✔ Scopes: %s\n", strings.Join(whoami.Scopes, ", "))
	}
	fmt.Println("✔ Saved to ~/.ks/credentials.json")
	return nil
}

// loginInteractive 现有交互登录路径（邮箱 + 密码）。
func loginInteractive(hubURL, email string) error {
	if email == "" {
		fmt.Print("邮箱: ")
		reader := bufio.NewReader(os.Stdin)
		email, _ = reader.ReadString('\n')
		email = strings.TrimSpace(email)
	}

	fmt.Print("密码: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return exitcode.Wrap(fmt.Errorf("读取密码失败: %w", err), exitcode.Generic)
	}
	fmt.Println()
	password := string(passwordBytes)

	client := hub.NewClient(hubURL, "")
	result, err := client.Login(email, password)
	if err != nil {
		if hub.IsUnauthorized(err) {
			return exitcode.Wrap(fmt.Errorf("登录失败: %w", err), exitcode.AuthOrPermission)
		}
		return exitcode.Wrap(fmt.Errorf("登录失败: %w", err), exitcode.Network)
	}

	if err := saveUserLogin(hubURL, result, email); err != nil {
		return err
	}
	fmt.Printf("✓ 已登录为 %s\n", email)
	return nil
}

func saveUserLogin(hubURL string, result *hub.LoginResponse, fallbackEmail string) error {
	email := fallbackEmail
	profile, err := hub.NewClient(hubURL, result.AccessToken).GetProfile()
	if err == nil && profile.Email != "" {
		email = profile.Email
	}
	cred := &auth.Credentials{
		AuthType:     auth.AuthTypeUser,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		Email:        email,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := auth.SaveCredentials(auth.DefaultCredentialsPath(), cred); err != nil {
		return exitcode.Wrap(fmt.Errorf("保存凭证失败: %w", err), exitcode.Generic)
	}
	if email != "" && email != fallbackEmail {
		fmt.Printf("✓ 已登录为 %s\n", email)
	} else if fallbackEmail == "" && email != "" {
		fmt.Printf("✓ 已登录为 %s\n", email)
	}
	return nil
}
