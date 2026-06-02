package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
	}
}

type APIResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// APIError 表示 Hub API 返回的业务错误（code != 0）
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// IsNotFound 判断错误是否为资源不存在（code 404xx）
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code/100 == 404
	}
	return false
}

// HTTPError 表示底层 HTTP 协议错误（5xx、JSON 解析失败、连接错误等），
// 与 APIError（业务错误码 != 0）正交。退出码映射时把 HTTPError 归到 Network。
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

// IsUnauthorized 判断是否为业务 401xx（token 无效 / 撤销 / 过期）。
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code/100 == 401
	}
	return false
}

// IsForbidden 判断是否为业务 403xx（scope 缺失 / 跨 publisher）。
func IsForbidden(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code/100 == 403
	}
	return false
}

// IsConflict 判断是否为业务 409xx（典型场景：版本已存在）。
func IsConflict(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code/100 == 409
	}
	return false
}

// IsServerError 判断是否为底层 HTTP 5xx / 网络错。
func IsServerError(err error) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 500
	}
	return false
}

func (c *Client) do(method, path string, body any) (*APIResponse, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var apiResp APIResponse
	if jsonErr := json.Unmarshal(respBody, &apiResp); jsonErr != nil {
		// 非 JSON 响应（网关错误、空 body 等），把 status code + 前 200 字节暴露给用户
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: truncateBody(respBody, 200)}
	}
	if apiResp.Code != 0 {
		return nil, &APIError{Code: apiResp.Code, Message: apiResp.Message}
	}
	return &apiResp, nil
}

func truncateBody(body []byte, max int) string {
	if len(body) == 0 {
		return "(empty body)"
	}
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "..."
}

type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

const (
	CodeDeviceAuthorizationPending  = 42801
	CodeDeviceAuthorizationSlowDown = 42902
	CodeDeviceAuthorizationExpired  = 40114
	CodeDeviceAuthorizationDenied   = 40313
)

type DeviceAuthStartRequest struct {
	ClientName string `json:"client_name"`
	Hostname   string `json:"hostname,omitempty"`
	CLIVersion string `json:"cli_version,omitempty"`
}

type DeviceAuthStartResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// ProfileResponse 开发者个人信息
type ProfileResponse struct {
	UserID      int64  `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

// Publisher 出版者信息
type Publisher struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}

// App 应用信息
type App struct {
	AppID       string `json:"app_id"`
	PublisherID int64  `json:"publisher_id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Summary     string `json:"summary"`
	Status      string `json:"status"`
}

// CreateAppRequest 创建应用请求。
//
// metadata 字段（Summary/Description/Category/Tags/PricingType）来自 publish 流程
// 的 fallback chain 后的 AppSpec：作者填的或 LLM 建议被采纳后的中文值。
// 不传时 hub 端写空，store 详情页将依赖后端 AI metadata 兜底。
type CreateAppRequest struct {
	PublisherID int64    `json:"publisher_id"`
	AppID       string   `json:"app_id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	PricingType string   `json:"pricing_type,omitempty"`
}

// UpdateAppRequest 更新应用 metadata（PUT /v1/developer/apps/:app_id）。
//
// 字段语义与 CreateAppRequest 的 metadata 子集一致；hub 端为 PUT 全量替换：
// 空字段会清空后端对应字段。publish 流程在 fallback chain 后调，传值都已经过 LLM 兜底。
//
// Name 必填（hub 端 binding:"required"）。
type UpdateAppRequest struct {
	Name        string   `json:"name"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	PricingType string   `json:"pricing_type,omitempty"`
}

// UploadVersionRequest 上传版本请求
type UploadVersionRequest struct {
	AppID          string
	Version        string
	TarballPath    string
	Manifest       []byte
	Permissions    []byte // 权限声明 JSON，对应 manifest.permissions
	InstallSpec    []byte // install.yaml 内容（可选，JSON 序列化的 InstallSpec）
	Changelog      string
	CompatKeystone string
}

