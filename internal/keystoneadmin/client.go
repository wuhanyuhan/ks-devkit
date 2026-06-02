// Package keystoneadmin 是本地 keystone-dev 的 admin HTTP 客户端，
// 供 ks register / refresh-meta 编排（登录 → install app from manifest）。
// 只调 admin HTTP 契约，不依赖 keystone 内部实现。
package keystoneadmin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: http.DefaultClient}
}

// Login 用 admin 账密换 JWT，存入 c.token。keystone POST /v1/auth/login 收 {username,password}。
func (c *Client) Login(username, password string) error {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := c.http.Post(c.baseURL+"/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("登录本地 keystone 失败（确认 ks dev 已启动）: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("登录失败 HTTP %d: %s", resp.StatusCode, b)
	}
	var out struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if out.Data.AccessToken == "" {
		return fmt.Errorf("登录响应无 access_token")
	}
	c.token = out.Data.AccessToken
	return nil
}

// InstallReq 对齐 keystone 安装请求契约的本地联调子集。
// ExternalEndpoint 非空 → keystone 强制 runtime.mode=none、只接管 manifest 注册
// （nav/permissions/mcp_server/反代/capabilities），走与平台安装同一段 install 主流程。
type InstallReq struct {
	AppID            string `json:"app_id"`
	Version          string `json:"version,omitempty"`
	ExternalEndpoint string `json:"external_endpoint,omitempty"`
}

// InstallApp 调 POST /v1/admin/apps/install（JSON body，keystone ShouldBindJSON）。
func (c *Client) InstallApp(req InstallReq) error {
	return c.do("/v1/admin/apps/install", "", req)
}

// UninstallApp 调 POST /v1/admin/apps/uninstall?app_id=<id>。
// 注意：keystone 该端点从 query 读 app_id（c.Query），不是 JSON body——与 install 不对称。
func (c *Client) UninstallApp(appID string) error {
	q := url.Values{"app_id": {appID}}.Encode()
	return c.do("/v1/admin/apps/uninstall", q, nil)
}

// do 发一个带 Bearer token 的 POST：payload 非 nil 时作为 JSON body，rawQuery 非空时拼到 URL。
func (c *Client) do(path, rawQuery string, payload any) error {
	if c.token == "" {
		return fmt.Errorf("未登录：先调 Login")
	}
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewReader(b)
	}
	u := c.baseURL + path
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	httpReq, _ := http.NewRequest(http.MethodPost, u, body)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("调用 %s 失败: %w", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s HTTP %d: %s", path, resp.StatusCode, b)
	}
	// keystone 统一 result 信封 {code,message,data}；code!=0 即业务失败。
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(b, &env)
	if env.Code != 0 {
		return fmt.Errorf("%s 失败 code=%d: %s", path, env.Code, env.Message)
	}
	return nil
}
