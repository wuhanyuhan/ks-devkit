package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AuthType 区分凭证形态：
//   - AuthTypeUser：开发者交互登录（user JWT）
//   - AuthTypePAT：Personal Access Token（ksh_pat_*），用于 CI / 自动化
type AuthType string

const (
	AuthTypeUser AuthType = "user"
	AuthTypePAT  AuthType = "pat"
)

// Credentials 是 ks-devkit 的本地凭证结构。
// 兼容三种来源：
//  1. 旧 v1 schema（无 auth_type）→ 视作 user
//  2. 新 user 显式声明
//  3. 新 PAT（ks auth login --token 写入）
//
// 写盘永远使用新 schema（含 auth_type），但读取容忍旧 v1。
type Credentials struct {
	AuthType      AuthType `json:"auth_type"`
	AccessToken   string   `json:"access_token"`
	RefreshToken  string   `json:"refresh_token,omitempty"`
	Email         string   `json:"email,omitempty"`
	PublisherSlug string   `json:"publisher_slug,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	CreatedAt     string   `json:"created_at,omitempty"`
}

// credentialsAlias 用于 UnmarshalJSON 解出原始字段后再补 default。
type credentialsAlias Credentials

// UnmarshalJSON 在缺失 auth_type 时回填为 user，保持旧 v1 schema 兼容。
func (c *Credentials) UnmarshalJSON(data []byte) error {
	var tmp credentialsAlias
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	if tmp.AuthType == "" {
		tmp.AuthType = AuthTypeUser
	}
	*c = Credentials(tmp)
	return nil
}

// LoadCredentials 从指定路径读取凭证。文件不存在时返回带 path 提示的错误。
func LoadCredentials(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}
	var cred Credentials
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, err
	}
	return &cred, nil
}

// SaveCredentials 始终用新 schema 写入（含 auth_type），权限 0600。
func SaveCredentials(path string, cred *Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if cred.AuthType == "" {
		cred.AuthType = AuthTypeUser
	}
	data, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600) // 0600: 仅当前用户读写
}

// DeleteCredentials 删除凭证文件，文件不存在不视为错误（NotExist 由调用方判）。
func DeleteCredentials(path string) error {
	return os.Remove(path)
}

// DefaultCredentialsPath 返回 ~/.ks/credentials.json。
func DefaultCredentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ks", "credentials.json")
}

// LoadFromEnvOrFile 按优先级加载凭证：
//  1. KS_HUB_TOKEN 环境变量（必须是 PAT，ksh_pat_ 前缀）
//  2. fallback 到 credentialsPath 文件
//
// env 模式下 PublisherSlug / Scopes 留空，由调用方在需要时调 hub whoami 填充
// （结果只缓存到当前进程内存，不写到磁盘）。
func LoadFromEnvOrFile(credentialsPath string) (*Credentials, error) {
	if envToken := os.Getenv("KS_HUB_TOKEN"); envToken != "" {
		if !strings.HasPrefix(envToken, "ksh_pat_") {
			return nil, ErrInvalidEnvToken
		}
		return &Credentials{
			AuthType:    AuthTypePAT,
			AccessToken: envToken,
		}, nil
	}
	cred, err := LoadCredentials(credentialsPath)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") {
			return nil, ErrCredentialsNotFound
		}
		return nil, err
	}
	return cred, nil
}
