package ksapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestEventsClientRegisterAndDispatch(t *testing.T) {
	c := NewEventsClient("http://k", "t", EventsModePolling)
	stream := c.Register("task-1")
	defer c.Unregister("task-1")

	c.dispatch(map[string]any{
		"type":    "capability.task.lifecycle",
		"task_id": "task-1",
		"status":  "running",
		"percent": 30,
	})

	select {
	case ev := <-stream.Channel():
		if ev["status"] != "running" {
			t.Fatalf("status=%v", ev["status"])
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive event")
	}
}

func TestEventsClientDropUnregistered(t *testing.T) {
	c := NewEventsClient("http://k", "t", EventsModePolling)
	stream := c.Register("task-1")

	c.dispatch(map[string]any{
		"task_id": "task-OTHER",
		"status":  "running",
	})

	select {
	case ev := <-stream.Channel():
		t.Fatalf("unexpected event: %v", ev)
	case <-time.After(50 * time.Millisecond):
		// pass — drop 未注册 task 的事件
	}
}

func TestEventsClientPollOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/self/events" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"events":[{"type":"capability.task.lifecycle","task_id":"t1","status":"running","percent":50}],"next_cursor":"100"}}`))
	}))
	defer srv.Close()
	c := NewEventsClient(srv.URL, "t", EventsModePolling)
	stream := c.Register("t1")
	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.pollingCursor != "100" {
		t.Fatalf("cursor=%s", c.pollingCursor)
	}
	select {
	case ev := <-stream.Channel():
		if ev["status"] != "running" {
			t.Fatalf("status=%v", ev["status"])
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}

func TestEventsClientWSReconnectsAfterDisconnect(t *testing.T) {
	var conns atomic.Int32
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		n := conns.Add(1)
		if n == 1 {
			_ = conn.WriteJSON(map[string]any{
				"type":    "capability.task.lifecycle",
				"task_id": "task-1",
				"status":  "running",
				"seq":     1,
			})
			// 模拟服务端断开
			_ = conn.Close()
			return
		}
		// 第二次连接：再发一个事件，验证重连后流恢复
		_ = conn.WriteJSON(map[string]any{
			"type":    "capability.task.lifecycle",
			"task_id": "task-1",
			"status":  "done",
			"seq":     2,
		})
		// 保持连接直到 ctx 关
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1)

	c := NewEventsClient(wsURL, "t", EventsModeWS)
	stream := c.Register("task-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	defer c.Close()

	seqs := []int{}
	deadline := time.After(3 * time.Second)
	for len(seqs) < 2 {
		select {
		case ev := <-stream.Channel():
			if s, ok := ev["seq"].(float64); ok {
				seqs = append(seqs, int(s))
			}
		case <-deadline:
			t.Fatalf("only received %v events before deadline", seqs)
		}
	}
	if seqs[0] != 1 || seqs[1] != 2 {
		t.Fatalf("event order = %v, want [1,2]", seqs)
	}
	if conns.Load() < 2 {
		t.Fatalf("expected >=2 connections (initial + reconnect), got %d", conns.Load())
	}
}

func TestEventsClientPollOnceAdvancesCursor(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			if r.URL.RawQuery != "since=0" {
				t.Fatalf("first poll should pass since=0, got %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"events":[],"next_cursor":"42"}}`))
			return
		}
		if r.URL.RawQuery != "since=42" {
			t.Fatalf("second poll should pass since=42, got %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"events":[],"next_cursor":"77"}}`))
	}))
	defer srv.Close()
	c := NewEventsClient(srv.URL, "t", EventsModePolling)
	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.pollingCursor != "42" {
		t.Fatalf("cursor after 1st = %s", c.pollingCursor)
	}
	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.pollingCursor != "77" {
		t.Fatalf("cursor after 2nd = %s", c.pollingCursor)
	}
}
