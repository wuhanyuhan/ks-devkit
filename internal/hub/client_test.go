package hub

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogin_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/auth/login" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"access_token":"abc","refresh_token":"def","expires_in":3600}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	resp, err := c.Login("a@b.c", "pw")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AccessToken != "abc" || resp.RefreshToken != "def" || resp.ExpiresIn != 3600 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestLogin_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":40001,"message":"invalid credentials"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	_, err := c.Login("a@b.c", "wrong")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "40001") {
		t.Errorf("expected error to contain '40001', got: %v", err)
	}
}

func TestLogin_NonJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	_, err := client.Login("a@b.c", "pw")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("expected HTTP 502 in error, got: %v", err)
	}
}

func TestClient_Authorization(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"access_token":"x","refresh_token":"y","expires_in":1}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token123")
	if _, err := c.Login("a@b.c", "pw"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer token123" {
		t.Errorf("expected 'Bearer token123', got %q", gotAuth)
	}
}

func TestGetProfile_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/profile" || r.Method != "GET" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"id":1,"email":"a@b.c","display_name":"Test"}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	p, err := c.GetProfile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Email != "a@b.c" || p.DisplayName != "Test" || p.UserID != 1 {
		t.Errorf("unexpected profile: %+v", p)
	}
}

func TestGetPublisher_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/publishers/my-team" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"id":10,"slug":"my-team","display_name":"My Team"}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	pub, err := c.GetPublisher("my-team")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.ID != 10 || pub.Slug != "my-team" {
		t.Errorf("unexpected publisher: %+v", pub)
	}
}

func TestGetApp_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/apps/my-app" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"app_id":"my-app","name":"My App","type":"service"}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	app, err := c.GetApp("my-app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app.AppID != "my-app" {
		t.Errorf("unexpected app: %+v", app)
	}
}

func TestGetApp_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":40401,"message":"app not found"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	_, err := c.GetApp("missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFound(err) {
		t.Errorf("期望 IsNotFound 返回 true，实际错误: %v", err)
	}
}

func TestCreateApp_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/apps" || r.Method != "POST" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"app_id":"new-app","name":"New App","type":"service","publisher_id":10}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	app, err := c.CreateApp(&CreateAppRequest{
		PublisherID: 10,
		AppID:       "new-app",
		Name:        "New App",
		Type:        "service",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app.AppID != "new-app" || app.PublisherID != 10 {
		t.Errorf("unexpected app: %+v", app)
	}
}

func TestUpdateApp_Success(t *testing.T) {
	// 守护 publish 链路 step 6 同步 app metadata 的客户端契约：
	// PUT /v1/developer/apps/:app_id，body 含 name + 完整 metadata（含 LocalizedString 取出的 zh-CN 值）。
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/v1/developer/apps/test-skill" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"app_id":"test-skill","name":"测试技能","summary":"摘要","category":"开发流程"}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	app, err := c.UpdateApp("test-skill", &UpdateAppRequest{
		Name:        "测试技能",
		Summary:     "摘要",
		Description: "描述",
		Category:    "开发流程",
		Tags:        []string{"a", "b", "c"},
		PricingType: "free",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app.AppID != "test-skill" || app.Summary != "摘要" {
		t.Errorf("unexpected app: %+v", app)
	}

	// 验证发送的 body 含全部 metadata 字段（防止有人改回只传 Name）
	bodyStr := string(capturedBody)
	for _, want := range []string{`"name":"测试技能"`, `"summary":"摘要"`, `"description":"描述"`, `"category":"开发流程"`, `"tags":["a","b","c"]`, `"pricing_type":"free"`} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("body 缺字段 %s: %s", want, bodyStr)
		}
	}
}

