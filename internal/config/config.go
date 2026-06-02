package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HubURL string `yaml:"hub_url"`
}

// DefaultHubURL 是生产 hub 服务的默认地址。
// dev/staging 切换通过 ~/.ks/config.yaml 的 hub_url 字段或 KS_HUB_URL env var 覆盖。
const DefaultHubURL = "https://ks-hub.yuhaninfo.cn"

func DefaultConfig() Config {
	return Config{
		HubURL: DefaultHubURL,
	}
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 允许 KS_HUB_URL 环境变量覆盖（CI / 测试场景）
			if envURL := os.Getenv("KS_HUB_URL"); envURL != "" {
				cfg.HubURL = envURL
			}
			return &cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	// 允许 KS_HUB_URL 环境变量覆盖（CI / 测试场景）
	if envURL := os.Getenv("KS_HUB_URL"); envURL != "" {
		cfg.HubURL = envURL
	}
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// DefaultConfigPath 返回 ~/.ks/config.yaml
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ks", "config.yaml")
}
