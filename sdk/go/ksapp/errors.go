package ksapp

import (
	"errors"
	"fmt"
)

// BizError 业务错误码包装。Code 区分错误语义（ERR_VALIDATE / ERR_APPLY /
// ERR_SCHEMA / ERR_STORE / ERR_DECRYPT / ERR_NOT_CONFIGURED / ...），Err
// 是底层错误（OnValidate / OnApply / json.Marshal / keystore 等返回的
// error）。
//
// 设计目标：让 endpoint handler 可以用 errors.As(err, &be
// *BizError) 取出 Code 做分支判断（决定 HTTP 状态码 / 日志等级），同时
// 保留 Unwrap 链路便于日志或 errors.Is 比对底层错误。
//
// .Error() 输出 "[CODE] inner-msg"（Err == nil 时只输出 "[CODE]"），与
// 升级前 errBizf 的字符串格式兼容，现有 strings.Contains 风格测试不变。
//
// 规范源：标准错误码 enum。
type BizError struct {
	Code string
	Err  error
}

// Error 输出 "[CODE] inner-msg" 字符串。Err == nil 时只输出 "[CODE]"。
func (e *BizError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("[%s]", e.Code)
	}
	return fmt.Sprintf("[%s] %v", e.Code, e.Err)
}

// Unwrap 返回底层 Err，让 errors.Is / errors.As 沿 Unwrap 链回溯。
func (e *BizError) Unwrap() error { return e.Err }

// errBizf 把一个错误码 + 底层 error 包装成 *BizError。
//
// 用法：
//
//	return errBizf("ERR_VALIDATE", err)
//	return errBizf("ERR_STORE", err)
//
// 注意：调用方应确保 err 不含敏感字节（DEK / privkey / plaintext）。
//
// 返回类型是 *BizError（实现 error 接口），调用点直接 return err 即可；
// 上层调用方用 errors.As(err, &be *BizError) 提取 Code 分支。
func errBizf(code string, err error) *BizError {
	return &BizError{Code: code, Err: err}
}

// ErrNotConfigured 是 "配置未提供 / 凭证缺失" 的哨兵 error。
//
// 调用方用 errors.Is 断言；消息内容由 NewErrNotConfigured 包装。
// 字符串格式始终以 "ERR_NOT_CONFIGURED: " 起（黑盒等价）。
//
// 在 SDK 中统一定义，避免各应用各自重复定义 ErrNotConfigured。
var ErrNotConfigured = errors.New("ERR_NOT_CONFIGURED")

// NewErrNotConfigured 返回一个 wrap ErrNotConfigured 的 error，字符串格式：
//
//	ERR_NOT_CONFIGURED: <scope> <detail>
//
// scope 通常是 MCP 的短名（"minimax" / "email" / "git-github" 等）；
// detail 用 fmt.Sprintf 格式化 format + args。
//
// 示例：
//
//	return ksapp.NewErrNotConfigured("minimax", "api key 未填")
//	return ksapp.NewErrNotConfigured("email", "SMTP host %q 未配置", host)
func NewErrNotConfigured(scope, format string, args ...any) error {
	detail := fmt.Sprintf(format, args...)
	return fmt.Errorf("ERR_NOT_CONFIGURED: %s %s: %w", scope, detail, ErrNotConfigured)
}
