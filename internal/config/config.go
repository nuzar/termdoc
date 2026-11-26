// Package config handles termdoc's data directory and user configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the contents of $TERMDOC_HOME/config.toml.
type Config struct {
	// Proxy is an http:// or socks5:// proxy used for downloading docs.
	// Standard HTTPS_PROXY/HTTP_PROXY environment variables are also honored.
	Proxy string `toml:"proxy"`
	// Pager is the command used to page long documents. Empty means auto.
	Pager string `toml:"pager"`
	// Theme is the rendering style: auto, dark, light, notty, ascii, dracula...
	Theme string `toml:"theme"`
	// Width forces the document wrap width. 0 means auto.
	Width int `toml:"width"`
	// NoPager disables paging when true.
	NoPager bool `toml:"no_pager"`
}

// Home returns the termdoc data directory.
//
// Precedence: $TERMDOC_HOME, $XDG_DATA_HOME/termdoc, platform default.
func Home() (string, error) {
	if h := os.Getenv("TERMDOC_HOME"); h != "" {
		return expand(h)
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "termdoc"), nil
	}
	switch runtime.GOOS {
	case "windows":
		if x := os.Getenv("LOCALAPPDATA"); x != "" {
			return filepath.Join(x, "termdoc"), nil
		}
	case "darwin":
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, "Library", "Application Support", "termdoc"), nil
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".local", "share", "termdoc"), nil
	}
	return "", errors.New("cannot determine data directory; set TERMDOC_HOME")
}

func expand(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(h, p[1:])
	}
	return filepath.Abs(p)
}

// Load reads config.toml from home. A missing file is not an error.
func Load(home string) (*Config, error) {
	cfg := &Config{Theme: "auto"}
	path := filepath.Join(home, "config.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// Path returns the config file path for home.
func Path(home string) string { return filepath.Join(home, "config.toml") }

// Proxy returns the effective proxy: env vars win over the config file.
func (c *Config) ProxyValue() string {
	for _, k := range []string{"TERMDOC_PROXY", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return strings.TrimSpace(c.Proxy)
}
