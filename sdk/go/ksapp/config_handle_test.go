package ksapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/keystore"
)

type testCfg struct {
	APIKey string `ksconfig:"required,type:password,label:API Key"`
}

func TestNewConfigOn_RegistersOnApp(t *testing.T) {
	t.Parallel()
	app := New("test-app-register")
	cfg := NewConfigOn(app, ConfigSpec[testCfg]{})
	if cfg.Get() != nil {
		t.Errorf("初始未 save，Get() 应返回 nil")
	}
}

func TestNewConfigOn_PanicsOnDuplicateTypeSameApp(t *testing.T) {
	t.Parallel()
	app := New("test-app-dup")
	_ = NewConfigOn(app, ConfigSpec[testCfg]{})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("same T 二次 NewConfigOn 应 panic")
		}
	}()
	_ = NewConfigOn(app, ConfigSpec[testCfg]{})
}

func TestConfig_Get_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	app := New("test-app-concurrent")
	cfg := NewConfigOn(app, ConfigSpec[testCfg]{})
	var wg sync.WaitGroup
	var counter atomic.Int64
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cfg.Get()
			counter.Add(1)
		}()
	}
	wg.Wait()
	if counter.Load() != 100 {
		t.Errorf("并发 Get 未完成: %d", counter.Load())
	}
}

func TestConfig_HandleValidate_Called(t *testing.T) {
	t.Parallel()
	app := New("test-app-validate")
	var validated atomic.Int32
	cfg := NewConfigOn(app, ConfigSpec[testCfg]{
		OnValidate: func(ctx context.Context, c *testCfg) error {
			validated.Store(1)
			return nil
		},
	})
	if err := cfg.handleValidate(context.Background(), &testCfg{APIKey: "sk-xxx"}); err != nil {
		t.Fatal(err)
	}
	if validated.Load() != 1 {
		t.Errorf("OnValidate 未被调用")
	}
}

// TestNewConfigOn_NilAppPanics 覆盖 I2：传 nil app 应给出友好 panic。
func TestNewConfigOn_NilAppPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for nil app")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "app 不能为 nil") {
			t.Errorf("panic 信息不含 \"app 不能为 nil\": %v", r)
		}
	}()
	_ = NewConfigOn[testCfg](nil, ConfigSpec[testCfg]{})
}

// TestHandleSave_ValidateError_NotStored 覆盖 I3：OnValidate 失败不应 Store。
func TestHandleSave_ValidateError_NotStored(t *testing.T) {
	t.Parallel()
	app := New("test-app-validate-fail")
	cfg := NewConfigOn(app, ConfigSpec[testCfg]{
		OnValidate: func(ctx context.Context, c *testCfg) error {
			return fmt.Errorf("validate failed")
		},
	})
	cfg.dek = make([]byte, 32) // fail-fast：handleSave 调用前必须注入 dek
	err := cfg.handleSave(context.Background(), &testCfg{APIKey: "sk-bad"})
	if err == nil || !strings.Contains(err.Error(), "validate failed") {
		t.Fatalf("expected validate error, got: %v", err)
	}
	if cfg.Get() != nil {
		t.Errorf("Get() should be nil after failed validate, got %+v", cfg.Get())
	}
}

// TestHandleSave_ApplyError_Rollback 覆盖 I3：OnApply 失败应回滚到 oldCfg（nil）。
func TestHandleSave_ApplyError_Rollback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	app := New("test-app-apply-fail")
	cfg := NewConfigOn(app, ConfigSpec[testCfg]{
		OnApply: func(ctx context.Context, c *testCfg) error {
			return fmt.Errorf("apply failed")
		},
	})
	// fail-fast：handleSave 调用前必须注入 dek + persistPath
	// （rollbackPersisted 走 oldCfg==nil 分支会试 os.Remove(persistPath)，
	// persistPath 为空字符串会引起 ENOENT 但被吞掉，无副作用——这里仍给出
	// 真实临时路径以贴近实际注入习惯）
	cfg.persistPath = filepath.Join(dir, "mcp-config.enc")
	cfg.dek = make([]byte, 32)
	// 第一次 save 直接失败；ptr 应保持 nil（rollback 到 nil）
	err := cfg.handleSave(context.Background(), &testCfg{APIKey: "sk-1"})
	if err == nil || !strings.Contains(err.Error(), "apply failed") {
		t.Fatalf("expected apply error, got: %v", err)
	}
	if cfg.Get() != nil {
		t.Errorf("Get() should be nil after failed apply (rollback to nil), got %+v", cfg.Get())
	}
}

