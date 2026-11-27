package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is what the meter remembers between runs.
type Config struct {
	X       int  `json:"x"`
	Y       int  `json:"y"`
	HasPos  bool `json:"hasPos"`
	Topmost bool `json:"topmost"`

	// Which meters are visible. At least one always is.
	ShowInput  bool `json:"showInput"`
	ShowOutput bool `json:"showOutput"`

	// The chosen devices. An empty ID follows the system default; the name
	// is kept so a device that is not connected can still be named.
	InputDevice      string `json:"inputDevice"`
	InputDeviceName  string `json:"inputDeviceName"`
	OutputDevice     string `json:"outputDevice"`
	OutputDeviceName string `json:"outputDeviceName"`
}

// DefaultConfig is used on first run and when the saved file is unusable.
func DefaultConfig() Config { return Config{Topmost: true, ShowInput: true} }

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
	// Files written before the two meters could be shown together have no
	// show flags, only a single source.
	legacy := struct {
		Source     string `json:"source"`
		ShowInput  *bool  `json:"showInput"`
		ShowOutput *bool  `json:"showOutput"`
	}{}
	if json.Unmarshal(data, &c) != nil || json.Unmarshal(data, &legacy) != nil {
		return DefaultConfig()
	}
	if legacy.ShowInput == nil && legacy.ShowOutput == nil {
		c.ShowInput, c.ShowOutput = legacy.Source != "output", legacy.Source == "output"
	}
	if !c.ShowInput && !c.ShowOutput {
		c.ShowInput = true
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
