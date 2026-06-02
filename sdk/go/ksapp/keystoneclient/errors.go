// Package keystoneclient 提供调 Keystone 控制面 API 的 Go 客户端。
//
// 当前仅含 SelfClient（应用启动期自取托管资源凭证）；未来若 SDK 需要
// 更多 keystone-related 客户端（admin/events/metrics），放在同一包下。
//
// 与 Python 端 ks_app.keystone_client 平行设计，go 包命名按 Go 习惯
// 去下划线为 keystoneclient。
package keystoneclient

import "errors"

// ErrFetchFailed 是 SelfClient 自取托管资源失败的哨兵错误。
//
// 无论 HTTP 错误码、网络错、响应解析错、业务 code != 0，全部用 fmt.Errorf
// 包装本哨兵返回；调用方（ksapp.App 启动期 / ksapp fetch-env CLI）用
// errors.Is(err, keystoneclient.ErrFetchFailed) 统一断言即可。
//
// spec: managed resources self-fetch contract
var ErrFetchFailed = errors.New("ERR_KEYSTONE_SELF_FETCH: 自取托管资源失败")
