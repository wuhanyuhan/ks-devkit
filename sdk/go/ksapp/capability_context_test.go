package ksapp

import (
	"context"
	"testing"
	"time"
)

func TestCapabilityContextCallerIDNaming(t *testing.T) {
	c := newCapabilityContext(capabilityContextInit{
		CallerID:      "app-7",
		CanonicalName: "ks-mcp-x.foo",
	})
	if c.CallerID() != "app-7" {
		t.Fatalf("CallerID()=%q want app-7", c.CallerID())
	}
}

func TestCapabilityContextFields(t *testing.T) {
	ctx := newCapabilityContext(capabilityContextInit{
		UserID:        "u-100",
		CallerID:      "ks-mcp-writer",
		CallerKind:    "app",
		ChainID:       "chain-1",
		ChainHeader:   "encoded-chain",
		TaskID:        "task-1",
		RequestID:     "req-1",
		CanonicalName: "ks.x.foo",
		TimeoutMs:     30000,
	})
	if ctx.UserID() != "u-100" {
		t.Fatalf("UserID = %q", ctx.UserID())
	}
	if ctx.CallerID() != "ks-mcp-writer" {
		t.Fatalf("CallerID = %q", ctx.CallerID())
	}
	if ctx.CallerKind() != "app" {
		t.Fatalf("CallerKind = %q", ctx.CallerKind())
	}
	if ctx.ChainID() != "chain-1" {
		t.Fatalf("ChainID = %q", ctx.ChainID())
	}
	if ctx.ChainHeader() != "encoded-chain" {
		t.Fatalf("ChainHeader = %q", ctx.ChainHeader())
	}
	if ctx.TaskID() != "task-1" {
		t.Fatalf("TaskID = %q", ctx.TaskID())
	}
	if ctx.RequestID() != "req-1" {
		t.Fatalf("RequestID = %q", ctx.RequestID())
	}
	if ctx.CanonicalName() != "ks.x.foo" {
		t.Fatalf("CanonicalName = %q", ctx.CanonicalName())
	}
}

func TestCapabilityContextDeadline(t *testing.T) {
	start := time.Now().UnixMilli()
	ctx := newCapabilityContext(capabilityContextInit{TimeoutMs: 30000, StartedAtMs: start})
	if ctx.Deadline() != start+30000 {
		t.Fatalf("Deadline = %d, want %d", ctx.Deadline(), start+30000)
	}
}

func TestCapabilityContextDeadlineZeroWhenNoTimeout(t *testing.T) {
	ctx := newCapabilityContext(capabilityContextInit{TimeoutMs: 0})
	if ctx.Deadline() != 0 {
		t.Fatalf("Deadline = %d, want 0", ctx.Deadline())
	}
}

func TestCapabilityContextProgressNoopWithoutTaskID(t *testing.T) {
	calls := 0
	ctx := newCapabilityContext(capabilityContextInit{
		TaskID:   "",
		ReportFn: func(_ context.Context, _ string, _ *int) error { calls++; return nil },
	})
	if err := ctx.Progress(context.Background(), "x", nil); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("expected report fn not called; calls=%d", calls)
	}
}

func TestCapabilityContextProgressCallsReporter(t *testing.T) {
	got := struct {
		stage   string
		percent *int
	}{}
	pct := 30
	ctx := newCapabilityContext(capabilityContextInit{
		TaskID: "task-1",
		ReportFn: func(_ context.Context, stage string, p *int) error {
			got.stage = stage
			got.percent = p
			return nil
		},
	})
	if err := ctx.Progress(context.Background(), "正在搜索", &pct); err != nil {
		t.Fatal(err)
	}
	if got.stage != "正在搜索" || got.percent == nil || *got.percent != 30 {
		t.Fatalf("got = %+v", got)
	}
}

func TestCapabilityContextCancelled(t *testing.T) {
	ctx := newCapabilityContext(capabilityContextInit{})
	if ctx.Cancelled() {
		t.Fatal("should not be cancelled initially")
	}
	ctx.setCancelled()
	if !ctx.Cancelled() {
		t.Fatal("should be cancelled after setCancelled")
	}
}
