package ksapp

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestToolDuplicatePanic 验证重复注册同名工具时 panic，且消息包含工具名。
func TestToolDuplicatePanic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic，但没有发生")
		}
		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, "greet") {
			t.Errorf("期望 panic 消息包含工具名 'greet'，实际: %s", msg)
		}
	}()
	app := New("test-app")
	app.Tool("greet", "打招呼", func(ctx context.Context, params map[string]any) (any, error) {
		return nil, nil
	})
	// 第二次注册相同名称，应触发 panic
	app.Tool("greet", "重复", func(ctx context.Context, params map[string]any) (any, error) {
		return nil, nil
	})
}

func TestToolRegistration(t *testing.T) {
	app := New("test-app")
	app.Tool("greet", "打招呼", func(ctx context.Context, params map[string]any) (any, error) {
		return map[string]string{"msg": "hello"}, nil
	})

	if len(app.tools) != 1 {
		t.Fatalf("tools count: %d", len(app.tools))
	}
	if app.tools[0].Name != "greet" {
		t.Errorf("name: %q", app.tools[0].Name)
	}
	if app.tools[0].Description != "打招呼" {
		t.Errorf("description: %q", app.tools[0].Description)
	}
	if app.tools[0].Handler == nil {
		t.Error("handler is nil")
	}
}

func TestToolExecution(t *testing.T) {
	app := New("test-app")
	app.Tool("add", "加法", func(ctx context.Context, params map[string]any) (any, error) {
		a := params["a"].(float64)
		b := params["b"].(float64)
		return map[string]float64{"sum": a + b}, nil
	})

	result, err := app.tools[0].Handler(context.Background(), map[string]any{"a": 1.0, "b": 2.0})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	m := result.(map[string]float64)
	if m["sum"] != 3.0 {
		t.Errorf("sum: %f", m["sum"])
	}
}
