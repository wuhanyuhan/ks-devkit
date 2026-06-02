package keystoneclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultTimeout 默认 HTTP 超时。启动期一次性调用，5s 兜底足够。
const DefaultTimeout = 5 * time.Second

// selfResourcesPath Keystone 应用自查端点。
const selfResourcesPath = "/v1/apps/self/resources"

// SelfClient 应用自查客户端：用 KS_APP_TOKEN 调
// keystone GET /v1/apps/self/resources 拉本安装实例被分配的托管资源凭证。
//
// 用法::
//
//	c := keystoneclient.New(gatewayURL, appToken)
//	env, err := c.FetchEnv(ctx)
//	if err != nil {
//	    if errors.Is(err, keystoneclient.ErrFetchFailed) { /* warn */ }
//	}
type SelfClient struct {
	gatewayURL string
	appToken   string
	timeout    time.Duration
	httpClient *http.Client
}

// Option 构造期可选项。
type Option func(*SelfClient)

// WithTimeout 覆盖默认 5s 超时（应用启动期对 keystone 不响应需要快速失败）。
func WithTimeout(d time.Duration) Option {
	return func(c *SelfClient) { c.timeout = d }
}

// WithHTTPClient 注入自定义 http.Client，方便测试或共享连接池。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *SelfClient) { c.httpClient = hc }
}

// New 创建 SelfClient。gatewayURL 末尾的 / 会被 trim，避免拼接出现 //v1/...。
func New(gatewayURL, appToken string, opts ...Option) *SelfClient {
	c := &SelfClient{
		gatewayURL: strings.TrimRight(gatewayURL, "/"),
		appToken:   appToken,
		timeout:    DefaultTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: c.timeout}
	}
	return c
}

// fetchResponse 应用自查端点的响应 envelope。
type fetchResponse struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    fetchEnvelope `json:"data"`
}

// fetchEnvelope data 段。env 用 RawMessage 以便后置严格校验"必须是 object"。
type fetchEnvelope struct {
	AppID     string          `json:"app_id"`
	Version   string          `json:"version"`
	InstallID int64           `json:"install_id"`
	Env       json.RawMessage `json:"env"`
}

// FetchEnv 调 keystone /v1/apps/self/resources，返回平铺 env map（key/value 均为 string）。
//
// 失败返回 fmt.Errorf 包装的 ErrFetchFailed；调用方用 errors.Is 断言即可。
// message 中包含状态码或错误原因，便于运维定位。
func (c *SelfClient) FetchEnv(ctx context.Context) (map[string]string, error) {
	url := c.gatewayURL + selfResourcesPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrFetchFailed, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.appToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: network error: %v", ErrFetchFailed, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrFetchFailed, err)
	}

	if resp.StatusCode != http.StatusOK {
		short := body
		if len(short) > 200 {
			short = short[:200]
		}
		return nil, fmt.Errorf("%w: keystone returned status=%d body=%s",
			ErrFetchFailed, resp.StatusCode, string(short))
	}

	var payload fetchResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON response: %v", ErrFetchFailed, err)
	}

	if payload.Code != 0 {
		return nil, fmt.Errorf("%w: keystone business error code=%d message=%s",
			ErrFetchFailed, payload.Code, payload.Message)
	}

	if len(payload.Data.Env) == 0 {
		return nil, fmt.Errorf("%w: response missing data.env field", ErrFetchFailed)
	}

	// 严格解析为 map[string]any，再强转 value 为 string（os.Setenv 只吃 string）。
	var raw map[string]any
	if err := json.Unmarshal(payload.Data.Env, &raw); err != nil {
		return nil, fmt.Errorf("%w: data.env must be object: %v", ErrFetchFailed, err)
	}

	env := make(map[string]string, len(raw))
	for k, v := range raw {
		env[k] = coerceString(v)
	}
	return env, nil
}

// coerceString 把 json.Unmarshal 解出的 any 值转字符串。
// keystone 一般直接返回 string；但 schema 无强约束，且 os.Setenv 只吃 string。
//
// 类型映射：
//   - string → 原样
//   - float64（JSON number）→ 优先整数表示（3306 而非 3306.000000），含小数才走 %v
//   - bool → "true" / "false"
//   - 其他（nil / array / object，理论不出现）→ fmt.Sprint
func coerceString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%v", x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(v)
	}
}
