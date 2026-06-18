package ksapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// InitProgressFunc 由 init_task handler 执行期调用以上报进度（percent 0-100、message 人话）。
type InitProgressFunc func(percent int, message string)

// InitTaskHandler 是应用为某 init_task 就绪门提供的一次性初始化逻辑。
// 经 progress 上报进度；返回 nil = 就绪(ready)，返回 error = 失败(failed，message 取 err.Error())。
// 应自身幂等（重跑安全）：keystone 可能重触发（重试 / 重 seed）。
type InitTaskHandler func(ctx context.Context, progress InitProgressFunc) error

// initTaskRuntime 持有单个 init_task 门的 handler 与运行时状态（SDK 内存态；应用重启即重置 pending，
// keystone 持久化权威态于 t_installed_app_readiness_gates，重启后按需重触发收敛）。
type initTaskRuntime struct {
	handler InitTaskHandler

	mu       sync.Mutex
	status   kstypes.ReadinessGateStatus
	progress *int
	message  string
}

// snapshot 在锁内拷出当前状态为 wire DTO（progress 深拷贝避免外泄内部指针）。
func (rt *initTaskRuntime) snapshot(gateID string) kstypes.ReadinessGateState {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	st := kstypes.ReadinessGateState{ID: gateID, Status: rt.status, Message: rt.message}
	if rt.progress != nil {
		p := *rt.progress
		st.Progress = &p
	}
	return st
}

// RegisterInitTask 为 manifest 声明的某 init_task 就绪门(readiness.gates[].id, kind=init_task)注册初始化逻辑。
// gateID 重复注册 panic（配置期错误快速失败，与 Tool/RegisterCapability 一致）。初始状态 pending。
func (a *App) RegisterInitTask(gateID string, handler InitTaskHandler) *App {
	if a.initTasks == nil {
		a.initTasks = make(map[string]*initTaskRuntime)
	}
	if _, exists := a.initTasks[gateID]; exists {
		panic(fmt.Sprintf("ksapp: init task %q already registered", gateID))
	}
	a.initTasks[gateID] = &initTaskRuntime{
		handler: handler,
		status:  kstypes.ReadinessGateStatusPending,
	}
	return a
}

// registerReadinessEndpoints 注册 GET /ks-readiness 与 POST /ks-readiness/init（仅当注册了 init_task）。
// server-to-server 端点，与 /healthz、/meta 同通路、不做鉴权（平台内网调用、非浏览器反代）。
func registerReadinessEndpoints(mux *http.ServeMux, initTasks map[string]*initTaskRuntime) {
	mux.HandleFunc("GET /ks-readiness", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		report := kstypes.ReadinessReport{Gates: make([]kstypes.ReadinessGateState, 0, len(initTasks))}
		for gateID, rt := range initTasks {
			report.Gates = append(report.Gates, rt.snapshot(gateID))
		}
		// 稳定排序，输出确定（map 遍历无序；便于 keystone diff 与测试断言）。
		sort.Slice(report.Gates, func(i, j int) bool { return report.Gates[i].ID < report.Gates[j].ID })
		_ = json.NewEncoder(w).Encode(report)
	})
	mux.HandleFunc("POST /ks-readiness/init", func(w http.ResponseWriter, r *http.Request) {
		var req kstypes.ReadinessInitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "ERR_SCHEMA", "request body JSON 解析失败: "+err.Error(), nil)
			return
		}
		rt, ok := initTasks[req.GateID]
		if !ok {
			writeErr(w, http.StatusNotFound, "ERR_GATE_NOT_FOUND", "未注册的 init_task 门: "+req.GateID, nil)
			return
		}
		rt.mu.Lock()
		if rt.status == kstypes.ReadinessGateStatusRunning {
			rt.mu.Unlock()
			// 已在跑 -> 幂等 no-op，回当前态。
			writeJSON(w, http.StatusOK, Result{Code: 0, Message: "初始化进行中", Data: rt.snapshot(req.GateID)})
			return
		}
		rt.status = kstypes.ReadinessGateStatusRunning
		rt.progress = nil
		rt.message = ""
		rt.mu.Unlock()

		go runInitTask(rt, req.GateID)
		writeJSON(w, http.StatusOK, Result{Code: 0, Message: "初始化已触发", Data: rt.snapshot(req.GateID)})
	})
}

// runInitTask 后台执行 init handler，按返回值收敛门状态（ready / failed），并承接 progress 上报。
// recover 防止应用 handler panic 击穿后台 goroutine（禁裸 goroutine 规约）。
func runInitTask(rt *initTaskRuntime, gateID string) {
	progress := func(percent int, message string) {
		rt.mu.Lock()
		p := percent
		rt.progress = &p
		rt.message = message
		rt.mu.Unlock()
	}
	defer func() {
		if rec := recover(); rec != nil {
			rt.mu.Lock()
			rt.status = kstypes.ReadinessGateStatusFailed
			rt.message = fmt.Sprintf("init task panic: %v", rec)
			rt.mu.Unlock()
		}
	}()
	err := rt.handler(context.Background(), progress)
	rt.mu.Lock()
	if err != nil {
		rt.status = kstypes.ReadinessGateStatusFailed
		rt.message = err.Error()
	} else {
		rt.status = kstypes.ReadinessGateStatusReady
		p := 100
		rt.progress = &p
	}
	rt.mu.Unlock()
}
