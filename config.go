package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	ListenAddr string            `json:"listen_addr"`
	Port       int               `json:"port"`
	AccessKey  string            `json:"access_key"`
	IPSetNames map[string]string `json:"ipset_names"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	if err := rejectLegacyShape(data); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func rejectLegacyShape(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.ListenAddr == "" {
		c.ListenAddr = "192.168.1.1"
	}
	if c.Port == 0 {
		c.Port = 8080
	}
}

func (c *Config) validate() error {
	if strings.TrimSpace(c.AccessKey) == "" {
		return fmt.Errorf("config: access_key is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: port %d out of range", c.Port)
	}
	if len(c.IPSetNames) == 0 {
		return fmt.Errorf("config: ipset_names must contain at least one entry")
	}
	for t, name := range c.IPSetNames {
		if strings.TrimSpace(t) == "" {
			return fmt.Errorf("config: ipset_names has an empty type key")
		}
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("config: ipset_names[%q] must not be empty", t)
		}
	}
	return nil
}

func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.ListenAddr, c.Port)
}
