// SelfClient 单元测试 — Go 端 mirror Python tests/test_self_client.py。
// 用 httptest.NewServer 模拟 keystone /v1/apps/self/resources 端点。
package keystoneclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// helper：起一个 keystone mock server，handler 自行决定响应。
func newKSMock(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apps/self/resources", handler)
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// ── 正常路径 ──────────────────────────────────────────────────────

func TestFetchEnv_ReturnsDict(t *testing.T) {
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"data": {
				"app_id": "ks-mcp-writer",
				"version": "1.0.0",
				"install_id": 42,
				"env": {
					"DB_HOST": "keystone-mysql",
					"DB_PORT": "3306",
					"DB_PASSWORD": "secret",
					"HMAC_SECRET": "hex32"
				}
			}
		}`))
	})

	c := New(s.URL, "ks-app:42:1:1:abc")
	env, err := c.FetchEnv(context.Background())
	if err != nil {
		t.Fatalf("FetchEnv: %v", err)
	}
	want := map[string]string{
		"DB_HOST":     "keystone-mysql",
		"DB_PORT":     "3306",
		"DB_PASSWORD": "secret",
		"HMAC_SECRET": "hex32",
	}
	if len(env) != len(want) {
		t.Fatalf("env size: got %d, want %d", len(env), len(want))
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("env[%q]=%q, want %q", k, env[k], v)
		}
	}
}

func TestFetchEnv_SendsBearerAuthorization(t *testing.T) {
	var gotAuth string
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"code":0,"data":{"env":{}}}`))
	})

	_, err := New(s.URL, "ks-app:42:1:1:abc").FetchEnv(context.Background())
	if err != nil {
		t.Fatalf("FetchEnv: %v", err)
	}
	want := "Bearer ks-app:42:1:1:abc"
	if gotAuth != want {
		t.Errorf("Authorization=%q, want %q", gotAuth, want)
	}
}

func TestFetchEnv_StripsTrailingSlash(t *testing.T) {
	var gotPath string
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"code":0,"data":{"env":{}}}`))
	})

	_, err := New(s.URL+"/", "tok").FetchEnv(context.Background())
	if err != nil {
		t.Fatalf("FetchEnv: %v", err)
	}
	if gotPath != "/v1/apps/self/resources" {
		t.Errorf("path: %q (expected no double slash)", gotPath)
	}
}

func TestFetchEnv_EmptyEnvReturnsEmptyMap(t *testing.T) {
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"env":{}}}`))
	})

	env, err := New(s.URL, "tok").FetchEnv(context.Background())
	if err != nil {
		t.Fatalf("FetchEnv: %v", err)
	}
	if len(env) != 0 {
		t.Errorf("env should be empty, got %v", env)
	}
}

// ── 失败路径：HTTP 错误码 ─────────────────────────────────────────

func TestFetchEnv_401_ReturnsError(t *testing.T) {
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"code":40101,"message":"invalid app token"}`))
	})

	_, err := New(s.URL, "bad").FetchEnv(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrFetchFailed) {
		t.Errorf("not ErrFetchFailed: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention 401: %v", err)
	}
}

func TestFetchEnv_5xx_ReturnsError(t *testing.T) {
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`upstream down`))
	})

	_, err := New(s.URL, "tok").FetchEnv(context.Background())
	if !errors.Is(err, ErrFetchFailed) {
		t.Fatalf("not ErrFetchFailed: %v", err)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention 503: %v", err)
	}
}

func TestFetchEnv_429_ReturnsError(t *testing.T) {
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	})
	_, err := New(s.URL, "tok").FetchEnv(context.Background())
	if !errors.Is(err, ErrFetchFailed) {
		t.Fatalf("not ErrFetchFailed: %v", err)
	}
}

func TestFetchEnv_BusinessCodeNonzero_ReturnsError(t *testing.T) {
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":40004,"message":"install not found"}`))
	})

	_, err := New(s.URL, "tok").FetchEnv(context.Background())
	if !errors.Is(err, ErrFetchFailed) {
		t.Fatalf("not ErrFetchFailed: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "40004") && !strings.Contains(msg, "install not found") {
		t.Errorf("error should carry biz code/message: %v", err)
	}
}

// ── 失败路径：网络层 / 响应解析 ─────────────────────────────────

func TestFetchEnv_NetworkError_ReturnsError(t *testing.T) {
	// 启一个 listener 立刻关掉，地址就连不上
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := "http://" + ln.Addr().String()
	_ = ln.Close()

	_, err = New(addr, "tok").FetchEnv(context.Background())
	if !errors.Is(err, ErrFetchFailed) {
		t.Errorf("not ErrFetchFailed: %v", err)
	}
}

func TestFetchEnv_ContextTimeout_ReturnsError(t *testing.T) {
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"code":0,"data":{"env":{}}}`))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := New(s.URL, "tok").FetchEnv(ctx)
	if !errors.Is(err, ErrFetchFailed) {
		t.Errorf("not ErrFetchFailed: %v", err)
	}
}

func TestFetchEnv_InvalidJSON_ReturnsError(t *testing.T) {
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})

	_, err := New(s.URL, "tok").FetchEnv(context.Background())
	if !errors.Is(err, ErrFetchFailed) {
		t.Errorf("not ErrFetchFailed: %v", err)
	}
}

func TestFetchEnv_MissingEnvField_ReturnsError(t *testing.T) {
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	})

	_, err := New(s.URL, "tok").FetchEnv(context.Background())
	if !errors.Is(err, ErrFetchFailed) {
		t.Errorf("not ErrFetchFailed: %v", err)
	}
}

func TestFetchEnv_EnvNotObject_ReturnsError(t *testing.T) {
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"env":"oops"}}`))
	})

	_, err := New(s.URL, "tok").FetchEnv(context.Background())
	if !errors.Is(err, ErrFetchFailed) {
		t.Errorf("not ErrFetchFailed: %v", err)
	}
}

// ── 值类型强制转 string（与 Python 端对齐） ───────────────────

func TestFetchEnv_ValuesCoercedToString(t *testing.T) {
	s := newKSMock(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"code": 0,
			"data": {
				"env": {
					"DB_PORT": 3306,
					"DEBUG": true,
					"NAME": "x"
				}
			}
		}`))
	})

	env, err := New(s.URL, "tok").FetchEnv(context.Background())
	if err != nil {
		t.Fatalf("FetchEnv: %v", err)
	}
	if env["DB_PORT"] != "3306" {
		t.Errorf("DB_PORT=%q, want \"3306\"", env["DB_PORT"])
	}
	if env["DEBUG"] != "true" {
		t.Errorf("DEBUG=%q, want \"true\"", env["DEBUG"])
	}
	if env["NAME"] != "x" {
		t.Errorf("NAME=%q, want \"x\"", env["NAME"])
	}
}

// ── Option 配置 ────────────────────────────────────────────────────

func TestNew_WithTimeout(t *testing.T) {
	c := New("http://x", "tok", WithTimeout(7*time.Second))
	if c.timeout != 7*time.Second {
		t.Errorf("timeout=%v, want 7s", c.timeout)
	}
}

func TestNew_DefaultTimeout(t *testing.T) {
	c := New("http://x", "tok")
	if c.timeout != DefaultTimeout {
		t.Errorf("default timeout=%v, want %v", c.timeout, DefaultTimeout)
	}
}
