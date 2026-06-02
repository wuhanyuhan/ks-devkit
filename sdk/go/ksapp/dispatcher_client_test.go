package ksapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDispatcherClientInvokeSync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/self/invoke" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("missing bearer")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"result":{"article":"hi"},"duration_ms":123}}`))
	}))
	defer srv.Close()
	c := NewDispatcherClient(srv.URL, "test-token")
	res, err := c.Invoke(context.Background(), InvokeOptions{
		Capability: "ks.x", Args: map[string]any{"k": "v"}, Mode: "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Sync == nil {
		t.Fatal("expected sync result")
	}
	if res.Sync.Result["article"] != "hi" {
		t.Fatalf("result=%v", res.Sync.Result)
	}
}

func TestDispatcherClientInvokeAsync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"task-abc","status":"pending"}}`))
	}))
	defer srv.Close()
	c := NewDispatcherClient(srv.URL, "test-token")
	res, err := c.Invoke(context.Background(), InvokeOptions{
		Capability: "ks.x", Mode: "async",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Async == nil {
		t.Fatal("expected async result")
	}
	if res.Async.TaskID != "task-abc" {
		t.Fatalf("task_id=%s", res.Async.TaskID)
	}
}

func TestDispatcherClientInvokeMapsErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{401, `{"code":40103,"message":"aud"}`, ErrTokenAudienceMismatch},
		{403, `{"code":40300,"message":"forbidden"}`, ErrCapabilityForbidden},
		{404, `{"code":40400,"message":"capability not found"}`, ErrCapabilityNotFound},
		{408, `{"code":40800,"message":"timeout"}`, ErrTimeout},
		{429, `{"code":42900,"message":"rate"}`, ErrRateLimitError},
		{502, `{"code":50200,"message":"backend"}`, ErrBackendError},
		{503, `{"code":50300,"message":"unavail"}`, ErrCapabilityUnavailable},
		{508, `{"code":50800,"message":"loop"}`, ErrLoopDetected},
	}
	for _, c := range cases {
		t.Run(c.want.Error(), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			cli := NewDispatcherClient(srv.URL, "t")
			_, err := cli.Invoke(context.Background(), InvokeOptions{Capability: "ks.x", Mode: "sync"})
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v want %v", err, c.want)
			}
		})
	}
}

func TestDispatcherClientReportProgress(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/user-tasks/task-1/progress" {
			called = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer srv.Close()
	cli := NewDispatcherClient(srv.URL, "t")
	pct := 30
	if err := cli.ReportProgress(context.Background(), "task-1", "正在搜索", &pct); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected progress endpoint called")
	}
}

func TestDispatcherClientGetTaskSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"t1","status":"running","percent":30}}`))
	}))
	defer srv.Close()
	cli := NewDispatcherClient(srv.URL, "t")
	snap, err := cli.GetTask(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != "running" {
		t.Fatalf("status=%s", snap.Status)
	}
	if snap.Percent != 30 {
		t.Fatalf("percent=%d", snap.Percent)
	}
}

func TestDispatcherClientGetTask404RemapsToTaskNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"code":40400,"message":"task not found"}`))
	}))
	defer srv.Close()
	cli := NewDispatcherClient(srv.URL, "t")
	_, err := cli.GetTask(context.Background(), "t-missing")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestDispatcherClientCancelTask404RemapsToTaskNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"code":40400,"message":"task not found"}`))
	}))
	defer srv.Close()
	cli := NewDispatcherClient(srv.URL, "t")
	err := cli.CancelTask(context.Background(), "t-missing")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

// TestDispatcherClient_Invoke_OnBehalfOfUserID 验证 InvokeOptions.OnBehalfOfUserID
// 正确出现在 POST /v1/apps/self/invoke 的 JSON payload 里（回归测试）。
func TestDispatcherClient_Invoke_OnBehalfOfUserID(t *testing.T) {
	var gotPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"result":{"ok":true},"duration_ms":1}}`))
	}))
	defer srv.Close()

	client := NewDispatcherClient(srv.URL, "test-token")
	_, err := client.Invoke(context.Background(), InvokeOptions{
		Capability:       "test.echo",
		Args:             map[string]any{"message": "hi"},
		Mode:             "sync",
		OnBehalfOfUserID: 42,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if gotPayload["on_behalf_of_user_id"] == nil {
		t.Fatal("payload 缺 on_behalf_of_user_id 字段")
	}
	if v, _ := gotPayload["on_behalf_of_user_id"].(float64); int64(v) != 42 {
		t.Errorf("on_behalf_of_user_id = %v, want 42", gotPayload["on_behalf_of_user_id"])
	}
}

// TestDispatcherClient_Invoke_OnBehalfOfUserID_Zero 验证 OnBehalfOfUserID=0 时
// payload 不携带该字段（避免误覆盖 keystone 内部默认行为）。
func TestDispatcherClient_Invoke_OnBehalfOfUserID_Zero(t *testing.T) {
	var gotPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"result":{},"duration_ms":1}}`))
	}))
	defer srv.Close()

	client := NewDispatcherClient(srv.URL, "test-token")
	_, _ = client.Invoke(context.Background(), InvokeOptions{
		Capability: "test.echo",
		Mode:       "sync",
	})
	if _, ok := gotPayload["on_behalf_of_user_id"]; ok {
		t.Errorf("OnBehalfOfUserID=0 时 payload 不应携带 on_behalf_of_user_id 字段，实际有 %v", gotPayload["on_behalf_of_user_id"])
	}
}
