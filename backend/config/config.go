package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Hotkey              string    `yaml:"hotkey" json:"hotkey"`
	OBS                 OBSConfig `yaml:"obs" json:"obs"`
	DiscordWebhookURL   string    `yaml:"discord_webhook_url" json:"discord_webhook_url"`
	ReplayBufferSeconds int       `yaml:"replay_buffer_seconds" json:"replay_buffer_seconds"`
	OutputDir           string    `yaml:"output_dir" json:"output_dir"`
	MaxStorageGB        float64   `yaml:"max_storage_gb" json:"max_storage_gb"`
	Port                int       `yaml:"port" json:"port"`
}

type OBSConfig struct {
	Path     string `yaml:"path" json:"path"`
	Port     int    `yaml:"port" json:"port"`
	Password string `yaml:"password" json:"password"`
}

func defaultConfig() *Config {
	return &Config{
		Hotkey:              "F9",
		OBS:                 OBSConfig{Port: 4455},
		ReplayBufferSeconds: 30,
		OutputDir:           "captures",
		MaxStorageGB:        10,
		Port:                8765,
	}
}

func applyDefaults(cfg *Config) {
	if cfg.Hotkey == "" {
		cfg.Hotkey = "F9"
	}
	if cfg.OBS.Port == 0 {
		cfg.OBS.Port = 4455
	}
	if cfg.ReplayBufferSeconds == 0 {
		cfg.ReplayBufferSeconds = 30
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = "captures"
	}
	if cfg.MaxStorageGB == 0 {
		cfg.MaxStorageGB = 10
	}
	if cfg.Port == 0 {
		cfg.Port = 8765
	}
}

func LoadConfig(path string) (*Config, error) {
	if path == "" {
		return nil, errors.New("config path is required")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := defaultConfig()
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}
	applyDefaults(cfg)
	return cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	if path == "" {
		return errors.New("config path is required")
	}
	if cfg == nil {
		cfg = defaultConfig()
	}
	applyDefaults(cfg)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
