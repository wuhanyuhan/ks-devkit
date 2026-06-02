package ksapp

import (
	"context"
	"sync/atomic"
	"time"
)

// CapabilityContext 是 capability handler 收到的运行时上下文。
// Handler 签名：func(ctx CapabilityContext, args map[string]any) (any, error)。
type CapabilityContext interface {
	UserID() string
	CallerID() string
	CallerKind() string
	ChainID() string
	ChainHeader() string
	TaskID() string
	RequestID() string
	CanonicalName() string

	// Context 返回原 context.Context，handler 内调下游 SDK / HTTP / 数据库时应透传它，
	// 这样 Task.Cancel / dispatcher 超时 / 上游 ctx done 能直接传到下游链路。
	// 上游未提供时返回 context.Background()（保证非 nil）。
	Context() context.Context

	// Progress 上报进度（仅 long_running 任务有效；sync 调用 / TaskID="" 时 no-op）。
	Progress(ctx context.Context, stage string, percent *int) error
	// Deadline 返回 unix ms；TimeoutMs<=0 返 0。
	Deadline() int64
	// Cancelled cooperative cancellation 检查。
	Cancelled() bool
}

// ProgressReportFn 用于注入实际的 progress 上报路径（DispatcherClient.ReportProgress）。
// 函数注入而非 interface 是为规避循环 import。
type ProgressReportFn func(ctx context.Context, stage string, percent *int) error

// capabilityContextInit 是 newCapabilityContext 的入参集合。
type capabilityContextInit struct {
	Ctx                                                                                  context.Context
	UserID, CallerID, CallerKind, ChainID, ChainHeader, TaskID, RequestID, CanonicalName string
	TimeoutMs                                                                            int
	StartedAtMs                                                                          int64
	ReportFn                                                                             ProgressReportFn
}

// capabilityContextImpl 是 CapabilityContext 的默认实现。
type capabilityContextImpl struct {
	ctx                                                                                  context.Context
	userID, callerID, callerKind, chainID, chainHeader, taskID, requestID, canonicalName string
	timeoutMs                                                                            int
	startedAtMs                                                                          int64
	reportFn                                                                             ProgressReportFn
	cancelled                                                                            atomic.Bool
}

func newCapabilityContext(init capabilityContextInit) *capabilityContextImpl {
	started := init.StartedAtMs
	if started == 0 {
		started = time.Now().UnixMilli()
	}
	ctx := init.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return &capabilityContextImpl{
		ctx:           ctx,
		userID:        init.UserID,
		callerID:      init.CallerID,
		callerKind:    init.CallerKind,
		chainID:       init.ChainID,
		chainHeader:   init.ChainHeader,
		taskID:        init.TaskID,
		requestID:     init.RequestID,
		canonicalName: init.CanonicalName,
		timeoutMs:     init.TimeoutMs,
		startedAtMs:   started,
		reportFn:      init.ReportFn,
	}
}

func (c *capabilityContextImpl) UserID() string           { return c.userID }
func (c *capabilityContextImpl) CallerID() string         { return c.callerID }
func (c *capabilityContextImpl) CallerKind() string       { return c.callerKind }
func (c *capabilityContextImpl) ChainID() string          { return c.chainID }
func (c *capabilityContextImpl) ChainHeader() string      { return c.chainHeader }
func (c *capabilityContextImpl) TaskID() string           { return c.taskID }
func (c *capabilityContextImpl) RequestID() string        { return c.requestID }
func (c *capabilityContextImpl) CanonicalName() string    { return c.canonicalName }
func (c *capabilityContextImpl) Context() context.Context { return c.ctx }

func (c *capabilityContextImpl) Progress(ctx context.Context, stage string, percent *int) error {
	if c.taskID == "" || c.reportFn == nil {
		return nil
	}
	// best-effort：业务 handler 不因 progress 失败而失败
	_ = c.reportFn(ctx, stage, percent)
	return nil
}

func (c *capabilityContextImpl) Deadline() int64 {
	if c.timeoutMs <= 0 {
		return 0
	}
	return c.startedAtMs + int64(c.timeoutMs)
}

func (c *capabilityContextImpl) Cancelled() bool {
	return c.cancelled.Load()
}

func (c *capabilityContextImpl) setCancelled() {
	c.cancelled.Store(true)
}
