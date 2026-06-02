package ksapp

import (
	"context"
	"fmt"
	"time"
)

// Task 是 async capability 调用的句柄（Go 形态）。
//
// 与 Python `Task` 等价：caller 调 CallCapability(name).Submit(args) 拿到一个 Task，
// 然后 Refresh / Cancel / Result / Events 跟踪任务状态。
type Task struct {
	TaskID        string
	Status        string
	CanonicalName string
	Percent       int
	StageMessage  string
	ResultPayload map[string]any
	ErrorCode     string
	ErrorMessage  string

	dispatcher *DispatcherClient
	events     *EventsClient
}

var terminalStatuses = map[string]struct{}{
	"done": {}, "failed": {}, "cancelled": {},
}

// Refresh 主动拉一次最新快照覆盖本地字段。
func (t *Task) Refresh(ctx context.Context) error {
	snap, err := t.dispatcher.GetTask(ctx, t.TaskID)
	if err != nil {
		return err
	}
	t.Status = snap.Status
	t.Percent = snap.Percent
	t.StageMessage = snap.StageMessage
	if snap.Result != nil {
		t.ResultPayload = snap.Result
	}
	if snap.ErrorCode != "" {
		t.ErrorCode = snap.ErrorCode
	}
	if snap.ErrorMessage != "" {
		t.ErrorMessage = snap.ErrorMessage
	}
	return nil
}

// Cancel 调 keystone 取消任务。
func (t *Task) Cancel(ctx context.Context) error {
	return t.dispatcher.CancelTask(ctx, t.TaskID)
}

// Result 等待终态。
//   - done   → 返回 ResultPayload
//   - failed → 包装 ErrBackendError（或 ErrDispatcherRestarted）
//   - cancelled → ErrCancelled
//
// pollInterval=0 时取默认 2 秒；ctx 超时返 ErrTimeout 包装。
// EventsClient 接入后会优先用 event 驱动，poll 仅作 fallback。
func (t *Task) Result(ctx context.Context, pollInterval time.Duration) (map[string]any, error) {
	if pollInterval == 0 {
		pollInterval = 2 * time.Second
	}
	for {
		if err := t.Refresh(ctx); err != nil {
			return nil, err
		}
		if _, terminal := terminalStatuses[t.Status]; terminal {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: ctx cancelled", ErrTimeout)
		case <-time.After(pollInterval):
		}
	}
	switch t.Status {
	case "done":
		return t.ResultPayload, nil
	case "failed":
		if t.ErrorCode == "50000" || t.ErrorCode == "DispatcherRestarted" {
			return nil, fmt.Errorf("%w: %s", ErrDispatcherRestarted, t.ErrorMessage)
		}
		return nil, fmt.Errorf("%w: %s", ErrBackendError, t.ErrorMessage)
	case "cancelled":
		return nil, fmt.Errorf("%w: %s", ErrCancelled, t.ErrorMessage)
	}
	return nil, fmt.Errorf("%w: unexpected status=%s", ErrBackendError, t.Status)
}