// Version 版本信息（扩展自 Y3，加 review_path / submitted_at / built_at /
// available / ksp_sha256 / ksp_size_bytes / review_reason 等字段）。
type Version struct {
	Version        string `json:"version"`
	Status         string `json:"status"`
	ReviewPath     string `json:"review_path,omitempty"` // "fast-track" | "manual"
	SubmittedAt    string `json:"submitted_at,omitempty"`
	ReviewedAt     string `json:"reviewed_at,omitempty"`
	BuiltAt        string `json:"built_at,omitempty"`
	Available      bool   `json:"available"`
	KSPSha256      string `json:"ksp_sha256,omitempty"`
	KSPSizeBytes   int64  `json:"ksp_size_bytes,omitempty"`
	ReviewReason   string `json:"review_reason,omitempty"`
	CompatKeystone string `json:"compat_keystone,omitempty"`
	Changelog      string `json:"changelog,omitempty"`
	PublishedAt    string `json:"published_at,omitempty"`
}

// WhoamiResponse 由 GET /v1/developer/auth/whoami 返回，PAT/user 字段不对称。
type WhoamiResponse struct {
	AuthType      string   `json:"auth_type"`
	PublisherSlug string   `json:"publisher_slug,omitempty"`
	PublisherID   int64    `json:"publisher_id,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	TokenID       int64    `json:"token_id,omitempty"`
	TokenName     string   `json:"token_name,omitempty"`
	ExpiresAt     string   `json:"expires_at,omitempty"`
	UserID        int64    `json:"user_id,omitempty"`
	Email         string   `json:"email,omitempty"`
	DisplayName   string   `json:"display_name,omitempty"`
}

// SubmitResponse 由 POST .../versions/:v/submit 返回。
type SubmitResponse struct {
	ReviewID   int64  `json:"review_id"`
	ReviewPath string `json:"review_path"` // "fast-track" | "manual"
}

// ListVersionsPage 包含分页元数据的 list versions 响应。
// 服务端统一用 ListResult 信封：data.items 承载数组（见 ListApps/ListPublishers）。
type ListVersionsPage struct {
	Items []Version `json:"items"`
	Total int       `json:"total"`
}

func (c *Client) Login(email, password string) (*LoginResponse, error) {
	resp, err := c.do("POST", "/v1/developer/auth/login", map[string]string{
		"email": email, "password": password,
	})
	if err != nil {
		return nil, err
	}
	var result LoginResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) StartDeviceAuth(req DeviceAuthStartRequest) (*DeviceAuthStartResponse, error) {
	resp, err := c.do("POST", "/v1/developer/auth/device/start", req)
	if err != nil {
		return nil, err
	}
	var result DeviceAuthStartResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) PollDeviceToken(deviceCode string) (*LoginResponse, error) {
	resp, err := c.do("POST", "/v1/developer/auth/device/token", map[string]string{
		"device_code": deviceCode,
	})
	if err != nil {
		return nil, err
	}
	var result LoginResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Register(email, password, displayName string) (*LoginResponse, error) {
	resp, err := c.do("POST", "/v1/developer/auth/register", map[string]string{
		"email": email, "password": password, "display_name": displayName,
	})
	if err != nil {
		return nil, err
	}
	var result LoginResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetProfile() (*ProfileResponse, error) {
	resp, err := c.do("GET", "/v1/developer/profile", nil)
	if err != nil {
		return nil, err
	}
	var result ProfileResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetPublisher(slug string) (*Publisher, error) {
	resp, err := c.do("GET", "/v1/developer/publishers/"+slug, nil)
	if err != nil {
		return nil, err
	}
	var result Publisher
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) CreateApp(req *CreateAppRequest) (*App, error) {
	resp, err := c.do("POST", "/v1/developer/apps", req)
	if err != nil {
		return nil, err
	}
	var result App
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateApp 更新应用 metadata。错误语义与 CreateApp 一致；
// publish 流程允许 UpdateApp 失败时仅 warn，不中断 version 上传。
func (c *Client) UpdateApp(appID string, req *UpdateAppRequest) (*App, error) {
	resp, err := c.do("PUT", "/v1/developer/apps/"+appID, req)
	if err != nil {
		return nil, err
	}
	var result App
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetApp(appID string) (*App, error) {
	resp, err := c.do("GET", "/v1/developer/apps/"+appID, nil)
	if err != nil {
		return nil, err
	}
	var result App
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SubmitVersion 提交版本审核。返回 review_id 和 review_path（fast-track | manual）。
func (c *Client) SubmitVersion(appID, version string) (*SubmitResponse, error) {
	resp, err := c.do("POST", "/v1/developer/apps/"+appID+"/versions/"+version+"/submit", nil)
	if err != nil {
		return nil, err
	}
	var result SubmitResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Whoami 调用 GET /v1/developer/auth/whoami，对当前 token 自检。
func (c *Client) Whoami() (*WhoamiResponse, error) {
	resp, err := c.do("GET", "/v1/developer/auth/whoami", nil)
	if err != nil {
		return nil, err
	}
	var result WhoamiResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetVersion 查询单个版本详情。
func (c *Client) GetVersion(appID, version string) (*Version, error) {
	resp, err := c.do("GET", "/v1/developer/apps/"+appID+"/versions/"+version, nil)
	if err != nil {
		return nil, err
	}
	var result Version
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListVersionsPaged 分页列出版本（与 ListVersions 同 endpoint，但带分页查询参数）。
func (c *Client) ListVersionsPaged(appID string, limit, offset int) (*ListVersionsPage, error) {
	path := fmt.Sprintf("/v1/developer/apps/%s/versions?limit=%d&offset=%d", appID, limit, offset)
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var page ListVersionsPage
	if err := json.Unmarshal(resp.Data, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (c *Client) CreatePublisher(slug, displayName string) (*Publisher, error) {
	resp, err := c.do("POST", "/v1/developer/publishers", map[string]string{
		"slug": slug, "display_name": displayName,
	})
	if err != nil {
		return nil, err
	}
	var result Publisher
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ListPublishers() ([]Publisher, error) {
	resp, err := c.do("GET", "/v1/developer/publishers", nil)
	if err != nil {
		return nil, err
	}
	var page struct {
		Items []Publisher `json:"items"`
	}
	if err := json.Unmarshal(resp.Data, &page); err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (c *Client) ListApps(publisherID string) ([]App, error) {
	path := "/v1/developer/apps"
	if publisherID != "" {
		path += "?publisher_id=" + publisherID
	}
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var page struct {
		Items []App `json:"items"`
	}
	if err := json.Unmarshal(resp.Data, &page); err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (c *Client) ListVersions(appID string) ([]Version, error) {
	resp, err := c.do("GET", "/v1/developer/apps/"+appID+"/versions", nil)
	if err != nil {
		return nil, err
	}
	var page struct {
		Items []Version `json:"items"`
	}
	if err := json.Unmarshal(resp.Data, &page); err != nil {
		return nil, err
	}
	return page.Items, nil
}

// UploadVersion 以 multipart/form-data 上传新版本到 Hub
func (c *Client) UploadVersion(req *UploadVersionRequest) error {
	file, err := os.Open(req.TarballPath)
	if err != nil {
		return fmt.Errorf("打开 tarball 失败: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("tarball", filepath.Base(req.TarballPath))
	if err != nil {
		return fmt.Errorf("创建 multipart form 失败: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("写入 tarball 失败: %w", err)
	}

	_ = writer.WriteField("manifest", string(req.Manifest))
	_ = writer.WriteField("version", req.Version)
	if len(req.Permissions) > 0 {
		_ = writer.WriteField("permissions", string(req.Permissions))
	}
	if len(req.InstallSpec) > 0 {
		_ = writer.WriteField("install_spec", string(req.InstallSpec))
	}
	if req.Changelog != "" {
		_ = writer.WriteField("changelog", req.Changelog)
	}
	if req.CompatKeystone != "" {
		_ = writer.WriteField("compat_keystone", req.CompatKeystone)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("关闭 multipart writer 失败: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/v1/developer/apps/"+req.AppID+"/versions", body)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	var apiResp APIResponse
	if jsonErr := json.Unmarshal(respBody, &apiResp); jsonErr != nil {
		return &HTTPError{StatusCode: resp.StatusCode, Body: truncateBody(respBody, 200)}
	}
	if apiResp.Code != 0 {
		return &APIError{Code: apiResp.Code, Message: apiResp.Message}
	}
	return nil
}

// ============================================================================
// devkit manifest LLM 辅助端点（v0.6.0+）
//
// 服务于 ks publish 的 fallback chain：作者 manifest 缺字段时，CLI 调
// /v1/developer/devkit/manifest/suggest 拿 LLM 建议；缺 changelog 时如本地
// CHANGELOG.md 抽取失败则调 /v1/developer/devkit/changelog/parse 兜底。
// ============================================================================

// SuggestManifestRequest 调 LLM 给 manifest 建议的入参。
//
//	AppID:           应用 id（用于 prompt 上下文，比如同 publisher 已有 skill 的 tags 风格）
//	SkillMdText:     SKILL.md / README 内容；后端用作 LLM 主输入
//	CurrentManifest: 当前 manifest 完整 map（已填字段作 prompt 参考，避免 LLM 重写）
//	MissingFields:   要 LLM 帮忙的字段列表，如 ["summary","tags","category"]
type SuggestManifestRequest struct {
	AppID           string         `json:"app_id"`
	SkillMdText     string         `json:"skill_md_text"`
	CurrentManifest map[string]any `json:"current_manifest,omitempty"`
	MissingFields   []string       `json:"missing_fields"`
}

// ManifestSuggestions 是 LLM 返回的字段级建议，键缺失表示 LLM 没建议（按 missing_fields 子集返）。
type ManifestSuggestions struct {
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// SuggestManifestResponse 含 LLM 建议 + 可观测字段（confidence / model / prompt_version）。
// CLI 展示 confidence 帮作者判断是否采纳，model / prompt_version 仅日志用。
type SuggestManifestResponse struct {
	Suggestions   ManifestSuggestions `json:"suggestions"`
	Rationale     string              `json:"rationale"`
	Confidence    float64             `json:"confidence"`
	LLMModel      string              `json:"llm_model"`
	PromptVersion string              `json:"prompt_version"`
}

// SuggestManifest 调 hub LLM suggest 端点拿 manifest 字段建议。
//
// 错误语义：
//   - 503 / 5xx → HTTPError；调用方走 inline editor，不重试
//   - 业务码 != 0 → APIError；调用方走 inline editor
//   - 网络错 → 直接返回；调用方走 inline editor
//
// 不接 ctx：与 hub.Client 现有方法风格保持一致；上层超时由 c.httpClient.Timeout (30s) 兜底。
func (c *Client) SuggestManifest(req SuggestManifestRequest) (*SuggestManifestResponse, error) {
	resp, err := c.do("POST", "/v1/developer/devkit/manifest/suggest", req)
	if err != nil {
		return nil, err
	}
	var out SuggestManifestResponse
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return nil, fmt.Errorf("解析 suggest 响应失败: %w", err)
	}
	return &out, nil
}

// ParseChangelogRequest 入参：CLI 把本地 CHANGELOG.md 全文 + 目标 version 发给 hub
// 由 hub 端用比 CLI 更宽容的解析规则尝试抽取 version section。
type ParseChangelogRequest struct {
	AppID           string `json:"app_id"`
	Version         string `json:"version"`
	ChangelogMDText string `json:"changelog_md_text"`
}

// ParseChangelogResponse 是 hub 解析结果。
//
//	Parsed.Found:          是否成功定位到目标 version
//	Parsed.VersionSection: 提取出的该版本 markdown section（不含 heading 行）
type ParseChangelogResponse struct {
	Parsed struct {
		Found          bool   `json:"found"`
		VersionSection string `json:"version_section,omitempty"`
	} `json:"parsed"`
}

// ParseChangelog 调 hub changelog parse 端点。语义同 SuggestManifest。
func (c *Client) ParseChangelog(req ParseChangelogRequest) (*ParseChangelogResponse, error) {
	resp, err := c.do("POST", "/v1/developer/devkit/changelog/parse", req)
	if err != nil {
		return nil, err
	}
	var out ParseChangelogResponse
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return nil, fmt.Errorf("解析 changelog 响应失败: %w", err)
	}
	return &out, nil
}
