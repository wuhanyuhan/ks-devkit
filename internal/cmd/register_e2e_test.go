package cmd

import (
	"os"
	"testing"
)

// TestRegister_E2E 真机联调：需本地 ks dev 跑着支持 clean-break 注册语义的 keystone-dev 镜像。
// 当前 docker-compose pin 的 keystone-dev:0.3.0 不带 clean-break 注册语义
// （去前缀派生 / decision_mode→guardrail / 能力注册）。故默认 skip；待 keystone 侧发 clean-break
// dev 镜像 + bump docker-compose tag 后，置 KS_E2E_KEYSTONE=1 手动跑。绝不假装 e2e 已通。
func TestRegister_E2E(t *testing.T) {
	if os.Getenv("KS_E2E_KEYSTONE") != "1" {
		t.Skip("e2e 联调依赖支持 clean-break 注册语义的 keystone-dev 镜像；设 KS_E2E_KEYSTONE=1 手动跑")
	}
	// 真机步骤（手动 / CI 接入后启用）：
	//   1. ks dev（拉起 clean-break keystone-dev）
	//   2. 在某 app 目录 go run .（:8080）
	//   3. runRegister → 断言能力已注册（GET /v1/admin/capabilities?... 命中 canonical=<app_id>.hello）
	//   4. runRefreshMeta → 幂等（连跑两次均成功）
	t.Fatal("e2e 主体待 clean-break dev 镜像就绪后填充")
}
