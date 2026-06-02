package tester

import "fmt"

// CheckResult 单项检查结果
type CheckResult struct {
	Name    string
	Passed  bool
	Message string
}

// Report 汇总打印检查结果，返回失败数
func Report(phase string, results []CheckResult) int {
	fmt.Printf("\n── %s ──\n", phase)
	failed := 0
	for _, r := range results {
		if r.Passed {
			fmt.Printf("  ✓ %s\n", r.Name)
		} else {
			fmt.Printf("  ✗ %s: %s\n", r.Name, r.Message)
			failed++
		}
	}
	return failed
}
