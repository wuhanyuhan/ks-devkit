package ksapp

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestBizError_Error_WithInner(t *testing.T) {
	t.Parallel()
	inner := errors.New("validate failed")
	be := errBizf("ERR_VALIDATE", inner)
	got := be.Error()
	want := "[ERR_VALIDATE] validate failed"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestBizError_Error_NilInner(t *testing.T) {
	t.Parallel()
	be := &BizError{Code: "ERR_INTERNAL", Err: nil}
	got := be.Error()
	want := "[ERR_INTERNAL]"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestBizError_AsExtractsCode(t *testing.T) {
	t.Parallel()
	inner := errors.New("apply boom")
	var err error = errBizf("ERR_APPLY", inner)

	var be *BizError
	if !errors.As(err, &be) {
		t.Fatal("errors.As(*BizError) 失败")
	}
	if be.Code != "ERR_APPLY" {
		t.Errorf("be.Code = %q, want ERR_APPLY", be.Code)
	}
	if be.Err != inner {
		t.Errorf("be.Err 不是 inner")
	}
}

func TestBizError_IsUnwrapsToInner(t *testing.T) {
	t.Parallel()
	inner := errors.New("schema fail")
	err := errBizf("ERR_SCHEMA", inner)
	if !errors.Is(err, inner) {
		t.Error("errors.Is(inner) 应通过 Unwrap 链命中")
	}
}

func TestBizError_AsAcrossWrap(t *testing.T) {
	t.Parallel()
	inner := errors.New("store io fail")
	be := errBizf("ERR_STORE", inner)
	wrapped := fmt.Errorf("调用 handleSave: %w", be)

	var got *BizError
	if !errors.As(wrapped, &got) {
		t.Fatal("errors.As 应能穿透 fmt.Errorf %w 包装")
	}
	if got.Code != "ERR_STORE" {
		t.Errorf("got.Code = %q", got.Code)
	}
}

// ErrNotConfigured 哨兵 + NewErrNotConfigured 构造器。
//
// 契约：
//   - 字符串前缀恒为 "ERR_NOT_CONFIGURED: "（与 5 MCP 仓历史字节等价）
//   - errors.Is(err, ksapp.ErrNotConfigured) 可断言
//   - 构造器签名：NewErrNotConfigured(scope, format string, args ...any) error
func TestErrNotConfigured_Sentinel(t *testing.T) {
	t.Parallel()
	err := NewErrNotConfigured("minimax", "api key missing")

	// 1. errors.Is 可断言
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("errors.Is: 未匹配 ErrNotConfigured 哨兵")
	}

	// 2. 字符串前缀契约
	if !strings.HasPrefix(err.Error(), "ERR_NOT_CONFIGURED: ") {
		t.Errorf("错误前缀: want \"ERR_NOT_CONFIGURED: \", got %q", err.Error())
	}

	// 3. scope + detail 可读
	if !strings.Contains(err.Error(), "minimax") {
		t.Errorf("错误消息缺 scope; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "api key missing") {
		t.Errorf("错误消息缺 detail; got %q", err.Error())
	}
}

func TestErrNotConfigured_Format(t *testing.T) {
	t.Parallel()
	err := NewErrNotConfigured("email", "SMTP host %q 未配置", "smtp.example.com")
	if !strings.Contains(err.Error(), `"smtp.example.com"`) {
		t.Errorf("format 参数未展开; got %q", err.Error())
	}
}
