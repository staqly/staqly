package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

const (
	defaultURL     = "http://localhost:3333"
	defaultProfile = "default"
)

// Profile is one named server + credential pair.
type Profile struct {
	URL string `json:"url"`
	Key string `json:"key,omitempty"`
}

// Config is ~/.config/staqly/config.json (mode 0600).
type Config struct {
	Current  string              `json:"current,omitempty"`
	Profiles map[string]*Profile `json:"profiles"`
}

// configPath honours STAQLY_CONFIG, then XDG_CONFIG_HOME, then ~/.config.
func configPath(getenv func(string) string) string {
	if p := getenv("STAQLY_CONFIG"); p != "" {
		return p
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "staqly", "config.json")
	}
	home := getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".config", "staqly", "config.json")
}

func loadConfig(path string) (*Config, error) {
	cfg := &Config{Profiles: map[string]*Profile{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config %s is not valid JSON: %w", path, err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*Profile{}
	}
	return cfg, nil
}

// insecurePerms reports whether a config file holding keys is readable by others.
func insecurePerms(path string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.Mode().Perm()&0o077 != 0
}

// save writes atomically with 0600 permissions (directory 0700).
func (c *Config) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (c *Config) names() []string {
	out := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// maskKey shows only the key prefix: stk_abcd...
func maskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "****"
	}
	return k[:8] + "..."
}
