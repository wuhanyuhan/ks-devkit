package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/cmd/exitcode"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

func TestVerifyPublisherMatchPATEqual(t *testing.T) {
	cred := &auth.Credentials{
		AuthType:      auth.AuthTypePAT,
		PublisherSlug: "keystone-official",
	}
	if err := verifyPublisherMatch(cred, "keystone-official"); err != nil {
		t.Fatalf("expected nil; got %v", err)
	}
}

func TestVerifyPublisherMatchPATMismatch(t *testing.T) {
	cred := &auth.Credentials{
		AuthType:      auth.AuthTypePAT,
		PublisherSlug: "keystone-official",
	}
	err := verifyPublisherMatch(cred, "ks-mcp-other")
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if exitcode.Extract(err) != exitcode.AuthOrPermission {
		t.Fatalf("exit code = %d, want %d", exitcode.Extract(err), exitcode.AuthOrPermission)
	}
}

func TestVerifyPublisherMatchUserSkipped(t *testing.T) {
	cred := &auth.Credentials{
		AuthType:    auth.AuthTypeUser,
		AccessToken: "eyJ",
	}
	// user JWT 模式：跳过 client-side 校验，由 hub 端 RequireMember 兜底
	if err := verifyPublisherMatch(cred, "any-publisher"); err != nil {
		t.Fatalf("user mode should skip; got %v", err)
	}
}

func TestVerifyPublisherMatchPATEmptySlugSkipped(t *testing.T) {
	// env 模式下 PublisherSlug 可能未填（首次 LoadFromEnvOrFile 时）；
	// 此时 verifyPublisherMatch 应跳过（让 publish 流程稍后调 whoami 填充并重新校验）。
	cred := &auth.Credentials{
		AuthType:    auth.AuthTypePAT,
		AccessToken: "ksh_pat_xxx",
	}
	if err := verifyPublisherMatch(cred, "anything"); err != nil {
		t.Fatalf("empty slug should skip; got %v", err)
	}
}

func TestMapHubErrorToExit(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"unauthorized → 2", &hub.APIError{Code: 40101, Message: "token revoked"}, exitcode.AuthOrPermission},
		{"forbidden → 2", &hub.APIError{Code: 40301, Message: "scope required"}, exitcode.AuthOrPermission},
		{"conflict → 5", &hub.APIError{Code: 40901, Message: "version exists"}, exitcode.DuplicateVersion},
		{"500 HTTPError → 4", &hub.HTTPError{StatusCode: 503, Body: "gateway"}, exitcode.Network},
		{"plain → 1", errors.New("???"), exitcode.Generic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := mapHubErrorToExit(tc.err)
			if got := exitcode.Extract(wrapped); got != tc.want {
				t.Errorf("got exit %d, want %d", got, tc.want)
			}
		})
	}
}

func TestWaitForReviewFastTrackApproved(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if n < 3 {
			_, _ = w.Write([]byte(`{"code":0,"data":{"version":"v0.5.2","status":"pending"}}`))
		} else {
			_, _ = w.Write([]byte(`{"code":0,"data":{"version":"v0.5.2","status":"approved","review_path":"fast-track"}}`))
		}
	}))
	defer srv.Close()

	c := hub.NewClient(srv.URL, "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// pollDelays 短一点用于测试，避免 60s 真实等待
	v, err := waitForReview(ctx, c, "ks-mcp-email", "v0.5.2", []time.Duration{
		10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "approved" {
		t.Fatalf("status = %q", v.Status)
	}
}

func TestWaitForReviewRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"version":"v0.5.2","status":"rejected","review_path":"fast-track","review_reason":"manifest schema invalid"}}`))
	}))
	defer srv.Close()

	c := hub.NewClient(srv.URL, "tok")
	ctx := context.Background()
	v, err := waitForReview(ctx, c, "ks-mcp-email", "v0.5.2", []time.Duration{10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "rejected" {
		t.Fatalf("status = %q", v.Status)
	}
}

func TestWaitForReviewTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"version":"v0.5.2","status":"pending"}}`))
	}))
	defer srv.Close()

	c := hub.NewClient(srv.URL, "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	v, err := waitForReview(ctx, c, "ks-mcp-email", "v0.5.2", []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond})
	// 超时返回 last seen + nil err（调用方判 timeout 自己处理）
	if err == nil && v != nil && v.Status == "pending" {
		// OK：合规 timeout，状态仍是 pending
	} else if err != context.DeadlineExceeded {
		t.Fatalf("unexpected: v=%+v err=%v", v, err)
	}
}

func TestPublishDryRunDoesNotUpload(t *testing.T) {
	// 基本契约：dry-run 退出 0、不调任何 hub endpoint
	// 用一个 httptest 起接口，验证流程没触发 publish 上传
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("dry-run should not call hub; got path=%q", r.URL.Path)
	}))
	defer srv.Close()

	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "manifest.yaml"), strings.Join([]string{
		"id: test-app",
		"version: 0.1.0",
		"name: t",
		"type: skill",
		"summary: x",
		"publisher: keystone-official",
		"store:",
		"  presentation: method_skill",
		"  highlights: [测试驱动]",
		"  try_prompts: [帮我写测试]",
		"",
	}, "\n"))

	t.Setenv("KS_HUB_TOKEN", "ksh_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("KS_HUB_URL", srv.URL) // config.Load 优先读 env（确认现有 config 实现支持）
	// 切到项目目录跑
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(tmp)

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"publish", "--dry-run"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("dry-run should succeed; err=%v\noutput=%s", err, buf.String())
	}
}