func TestUploadVersion_Success(t *testing.T) {
	expectedManifest := `{"id":"test-app","name":"Test App","version":"1.0.0"}`
	expectedPermissions := `{"network":{"level":"restricted"}}`
	expectedTarball := []byte("fake-tarball-bytes")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/v1/developer/apps/test-app/versions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}

		if got := r.FormValue("manifest"); got != expectedManifest {
			t.Errorf("manifest mismatch: %q", got)
		}
		if got := r.FormValue("version"); got != "1.0.0" {
			t.Errorf("version mismatch: %q", got)
		}
		if got := r.FormValue("permissions"); got != expectedPermissions {
			t.Errorf("permissions mismatch: %q", got)
		}
		if got := r.FormValue("changelog"); got != "初始版本" {
			t.Errorf("changelog mismatch: %q", got)
		}
		if got := r.FormValue("compat_keystone"); got != ">=1.0.0" {
			t.Errorf("compat_keystone mismatch: %q", got)
		}

		fileHeaders := r.MultipartForm.File["tarball"]
		if len(fileHeaders) != 1 {
			t.Fatalf("expected 1 tarball file, got %d", len(fileHeaders))
		}
		f, _ := fileHeaders[0].Open()
		defer f.Close()
		data, _ := io.ReadAll(f)
		if string(data) != string(expectedTarball) {
			t.Errorf("tarball mismatch")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok"}`)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	tarballPath := filepath.Join(tmpDir, "test-app-1.0.0.tar.gz")
	_ = os.WriteFile(tarballPath, expectedTarball, 0644)

	c := NewClient(srv.URL, "test-token")
	err := c.UploadVersion(&UploadVersionRequest{
		AppID:          "test-app",
		Version:        "1.0.0",
		TarballPath:    tarballPath,
		Manifest:       []byte(expectedManifest),
		Permissions:    []byte(expectedPermissions),
		Changelog:      "初始版本",
		CompatKeystone: ">=1.0.0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUploadVersion_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":40001,"message":"app not found"}`)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	tarballPath := filepath.Join(tmpDir, "test-app-1.0.0.tar.gz")
	_ = os.WriteFile(tarballPath, []byte("data"), 0644)

	c := NewClient(srv.URL, "test-token")
	err := c.UploadVersion(&UploadVersionRequest{
		AppID:       "test-app",
		Version:     "1.0.0",
		TarballPath: tarballPath,
		Manifest:    []byte(`{"id":"test-app"}`),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "app not found") {
		t.Errorf("expected error to contain 'app not found', got: %v", err)
	}
}

func TestSubmitVersion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/apps/my-app/versions/1.0.0/submit" || r.Method != "POST" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"review_id":1,"review_path":"manual"}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	if _, err := c.SubmitVersion("my-app", "1.0.0"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePublisher_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/publishers" || r.Method != "POST" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"id":1,"slug":"my-team","display_name":"My Team"}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	pub, err := c.CreatePublisher("my-team", "My Team")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.Slug != "my-team" || pub.ID != 1 {
		t.Errorf("unexpected publisher: %+v", pub)
	}
}

func TestListPublishers_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/publishers" || r.Method != "GET" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"items":[{"id":1,"slug":"team-a","display_name":"Team A"},{"id":2,"slug":"team-b","display_name":"Team B"}]}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	pubs, err := c.ListPublishers()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pubs) != 2 {
		t.Fatalf("expected 2 publishers, got %d", len(pubs))
	}
	if pubs[0].Slug != "team-a" {
		t.Errorf("unexpected publisher: %+v", pubs[0])
	}
}

func TestListApps_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/apps" || r.Method != "GET" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("publisher_id") != "1" {
			t.Errorf("unexpected publisher_id: %q", r.URL.Query().Get("publisher_id"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"items":[{"app_id":"app1","name":"App 1","type":"service"}],"total":1,"page":1,"size":20}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	apps, err := c.ListApps("1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 1 || apps[0].AppID != "app1" {
		t.Errorf("unexpected apps: %+v", apps)
	}
}

func TestListVersions_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/apps/my-app/versions" || r.Method != "GET" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"items":[{"version":"1.0.0","status":"published"},{"version":"1.1.0","status":"draft"}]}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	versions, err := c.ListVersions("my-app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 2 || versions[0].Version != "1.0.0" {
		t.Errorf("unexpected versions: %+v", versions)
	}
}

func TestIsUnauthorizedTrue(t *testing.T) {
	err := &APIError{Code: 40101, Message: "token revoked"}
	if !IsUnauthorized(err) {
		t.Fatal("IsUnauthorized should be true for 401xx")
	}
}

func TestIsUnauthorizedFalse(t *testing.T) {
	err := &APIError{Code: 40404, Message: "not found"}
	if IsUnauthorized(err) {
		t.Fatal("IsUnauthorized should be false for 404xx")
	}
	if IsUnauthorized(errors.New("plain")) {
		t.Fatal("plain error should not be unauthorized")
	}
	if IsUnauthorized(nil) {
		t.Fatal("nil should not be unauthorized")
	}
}

func TestIsForbidden(t *testing.T) {
	err := &APIError{Code: 40301, Message: "scope required"}
	if !IsForbidden(err) {
		t.Fatal("IsForbidden should be true for 403xx")
	}
}

func TestIsConflict(t *testing.T) {
	err := &APIError{Code: 40901, Message: "version exists"}
	if !IsConflict(err) {
		t.Fatal("IsConflict should be true for 409xx")
	}
}

func TestHTTPErrorIsServerError(t *testing.T) {
	e := &HTTPError{StatusCode: 503, Body: "gateway"}
	if !IsServerError(e) {
		t.Fatal("503 should be server error")
	}
	if e.Error() == "" {
		t.Fatal("Error() should not be empty")
	}
}

func TestIsServerErrorFalseForAPIError(t *testing.T) {
	if IsServerError(&APIError{Code: 40101}) {
		t.Fatal("APIError should not be classified as server error")
	}
}

func TestWhoamiPAT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/auth/whoami" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ksh_pat_test" {
			t.Fatalf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"auth_type":"pat","publisher_slug":"keystone-official","publisher_id":1,"scopes":["publish:apps"],"token_id":42,"token_name":"ci","expires_at":"2026-08-01T00:00:00Z"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "ksh_pat_test")
	w, err := c.Whoami()
	if err != nil {
		t.Fatal(err)
	}
	if w.AuthType != "pat" || w.PublisherSlug != "keystone-official" {
		t.Fatalf("unexpected whoami: %+v", w)
	}
	if len(w.Scopes) != 1 || w.Scopes[0] != "publish:apps" {
		t.Fatalf("scopes: %+v", w.Scopes)
	}
}

func TestGetVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/apps/ks-mcp-email/versions/v0.5.2" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"version":"v0.5.2","status":"approved","review_path":"fast-track","submitted_at":"2026-05-03T10:00:00Z","reviewed_at":"2026-05-03T10:00:12Z","built_at":"2026-05-03T10:01:48Z","available":true,"ksp_sha256":"abcd","ksp_size_bytes":24300000,"changelog":"fix typo"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	v, err := c.GetVersion("ks-mcp-email", "v0.5.2")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "approved" || v.ReviewPath != "fast-track" {
		t.Fatalf("got %+v", v)
	}
	if v.Available != true || v.KSPSha256 != "abcd" {
		t.Fatalf("got %+v", v)
	}
}

func TestListVersionsPagedQueryParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "10" || r.URL.Query().Get("offset") != "20" {
			t.Fatalf("query = %v", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"version":"v0.5.2","status":"approved","review_path":"fast-track"}],"total":1}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	page, err := c.ListVersionsPaged("ks-mcp-email", 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
}

func TestSubmitVersionReturnsReviewPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/apps/ks-mcp-email/versions/v0.5.2/submit" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"review_id":42,"review_path":"fast-track"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	resp, err := c.SubmitVersion("ks-mcp-email", "v0.5.2")
	if err != nil {
		t.Fatal(err)
	}
	if resp.ReviewID != 42 || resp.ReviewPath != "fast-track" {
		t.Fatalf("submit resp = %+v", resp)
	}
}
