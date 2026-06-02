package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/buildinfo"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

func TestLoginWithTokenWritesCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/auth/whoami" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ksh_pat_validvalidvalidvalidvalidvalid12" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"auth_type":"pat","publisher_slug":"keystone-official","scopes":["publish:apps"]}}`))
	}))
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := loginWithToken(srv.URL, "ksh_pat_validvalidvalidvalidvalidvalid12"); err != nil {
		t.Fatal(err)
	}

	credPath := filepath.Join(tmpHome, ".ks", "credentials.json")
	data, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatal(err)
	}
	var c auth.Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if c.AuthType != auth.AuthTypePAT {
		t.Fatalf("AuthType = %q", c.AuthType)
	}
	if c.PublisherSlug != "keystone-official" {
		t.Fatalf("PublisherSlug = %q", c.PublisherSlug)
	}
	if len(c.Scopes) != 1 || c.Scopes[0] != "publish:apps" {
		t.Fatalf("Scopes = %v", c.Scopes)
	}
}

func TestLoginWithTokenRejectsBadFormat(t *testing.T) {
	cases := []string{
		"",
		"eyJabc",                             // 不是 PAT 前缀
		"ksh_pat_short",                      // 长度不足 40
		"ksh_pat_" + strings.Repeat("z", 40), // 长度超
	}
	for _, tk := range cases {
		err := loginWithToken("http://nowhere", tk)
		if err == nil {
			t.Fatalf("expected error for token %q", tk)
		}
	}
}

func TestLoginWithTokenWhoami401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":40101,"message":"token revoked"}`))
	}))
	defer srv.Close()

	t.Setenv("HOME", t.TempDir())
	err := loginWithToken(srv.URL, "ksh_pat_validvalidvalidvalidvalidvalid12")
	if err == nil {
		t.Fatal("expected error from 401 whoami")
	}
}

func TestLoginWithBrowserWritesUserCredentials(t *testing.T) {
	var openedURL string
	oldOpenBrowser := openBrowser
	oldSleep := sleep
	openBrowser = func(url string) error {
		openedURL = url
		return nil
	}
	sleep = func(time.Duration) {}
	t.Cleanup(func() {
		openBrowser = oldOpenBrowser
		sleep = oldSleep
	})

	pollCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/developer/auth/device/start":
			var req hub.DeviceAuthStartRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode start request: %v", err)
			}
			if req.CLIVersion != "v1.2.3" {
				t.Fatalf("cli_version = %q, want v1.2.3", req.CLIVersion)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"device_code":"dev-123","user_code":"AB12-CD34","verification_uri":"https://ks-hub.example/device?user_code=AB12-CD34","expires_in":60,"interval":0}}`))
		case "/v1/developer/auth/device/token":
			pollCount++
			if pollCount == 1 {
				_, _ = w.Write([]byte(`{"code":42801,"message":"authorization pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"access-123","refresh_token":"refresh-123","expires_in":900}}`))
		case "/v1/developer/profile":
			if got := r.Header.Get("Authorization"); got != "Bearer access-123" {
				t.Fatalf("profile authorization = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"id":42,"email":"open@yuhaninfo.cn","display_name":"Open"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	buildinfo.SetVersion("v1.2.3")
	t.Cleanup(func() { buildinfo.SetVersion("dev") })

	if err := loginWithBrowser(srv.URL); err != nil {
		t.Fatal(err)
	}
	if openedURL != "https://ks-hub.example/device?user_code=AB12-CD34" {
		t.Fatalf("openedURL = %q", openedURL)
	}
	if pollCount != 2 {
		t.Fatalf("pollCount = %d, want 2", pollCount)
	}

	credPath := filepath.Join(tmpHome, ".ks", "credentials.json")
	data, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatal(err)
	}
	var c auth.Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if c.AuthType != auth.AuthTypeUser {
		t.Fatalf("AuthType = %q", c.AuthType)
	}
	if c.AccessToken != "access-123" || c.RefreshToken != "refresh-123" {
		t.Fatalf("credentials tokens = %+v", c)
	}
	if c.Email != "open@yuhaninfo.cn" {
		t.Fatalf("Email = %q", c.Email)
	}
}