func TestPublishStoreQualityBlocksBeforeUpload(t *testing.T) {
	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		t.Fatalf("store quality failure should not call hub; got path=%q", r.URL.Path)
	}))
	defer srv.Close()

	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "manifest.yaml"), strings.Join([]string{
		"id: skill-tdd",
		"version: 0.1.0",
		"name: TDD 技能",
		"type: skill",
		"summary: TDD",
		"description: TDD flow",
		"publisher: keystone-official",
		"category: 开发流程",
		"tags: [tdd]",
		"store:",
		"  presentation: method_skill",
		"  try_prompts:",
		"    - 帮我用 TDD 开发这个功能",
		"",
	}, "\n"))

	t.Setenv("KS_HUB_TOKEN", "ksh_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("KS_HUB_URL", srv.URL)
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(tmp)

	rootCmd.SetArgs([]string{"publish", "--dry-run=false", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("publish should fail when store.highlights is missing")
	}
	if exitcode.Extract(err) != exitcode.ClientConfig {
		t.Fatalf("exit code = %d, want %d; err=%v", exitcode.Extract(err), exitcode.ClientConfig, err)
	}
	if called.Load() {
		t.Fatal("hub should not be called")
	}
}

func TestPublishDryRunStoreQualityBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("dry-run store quality failure should not call hub; got path=%q", r.URL.Path)
	}))
	defer srv.Close()

	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "manifest.yaml"), strings.Join([]string{
		"id: skill-tdd",
		"version: 0.1.0",
		"name: TDD 技能",
		"type: skill",
		"summary: TDD",
		"description: TDD flow",
		"publisher: keystone-official",
		"category: 开发流程",
		"tags: [tdd]",
		"store:",
		"  presentation: method_skill",
		"  try_prompts:",
		"    - 帮我用 TDD 开发这个功能",
		"",
	}, "\n"))

	t.Setenv("KS_HUB_TOKEN", "ksh_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("KS_HUB_URL", srv.URL)
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(tmp)

	rootCmd.SetArgs([]string{"publish", "--dry-run"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("dry-run should fail when store.highlights is missing")
	}
	if exitcode.Extract(err) != exitcode.ClientConfig {
		t.Fatalf("exit code = %d, want %d; err=%v", exitcode.Extract(err), exitcode.ClientConfig, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// captureStdout 把 os.Stdout 重定向到 buf，跑 fn，恢复并返回 buf。
// 用于测试 emitJSONEvent / dry-run JSON 输出（这些走 fmt.Println 直接到 os.Stdout）。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()
	w.Close()
	return <-done
}

func TestEmitJSONEventOutputsNDJSON(t *testing.T) {
	out := captureStdout(t, func() {
		emitJSONEvent("submit_done", map[string]any{
			"app_id":      "ks-mcp-email",
			"version":     "v0.5.2",
			"review_path": "fast-track",
		})
	})
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("expected trailing newline; got %q", out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if got["event"] != "submit_done" || got["review_path"] != "fast-track" {
		t.Fatalf("got %+v", got)
	}
}

func TestPublishDryRunJSONLastLineHasReviewPath(t *testing.T) {
	// 契约：reusable workflow 用 `tail -1 publish.out | jq -r '.review_path'` 提取，
	// 因此 --json 模式下最后一行必须含 review_path 字段。dry-run 路径也要满足。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("dry-run should not call hub; got path=%q", r.URL.Path)
	}))
	defer srv.Close()

	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "manifest.yaml"), strings.Join([]string{
		"id: test-app",
		"version: 0.1.0",
		"name: t",
		"type: skill",
		"summary: x",
		"publisher: keystone-official",
		"store:",
		"  presentation: method_skill",
		"  highlights: [测试驱动]",
		"  try_prompts: [帮我写测试]",
		"",
	}, "\n"))

	t.Setenv("KS_HUB_TOKEN", "ksh_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("KS_HUB_URL", srv.URL)
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(tmp)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"publish", "--dry-run", "--json"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("no output")
	}
	last := lines[len(lines)-1]
	var ev map[string]any
	if err := json.Unmarshal([]byte(last), &ev); err != nil {
		t.Fatalf("last line not JSON: %v\nlast=%q\nfull:\n%s", err, last, out)
	}
	if _, ok := ev["review_path"]; !ok {
		t.Fatalf("last line missing review_path field; got %+v", ev)
	}
	if ev["event"] != "dry_run_done" {
		t.Fatalf("expected event=dry_run_done; got %v", ev["event"])
	}
	manifest, ok := ev["manifest"].(map[string]any)
	if !ok {
		t.Fatalf("dry-run JSON missing manifest: %+v", ev)
	}
	store, ok := manifest["store"].(map[string]any)
	if !ok {
		t.Fatalf("dry-run JSON missing manifest.store: %+v", manifest)
	}
	if store["presentation"] != "method_skill" {
		t.Fatalf("store.presentation = %v", store["presentation"])
	}
}
