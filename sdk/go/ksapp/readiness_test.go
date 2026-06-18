package ksapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// TestReadiness_ReportsRegisteredInitTasks：注册一个 init_task 门，GET /ks-readiness 初始报 pending。
func TestReadiness_ReportsRegisteredInitTasks(t *testing.T) {
	app := New("test-app")
	app.RegisterInitTask("corpus_embed", func(ctx context.Context, progress InitProgressFunc) error {
		return nil
	})
	mux := app.Mux()

	req := httptest.NewRequest("GET", "/ks-readiness", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var rep kstypes.ReadinessReport
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rep))
	require.Len(t, rep.Gates, 1)
	assert.Equal(t, "corpus_embed", rep.Gates[0].ID)
	assert.Equal(t, kstypes.ReadinessGateStatusPending, rep.Gates[0].Status)
}

// TestReadiness_InitTriggersToReady：POST 触发后 handler 完成，GET 收敛到 ready。
func TestReadiness_InitTriggersToReady(t *testing.T) {
	app := New("test-app")
	done := make(chan struct{})
	app.RegisterInitTask("warm", func(ctx context.Context, progress InitProgressFunc) error {
		progress(100, "done")
		close(done)
		return nil
	})
	mux := app.Mux()

	req := httptest.NewRequest("POST", "/ks-readiness/init", strings.NewReader(`{"gate_id":"warm"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	<-done // 等后台 handler 完成
	var status kstypes.ReadinessGateStatus
	for i := 0; i < 50; i++ {
		gw := httptest.NewRecorder()
		mux.ServeHTTP(gw, httptest.NewRequest("GET", "/ks-readiness", nil))
		var rep kstypes.ReadinessReport
		require.NoError(t, json.Unmarshal(gw.Body.Bytes(), &rep))
		status = rep.Gates[0].Status
		if status == kstypes.ReadinessGateStatusReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, kstypes.ReadinessGateStatusReady, status)
}

// TestReadiness_InitUnknownGate404：触发未注册门返回 404。
func TestReadiness_InitUnknownGate404(t *testing.T) {
	app := New("test-app")
	app.RegisterInitTask("warm", func(ctx context.Context, progress InitProgressFunc) error { return nil })
	mux := app.Mux()

	req := httptest.NewRequest("POST", "/ks-readiness/init", strings.NewReader(`{"gate_id":"nope"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestReadiness_NoInitTasks_NoEndpoint：未注册任何 init_task 时不挂端点（404）。
func TestReadiness_NoInitTasks_NoEndpoint(t *testing.T) {
	app := New("test-app")
	mux := app.Mux()
	req := httptest.NewRequest("GET", "/ks-readiness", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
