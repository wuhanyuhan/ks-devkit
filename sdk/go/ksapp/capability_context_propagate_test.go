package ksapp

import (
	"context"
	"testing"
	"time"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/mcpproto"
)

// TestCapabilityContext_Context_Cancellation 验证 handler 内通过 ctx.Context() 取到的
// 原 context.Context 能正确感知上游 Cancel（回归测试）。
func TestCapabilityContext_Context_Cancellation(t *testing.T) {
	parentCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	entry := &capabilityEntry{
		CanonicalName: "test.cap",
		TimeoutMs:     1000,
	}
	app := New("test-app")
	capCtx := buildCapabilityContextFromGoCtx(parentCtx, entry, app)

	got := capCtx.Context()
	if got == nil {
		t.Fatal("CapabilityContext.Context() returned nil")
	}

	cancel()

	select {
	case <-got.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ctx.Done() 未在上游 cancel 后收到信号")
	}
}

// TestCapabilityContext_Context_NilFallback 验证 init.Ctx 为 nil 时 fallback 到
// context.Background()（保护未来 caller 漏传 ctx 不至于返 nil）。
func TestCapabilityContext_Context_NilFallback(t *testing.T) {
	capCtx := newCapabilityContext(capabilityContextInit{
		CanonicalName: "test.bare",
	})
	got := capCtx.Context()
	if got == nil {
		t.Fatal("Context() returned nil when init.Ctx omitted; should fallback to Background")
	}
	if got.Err() != nil {
		t.Errorf("fallback Background ctx should not be cancelled, got Err=%v", got.Err())
	}
}

// TestMcpToolPathInjectsChainHeader 锁定（SDK 侧）：mcp_tool capability 路径
// 从 _meta.ks_chain_snapshot 读调用链快照并注入 CapabilityContext.ChainHeader，
// 使被调 capability 能继续透传调用链。
// 注意：完整生效需 keystone mcptool executor 配套往 _meta 放 ks_chain_snapshot；
// keystone 未透传时 ChainHeader 为空但不回归。
func TestMcpToolPathInjectsChainHeader(t *testing.T) {
	app := New("ks-mcp-x")
	entry := &capabilityEntry{CanonicalName: "ks-mcp-x.foo"}
	ctx := context.Background()
	ctx = mcpproto.WithMeta(ctx, map[string]any{
		"ks_chain_id":       "chn_1",
		"ks_chain_snapshot": "eyJjaGFpbiI6IDF9",
	})
	capCtx := buildCapabilityContextFromGoCtx(ctx, entry, app)
	if capCtx.ChainID() != "chn_1" {
		t.Fatalf("ChainID=%q", capCtx.ChainID())
	}
	if capCtx.ChainHeader() != "eyJjaGFpbiI6IDF9" {
		t.Fatalf("ChainHeader=%q want snapshot（Bug#2：mcp_tool 路径应注入 chain snapshot）", capCtx.ChainHeader())
	}
}
