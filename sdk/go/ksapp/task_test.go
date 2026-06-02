package ksapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newMockDispatcher(t *testing.T, handler http.HandlerFunc) *DispatcherClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewDispatcherClient(srv.URL, "t")
}

func TestCapabilityCallInvokeSync(t *testing.T) {
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"result":{"x":1},"duration_ms":10}}`))
	})
	cc := &CapabilityCall{canonicalName: "ks.x", dispatcher: cli}
	res, err := cc.Invoke(context.Background(), map[string]any{"topic": "AI"})
	if err != nil {
		t.Fatal(err)
	}
	if res["x"].(float64) != 1 {
		t.Fatalf("res=%v", res)
	}
}

func TestCapabilityCallSubmitAndTaskRefresh(t *testing.T) {
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/self/invoke":
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"task-1","status":"pending"}}`))
		case "/v1/user-tasks/task-1":
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"task-1","status":"running","percent":50}}`))
		}
	})
	cc := &CapabilityCall{canonicalName: "ks.x", dispatcher: cli}
	task, err := cc.Submit(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if task.TaskID != "task-1" {
		t.Fatalf("task_id=%s", task.TaskID)
	}
	if err := task.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if task.Status != "running" {
		t.Fatalf("status=%s", task.Status)
	}
	if task.Percent != 50 {
		t.Fatalf("percent=%d", task.Percent)
	}
}

func TestTaskResultDone(t *testing.T) {
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"task-1","status":"done","result":{"x":1}}}`))
	})
	task := &Task{TaskID: "task-1", Status: "pending", dispatcher: cli}
	res, err := task.Result(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if res["x"].(float64) != 1 {
		t.Fatalf("res=%v", res)
	}
}

func TestTaskResultFailedMapsToBackendError(t *testing.T) {
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"task-1","status":"failed","error_code":"BUSINESS","error_message":"boom"}}`))
	})
	task := &Task{TaskID: "task-1", Status: "pending", dispatcher: cli}
	_, err := task.Result(context.Background(), 0)
	if !errors.Is(err, ErrBackendError) {
		t.Fatalf("expected ErrBackendError, got %v", err)
	}
}

func TestTaskResultCancelled(t *testing.T) {
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"task-1","status":"cancelled"}}`))
	})
	task := &Task{TaskID: "task-1", Status: "pending", dispatcher: cli}
	_, err := task.Result(context.Background(), 0)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got %v", err)
	}
}

func TestTaskCancelHitsDispatcher(t *testing.T) {
	var hit atomic.Bool
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/user-tasks/task-1/cancel" {
			hit.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	})
	task := &Task{TaskID: "task-1", dispatcher: cli}
	if err := task.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !hit.Load() {
		t.Fatal("expected cancel endpoint hit")
	}
}

// TestCapabilityCallInvoke_WithOnBehalfOfUser 验证 v0.10.0 起的行为：
// 链式 WithOnBehalfOfUser(uid).Invoke(...) 必须把 user_id 透传到 dispatcher payload。
func TestCapabilityCallInvoke_WithOnBehalfOfUser(t *testing.T) {
	var gotPayload map[string]any
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"result":{"ok":true},"duration_ms":1}}`))
	})
	cc := &CapabilityCall{canonicalName: "ks.x", dispatcher: cli}
	_, err := cc.WithOnBehalfOfUser(42).Invoke(context.Background(), map[string]any{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPayload["on_behalf_of_user_id"] == nil {
		t.Fatal("CapabilityCall.Invoke 必须把 WithOnBehalfOfUser 透传到 payload")
	}
	if v, _ := gotPayload["on_behalf_of_user_id"].(float64); int64(v) != 42 {
		t.Errorf("on_behalf_of_user_id = %v, want 42", gotPayload["on_behalf_of_user_id"])
	}
}

func TestCapabilityCallInvoke_WithChainContext(t *testing.T) {
	var gotChainHeader string
	var gotChainID string
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		gotChainHeader = r.Header.Get(headerCallChain)
		gotChainID = r.Header.Get(headerChainID)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"result":{"ok":true},"duration_ms":1}}`))
	})
	cc := &CapabilityCall{canonicalName: "ks.x", dispatcher: cli}
	_, err := cc.WithChainContext("chain-1", "encoded-chain").Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if gotChainHeader != "encoded-chain" {
		t.Fatalf("%s = %q, want encoded-chain", headerCallChain, gotChainHeader)
	}
	if gotChainID != "chain-1" {
		t.Fatalf("%s = %q, want chain-1", headerChainID, gotChainID)
	}
}

// TestCapabilityCallInvoke_NoOnBehalfOfUser 验证未调 WithOnBehalfOfUser 时
// dispatcher payload 不携带 on_behalf_of_user_id 字段。
func TestCapabilityCallInvoke_NoOnBehalfOfUser(t *testing.T) {
	var gotPayload map[string]any
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"result":{},"duration_ms":1}}`))
	})
	cc := &CapabilityCall{canonicalName: "ks.x", dispatcher: cli}
	_, _ = cc.Invoke(context.Background(), map[string]any{})
	if _, ok := gotPayload["on_behalf_of_user_id"]; ok {
		t.Errorf("未调 WithOnBehalfOfUser 时 payload 不应携带 on_behalf_of_user_id，实际有 %v",
			gotPayload["on_behalf_of_user_id"])
	}
}

// TestCapabilityCallSubmit_WithOnBehalfOfUser 验证 Submit 路径也透传 user_id。
func TestCapabilityCallSubmit_WithOnBehalfOfUser(t *testing.T) {
	var gotPayload map[string]any
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"t-1","status":"pending"}}`))
	})
	cc := &CapabilityCall{canonicalName: "ks.x", dispatcher: cli}
	task, err := cc.WithOnBehalfOfUser(99).Submit(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if task.TaskID != "t-1" {
		t.Fatalf("task_id=%s", task.TaskID)
	}
	if v, _ := gotPayload["on_behalf_of_user_id"].(float64); int64(v) != 99 {
		t.Errorf("Submit on_behalf_of_user_id = %v, want 99", gotPayload["on_behalf_of_user_id"])
	}
}

func TestCapabilityCallSubmit_WithChainContext(t *testing.T) {
	var gotChainHeader string
	var gotChainID string
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		gotChainHeader = r.Header.Get(headerCallChain)
		gotChainID = r.Header.Get(headerChainID)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"t-1","status":"pending"}}`))
	})
	cc := &CapabilityCall{canonicalName: "ks.x", dispatcher: cli}
	_, err := cc.WithChainContext("chain-2", "encoded-chain-2").Submit(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if gotChainHeader != "encoded-chain-2" {
		t.Fatalf("%s = %q, want encoded-chain-2", headerCallChain, gotChainHeader)
	}
	if gotChainID != "chain-2" {
		t.Fatalf("%s = %q, want chain-2", headerChainID, gotChainID)
	}
}

func TestTaskResultPollsThenDone(t *testing.T) {
	var calls atomic.Int32
	cli := newMockDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n := calls.Add(1)
		if n < 3 {
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"task-1","status":"running","percent":50}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"task-1","status":"done","result":{"x":1}}}`))
	})
	task := &Task{TaskID: "task-1", Status: "pending", dispatcher: cli}
	res, err := task.Result(context.Background(), 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if res["x"].(float64) != 1 {
		t.Fatalf("res=%v", res)
	}
	if calls.Load() < 3 {
		t.Fatalf("expected >=3 polls, got %d", calls.Load())
	}
}
