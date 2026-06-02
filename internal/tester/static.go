package tester

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// RunStaticChecks 对项目目录执行静态检查，返回检查结果列表
func RunStaticChecks(projectDir string) []CheckResult {
	var results []CheckResult

	// 1. manifest 存在性
	manifestPath := filepath.Join(projectDir, "manifest.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		results = append(results, CheckResult{
			Name:    "manifest.yaml",
			Passed:  false,
			Message: fmt.Sprintf("读取失败: %v", err),
		})
		return results // 后续检查依赖 manifest，提前返回
	}
	results = append(results, CheckResult{Name: "manifest.yaml", Passed: true})

	// 2. manifest 格式校验
	manifest, err := kstypes.ParseAppSpec(data)
	if err != nil {
		results = append(results, CheckResult{
			Name:    "manifest 格式",
			Passed:  false,
			Message: err.Error(),
		})
		return results
	}
	if err := manifest.Validate(); err != nil {
		results = append(results, CheckResult{
			Name:    "manifest 格式",
			Passed:  false,
			Message: err.Error(),
		})
		return results
	}
	results = append(results, CheckResult{Name: "manifest 格式", Passed: true})

	// 3. 权限声明校验
	if len(manifest.Permissions) > 0 {
		registry := kstypes.DefaultPermissionRegistry()
		warnings, err := registry.Validate(manifest.Permissions)
		if err != nil {
			results = append(results, CheckResult{
				Name:    "权限声明",
				Passed:  false,
				Message: err.Error(),
			})
		} else if len(warnings) > 0 {
			msgs := make([]string, len(warnings))
			for i, w := range warnings {
				msgs[i] = w.Message
			}
			results = append(results, CheckResult{
				Name:    "权限声明",
				Passed:  true,
				Message: "警告: " + strings.Join(msgs, "; "),
			})
		} else {
			results = append(results, CheckResult{Name: "权限声明", Passed: true})
		}

		// 4. 高风险权限
		highRisk := registry.HighRiskPermissions(manifest.Permissions, 6)
		if len(highRisk) > 0 {
			results = append(results, CheckResult{
				Name:    "高风险权限",
				Passed:  false,
				Message: fmt.Sprintf("以下权限风险较高: %s", strings.Join(highRisk, ", ")),
			})
		}
	} else {
		results = append(results, CheckResult{Name: "权限声明", Passed: true})
	}

	return results
}
