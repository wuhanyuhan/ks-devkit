// maybeFetchKeystoneManagedEnv 单元测试。
//
// 验证：
//   - KS_APP_TOKEN 或 KS_GATEWAY_URL 任一缺失 → 跳过，不发起 HTTP
//   - 都设了 → fetch + os.Setenv 不覆盖已有值
//   - SelfClient 失败 → 不 panic，不注入
package ksapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// helper：启动 keystone mock 返回 env。fetched 记录被调用次数。
func newKSEnvServer(t *testing.T, env string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apps/self/resources", func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"code":0,"data":{"env":` + env + `}}`))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s, &calls
}

// resetKSEnvForTest 清空 KS_APP_TOKEN / KS_GATEWAY_URL 防止主机 env 污染。
func resetKSEnvForTest(t *testing.T, injectKeys ...string) {
	t.Helper()
	t.Setenv("KS_APP_TOKEN", "")
	t.Setenv("KS_GATEWAY_URL", "")
	for _, k := range injectKeys {
		// t.Setenv("", ...) 不允许，所以用单独的 Unsetenv cleanup
		_ = os.Unsetenv(k)
		t.Cleanup(func() { _ = os.Unsetenv(k) })
	}
}

// ── 跳过路径 ──────────────────────────────────────────────────

func TestMaybeFetch_SkipsWhenBothMissing(t *testing.T) {
	server, calls := newKSEnvServer(t, `{"DB_HOST":"x"}`)
	resetKSEnvForTest(t, "DB_HOST")
	_ = server // 不让用 KS_GATEWAY_URL 指向它

	maybeFetchKeystoneManagedEnv()

	if *calls != 0 {
		t.Errorf("HTTP called %d times, want 0", *calls)
	}
	if os.Getenv("DB_HOST") != "" {
		t.Errorf("DB_HOST should not be set")
	}
}

func TestMaybeFetch_SkipsWhenOnlyTokenSet(t *testing.T) {
	server, calls := newKSEnvServer(t, `{"DB_HOST":"x"}`)
	_ = server
	resetKSEnvForTest(t, "DB_HOST")
	t.Setenv("KS_APP_TOKEN", "tok")

	maybeFetchKeystoneManagedEnv()

	if *calls != 0 {
		t.Errorf("HTTP called %d times, want 0", *calls)
	}
}

func TestMaybeFetch_SkipsWhenOnlyGatewaySet(t *testing.T) {
	server, calls := newKSEnvServer(t, `{"DB_HOST":"x"}`)
	resetKSEnvForTest(t, "DB_HOST")
	t.Setenv("KS_GATEWAY_URL", server.URL)

	maybeFetchKeystoneManagedEnv()

	if *calls != 0 {
		t.Errorf("HTTP called %d times, want 0", *calls)
	}
}

// ── 注入路径 ──────────────────────────────────────────────────

func TestMaybeFetch_InjectsEnv(t *testing.T) {
	server, calls := newKSEnvServer(t, `{"KS_TEST_INJ_FOO":"foo-val","KS_TEST_INJ_BAR":"bar-val"}`)
	resetKSEnvForTest(t, "KS_TEST_INJ_FOO", "KS_TEST_INJ_BAR")
	t.Setenv("KS_APP_TOKEN", "tok")
	t.Setenv("KS_GATEWAY_URL", server.URL)

	maybeFetchKeystoneManagedEnv()

	if *calls != 1 {
		t.Errorf("HTTP called %d times, want 1", *calls)
	}
	if os.Getenv("KS_TEST_INJ_FOO") != "foo-val" {
		t.Errorf("KS_TEST_INJ_FOO not injected: %q", os.Getenv("KS_TEST_INJ_FOO"))
	}
	if os.Getenv("KS_TEST_INJ_BAR") != "bar-val" {
		t.Errorf("KS_TEST_INJ_BAR not injected: %q", os.Getenv("KS_TEST_INJ_BAR"))
	}
}

func TestMaybeFetch_DoesNotOverwriteExisting(t *testing.T) {
	server, _ := newKSEnvServer(t,
		`{"KS_TEST_INJ_HOST":"from-keystone","KS_TEST_INJ_PASS":"from-keystone"}`)
	resetKSEnvForTest(t, "KS_TEST_INJ_PASS") // 这个未设，应被注入
	t.Setenv("KS_APP_TOKEN", "tok")
	t.Setenv("KS_GATEWAY_URL", server.URL)
	t.Setenv("KS_TEST_INJ_HOST", "local-override") // 这个预设，应保留

	maybeFetchKeystoneManagedEnv()

	if got := os.Getenv("KS_TEST_INJ_HOST"); got != "local-override" {
		t.Errorf("KS_TEST_INJ_HOST=%q, want local-override (本地优先)", got)
	}
	if got := os.Getenv("KS_TEST_INJ_PASS"); got != "from-keystone" {
		t.Errorf("KS_TEST_INJ_PASS=%q, want from-keystone", got)
	}
}

// ── 失败路径 ──────────────────────────────────────────────────

func TestMaybeFetch_DoesNotPanicOnServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apps/self/resources", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resetKSEnvForTest(t, "KS_TEST_INJ_X")
	t.Setenv("KS_APP_TOKEN", "tok")
	t.Setenv("KS_GATEWAY_URL", server.URL)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic: %v", r)
		}
	}()
	maybeFetchKeystoneManagedEnv()

	if os.Getenv("KS_TEST_INJ_X") != "" {
		t.Error("不应注入任何 env 当 fetch 失败")
	}
}
