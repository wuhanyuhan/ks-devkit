// Package exitcode 集中定义 ks-devkit CLI 的 7 值退出码协议。
// 维持稳定性：值不可重排、不可删除，只能新增。
package exitcode

import "errors"

// 退出码常量。修改任何值前请先看 公开 CLI exit code contract。
const (
	Success          = 0 // 成功
	Generic          = 1 // 通用失败（panic / 未分类业务异常）
	AuthOrPermission = 2 // 认证 / 权限错误
	ClientConfig     = 3 // 客户端配置错误（manifest / preflight / build）
	Network          = 4 // 网络 / 上传错误（建议 retry）
	DuplicateVersion = 5 // 重复版本
	ReviewRejected   = 6 // fast-track 路径下 review 被拒
)

// ExitError 包装任意 error，携带 ks-devkit 退出码语义。
// CLI 入口（cmd/ks/main.go）从顶层 error 提取此 code 调用 os.Exit。
type ExitError struct {
	code int
	err  error
}

// Wrap 用指定 exit code 包装 err。err 不能为 nil；调用方需自行判 nil。
func Wrap(err error, code int) *ExitError {
	return &ExitError{code: code, err: err}
}

func (e *ExitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *ExitError) Unwrap() error { return e.err }

// Code 返回包装时记录的退出码。
func (e *ExitError) Code() int { return e.code }

// Extract 从任意 error 提取退出码：
// - nil → Success
// - *ExitError → 其 Code()
// - 其它 → Generic
func Extract(err error) int {
	if err == nil {
		return Success
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code()
	}
	return Generic
}
