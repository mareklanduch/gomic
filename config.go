package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is what the meter remembers between runs.
type Config struct {
	X       int    `json:"x"`
	Y       int    `json:"y"`
	HasPos  bool   `json:"hasPos"`
	Topmost bool   `json:"topmost"`
	Source  string `json:"source"` // "input" or "output"
}

// DefaultConfig is used on first run and when the saved file is unusable.
func DefaultConfig() Config { return Config{Topmost: true, Source: "input"} }

// ConfigPath returns the per-user settings file location.
func ConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "gomic", "config.json")
}

// LoadConfig reads path, falling back to defaults on any problem.
func LoadConfig(path string) Config {
	c := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	if json.Unmarshal(data, &c) != nil {
		return DefaultConfig()
	}
	if c.Source != "input" && c.Source != "output" {
		c.Source = "input"
	}
	return c
}

// Save writes the config, creating the folder if needed.
func (c Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, data)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
