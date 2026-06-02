package keystoneclient_test

import (
	"context"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/conformance/managed-resources/expectations"
	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/keystoneclient"
)

// ksapp 的 SelfClient 真实 API（已 read sdk/go/ksapp/keystoneclient/self_client.go 确认）：
//
//	func New(gatewayURL, appToken string, opts ...Option) *SelfClient
//	func (c *SelfClient) FetchEnv(ctx context.Context) (map[string]string, error)
//
// FetchEnv 在网络/HTTP 非 200/JSON/code != 0/data.env 缺 都返回 error wrap 了 ErrFetchFailed。
type ksappAdapter struct {
	inner *keystoneclient.SelfClient
}

func (a *ksappAdapter) Fetch(ctx context.Context) (map[string]string, error) {
	return a.inner.FetchEnv(ctx)
}

func TestKsappSelfClient_Conformance(t *testing.T) {
	expectations.RunSuite(t, func(baseURL, token string) expectations.SelfClientUnderTest {
		return &ksappAdapter{inner: keystoneclient.New(baseURL, token)}
	})
}
