package ksapp

import (
	"os"
	"strconv"
)

// AppConfig 保存 App 运行时配置（当前仅包含监听端口）。
type AppConfig struct {
	Port int
}

// loadAppConfig 从环境变量加载配置。
// 当前支持：
//   - KS_APP_PORT: HTTP 监听端口，默认 8080；非法值时回退到 8080。
func loadAppConfig() *AppConfig {
	port := 8080
	if v := os.Getenv("KS_APP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			port = p
		}
	}
	return &AppConfig{Port: port}
}