// Events 返一个 channel 推送 lifecycle 事件。
// 已注入 EventsClient → 走 WS / polling；未注入则发一次 snapshot 后关闭。
func (t *Task) Events(ctx context.Context) (<-chan map[string]any, error) {
	if t.events != nil {
		stream := t.events.Register(t.TaskID)
		return stream.Channel(), nil
	}
	ch := make(chan map[string]any, 1)
	go func() {
		defer close(ch)
		_ = t.Refresh(ctx)
		select {
		case ch <- map[string]any{
			"type":          "snapshot",
			"task_id":       t.TaskID,
			"status":        t.Status,
			"percent":       t.Percent,
			"stage_message": t.StageMessage,
		}:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

// CapabilityCall 是 app.CallCapability(name) 返的构造器。
// 链式调 .Invoke(ctx, args) 走 sync；调 .Submit(ctx, args) 走 async 拿 *Task。
//
// 链式 options（v0.10.0+）：
//   - WithOnBehalfOfUser(uid)：透传调用链发起人 user_id，多跳 capability mesh
//     调用时必须设置（keystone dispatcher 端契约要求）。
//   - WithChainContext(chainID, chainHeader)：从上游 CapabilityContext 透传调用链，
//     让嵌套 app-to-app 调用继续落在同一 chain_id / chain_snapshot。
type CapabilityCall struct {
	canonicalName    string
	dispatcher       *DispatcherClient
	events           *EventsClient
	onBehalfOfUserID int64
	chainID          string
	chainHeader      string
}

// WithOnBehalfOfUser 设置调用链发起人 user_id，透传到 dispatcher 的
// on_behalf_of_user_id payload 字段，让 keystone 在多跳 capability mesh 调用中
// 维持初始 user 上下文。
//
// 典型使用：app-to-app 调用时，caller 应从 CapabilityContext.UserID() 取字符串
// 形态 user_id，atoi 后传入：
//
//	uid, _ := strconv.ParseInt(ctx.UserID(), 10, 64)
//	res, err := app.CallCapability("foo.bar").
//	    WithOnBehalfOfUser(uid).
//	    Invoke(reqCtx, args)
//
// userID=0 视为「无 user 上下文」（dispatcher payload 不带该字段，由 keystone
// 默认行为兜底，与 InvokeOptions.OnBehalfOfUserID=0 语义对齐）。
//
// 返回 *CapabilityCall 自身以便链式调用。
func (cc *CapabilityCall) WithOnBehalfOfUser(userID int64) *CapabilityCall {
	cc.onBehalfOfUserID = userID
	return cc
}

// WithChainContext 透传当前 capability 调用链上下文。
//
// 典型使用：在 capability handler 内嵌套调用其它 capability 时，把
// CapabilityContext.ChainID() 与 CapabilityContext.ChainHeader() 原样传入：
//
//	res, err := app.CallCapability("foo.bar").
//	    WithOnBehalfOfUser(uid).
//	    WithChainContext(ctx.ChainID(), ctx.ChainHeader()).
//	    Invoke(reqCtx, args)
//
// 任一参数为空时 SDK 不写对应 header，dispatcher 会按现有默认行为兜底。
func (cc *CapabilityCall) WithChainContext(chainID, chainHeader string) *CapabilityCall {
	cc.chainID = chainID
	cc.chainHeader = chainHeader
	return cc
}

// Invoke sync 调 dispatcher，返结果 map。
func (cc *CapabilityCall) Invoke(ctx context.Context, args map[string]any) (map[string]any, error) {
	res, err := cc.dispatcher.Invoke(ctx, InvokeOptions{
		Capability:       cc.canonicalName,
		Args:             args,
		Mode:             "sync",
		OnBehalfOfUserID: cc.onBehalfOfUserID,
		ChainID:          cc.chainID,
		ChainHeader:      cc.chainHeader,
	})
	if err != nil {
		return nil, err
	}
	if res.Sync != nil {
		return res.Sync.Result, nil
	}
	return nil, fmt.Errorf("%w: expected sync but got async task_id", ErrBackendError)
}

// Submit async 提交，返 *Task。
// 若 dispatcher 同步完成（mode=auto 时可能），返 Status="done" 已有 ResultPayload 的 Task。
func (cc *CapabilityCall) Submit(ctx context.Context, args map[string]any) (*Task, error) {
	res, err := cc.dispatcher.Invoke(ctx, InvokeOptions{
		Capability:       cc.canonicalName,
		Args:             args,
		Mode:             "async",
		OnBehalfOfUserID: cc.onBehalfOfUserID,
		ChainID:          cc.chainID,
		ChainHeader:      cc.chainHeader,
	})
	if err != nil {
		return nil, err
	}
	if res.Async != nil {
		return &Task{
			TaskID:        res.Async.TaskID,
			Status:        res.Async.Status,
			CanonicalName: cc.canonicalName,
			dispatcher:    cc.dispatcher,
			events:        cc.events,
		}, nil
	}
	if res.Sync != nil {
		return &Task{
			Status:        "done",
			CanonicalName: cc.canonicalName,
			ResultPayload: res.Sync.Result,
			dispatcher:    cc.dispatcher,
			events:        cc.events,
		}, nil
	}
	return nil, fmt.Errorf("%w: empty invoke result", ErrBackendError)
}
