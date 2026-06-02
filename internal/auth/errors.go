package auth

import "errors"

// ErrInvalidEnvToken 表示 KS_HUB_TOKEN 环境变量不是合法 PAT 形态。
// 设计上 env 注入仅支持 PAT（ksh_pat_ 前缀），user JWT 不允许进入 env。
var ErrInvalidEnvToken = errors.New("KS_HUB_TOKEN must be a PAT (prefix ksh_pat_)")

// ErrCredentialsNotFound 表示既无 env 也无 credentials.json 文件可读。
var ErrCredentialsNotFound = errors.New("no credentials: set KS_HUB_TOKEN or run 'ks auth login'")