// TestHandleSave_PersistAndLoad 覆盖端到端：handleSave 把 newCfg 通过 DEK
// 加密落盘到 mcp-config.enc，loadPersisted 重启场景能从同一文件解密恢复。
func TestHandleSave_PersistAndLoad(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	app := New("test-app-persist")
	cfg := NewConfigOn(app, ConfigSpec[testCfg]{
		OnApply: func(ctx context.Context, c *testCfg) error { return nil },
	})
	cfg.persistPath = filepath.Join(dir, "mcp-config.enc")
	cfg.dekPath = filepath.Join(dir, ".local-dek")
	dek, err := keystore.LoadOrGenerateDEK(cfg.dekPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateDEK: %v", err)
	}
	cfg.dek = dek

	newCfg := &testCfg{APIKey: "sk-12345"}
	if err := cfg.handleSave(context.Background(), newCfg); err != nil {
		t.Fatalf("handleSave: %v", err)
	}
	// mcp-config.enc 应已生成
	if _, err := os.Stat(cfg.persistPath); err != nil {
		t.Fatalf("mcp-config.enc 未生成: %v", err)
	}

	loaded := cfg.loadPersisted()
	if loaded == nil {
		t.Fatal("loadPersisted 返回 nil")
	}
	if loaded.APIKey != "sk-12345" {
		t.Errorf("loadPersisted APIKey = %q, want \"sk-12345\"", loaded.APIKey)
	}
}

// TestHandleSave_ApplyError_DiskRollback_FirstSave 首次 save 失败时，磁盘上的
// mcp-config.enc 应被删除（恢复"无配置"状态）。
func TestHandleSave_ApplyError_DiskRollback_FirstSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	app := New("test-app-disk-rollback-first")
	cfg := NewConfigOn(app, ConfigSpec[testCfg]{
		OnApply: func(ctx context.Context, c *testCfg) error {
			return fmt.Errorf("apply boom")
		},
	})
	cfg.persistPath = filepath.Join(dir, "mcp-config.enc")
	cfg.dekPath = filepath.Join(dir, ".local-dek")
	dek, _ := keystore.LoadOrGenerateDEK(cfg.dekPath)
	cfg.dek = dek

	err := cfg.handleSave(context.Background(), &testCfg{APIKey: "sk-fail"})
	if err == nil || !strings.Contains(err.Error(), "apply boom") {
		t.Fatalf("expected apply error, got: %v", err)
	}
	// 磁盘文件应已被回滚删除
	if _, err := os.Stat(cfg.persistPath); !os.IsNotExist(err) {
		t.Errorf("首次 save 失败应删除 mcp-config.enc, got err=%v", err)
	}
	// 内存 ptr 也应回到 nil
	if cfg.Get() != nil {
		t.Errorf("Get() should be nil after failed first apply, got %+v", cfg.Get())
	}
}

// TestHandleSave_ApplyError_DiskRollback_SecondSave 第二次 save 失败时，磁盘应回滚
// 到第一次 save 的内容（loadPersisted 能拿到旧值）。
func TestHandleSave_ApplyError_DiskRollback_SecondSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	app := New("test-app-disk-rollback-second")
	var failOnSecond atomic.Bool
	cfg := NewConfigOn(app, ConfigSpec[testCfg]{
		OnApply: func(ctx context.Context, c *testCfg) error {
			if failOnSecond.Load() {
				return fmt.Errorf("second apply boom")
			}
			return nil
		},
	})
	cfg.persistPath = filepath.Join(dir, "mcp-config.enc")
	cfg.dekPath = filepath.Join(dir, ".local-dek")
	dek, _ := keystore.LoadOrGenerateDEK(cfg.dekPath)
	cfg.dek = dek

	// 第一次 save 成功
	first := &testCfg{APIKey: "sk-first"}
	if err := cfg.handleSave(context.Background(), first); err != nil {
		t.Fatalf("first handleSave: %v", err)
	}
	// 第二次 save 在 OnApply 失败
	failOnSecond.Store(true)
	err := cfg.handleSave(context.Background(), &testCfg{APIKey: "sk-second"})
	if err == nil || !strings.Contains(err.Error(), "second apply boom") {
		t.Fatalf("expected second apply error, got: %v", err)
	}
	// 内存应回到 first
	got := cfg.Get()
	if got == nil || got.APIKey != "sk-first" {
		t.Errorf("内存 ptr 未回滚到 first, got %+v", got)
	}
	// 磁盘 loadPersisted 也应回到 first
	loaded := cfg.loadPersisted()
	if loaded == nil || loaded.APIKey != "sk-first" {
		t.Errorf("磁盘未回滚到 first, got %+v", loaded)
	}
}

// TestHandleSave_ValidateError_NoFile OnValidate 失败不应留下任何 mcp-config.enc。
func TestHandleSave_ValidateError_NoFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	app := New("test-app-validate-fail-nofile")
	cfg := NewConfigOn(app, ConfigSpec[testCfg]{
		OnValidate: func(ctx context.Context, c *testCfg) error {
			return fmt.Errorf("validate failed")
		},
	})
	cfg.persistPath = filepath.Join(dir, "mcp-config.enc")
	cfg.dekPath = filepath.Join(dir, ".local-dek")
	dek, _ := keystore.LoadOrGenerateDEK(cfg.dekPath)
	cfg.dek = dek

	err := cfg.handleSave(context.Background(), &testCfg{APIKey: "sk-bad"})
	if err == nil || !strings.Contains(err.Error(), "validate failed") {
		t.Fatalf("expected validate error, got: %v", err)
	}
	if _, err := os.Stat(cfg.persistPath); !os.IsNotExist(err) {
		t.Errorf("OnValidate 失败不应写盘, got err=%v", err)
	}
}

// 注：原 TestLoadPersisted_NoDEK 已删除。dek == nil 改为直接 panic
// （fail-fast），"不注入 dek 时返回 nil" 的兼容兜底语义不再存在，无需测试。
