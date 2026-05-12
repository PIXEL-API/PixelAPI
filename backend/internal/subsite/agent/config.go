package agent

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr     string        `yaml:"listen_addr"`
	Version        string        `yaml:"version"`
	TrustedProxies []string      `yaml:"trusted_proxies"`
	Subsite        SubsiteConfig `yaml:"subsite"`
	Master         MasterConfig  `yaml:"master"`
	Queue          QueueConfig   `yaml:"queue"`
}

type SubsiteConfig struct {
	ID        string `yaml:"id"`
	PublicURL string `yaml:"public_url"`
}

type MasterConfig struct {
	BaseURL string `yaml:"base_url"`
	Secret  string `yaml:"secret"`
}

type QueueConfig struct {
	Path string `yaml:"path"`
}

func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		ListenAddr: ":8080",
		Version:    "dev",
		Queue: QueueConfig{
			Path: "subsite-usage.db",
		},
	}
	if strings.TrimSpace(path) != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}
	applyEnv(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return fmt.Errorf("listen_addr is required")
	}
	if strings.TrimSpace(c.Subsite.ID) == "" {
		return fmt.Errorf("subsite.id is required")
	}
	if strings.TrimSpace(c.Master.BaseURL) == "" {
		return fmt.Errorf("master.base_url is required")
	}
	if strings.TrimSpace(c.Master.Secret) == "" {
		return fmt.Errorf("master.secret is required")
	}
	if strings.TrimSpace(c.Queue.Path) == "" {
		return fmt.Errorf("queue.path is required")
	}
	c.Master.BaseURL = strings.TrimRight(strings.TrimSpace(c.Master.BaseURL), "/")
	return nil
}

func applyEnv(cfg *Config) {
	if value := strings.TrimSpace(os.Getenv("SUBSITE_LISTEN_ADDR")); value != "" {
		cfg.ListenAddr = value
	}
	if value := strings.TrimSpace(os.Getenv("SUBSITE_ID")); value != "" {
		cfg.Subsite.ID = value
	}
	if value := strings.TrimSpace(os.Getenv("SUBSITE_PUBLIC_URL")); value != "" {
		cfg.Subsite.PublicURL = value
	}
	if value := strings.TrimSpace(os.Getenv("SUBSITE_MASTER_URL")); value != "" {
		cfg.Master.BaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("SUBSITE_MASTER_SECRET")); value != "" {
		cfg.Master.Secret = value
	}
	if value := strings.TrimSpace(os.Getenv("SUBSITE_USAGE_QUEUE_PATH")); value != "" {
		cfg.Queue.Path = value
	}
	if value := strings.TrimSpace(os.Getenv("SUBSITE_VERSION")); value != "" {
		cfg.Version = value
	}
	if value := strings.TrimSpace(os.Getenv("SUBSITE_TRUSTED_PROXIES")); value != "" {
		cfg.TrustedProxies = splitCSV(value)
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
