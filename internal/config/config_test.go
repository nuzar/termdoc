package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomePrefersTermdocHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TERMDOC_HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg"))
	got, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("Home() = %q, want %q", got, dir)
	}
}

func TestHomeUsesXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TERMDOC_HOME", "")
	t.Setenv("XDG_DATA_HOME", dir)
	got, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "termdoc"); got != want {
		t.Errorf("Home() = %q, want %q", got, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "auto" {
		t.Errorf("default theme = %q, want auto", cfg.Theme)
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	content := "proxy = \"http://127.0.0.1:50000\"\npager = \"less -R\"\ntheme = \"dark\"\nwidth = 90\nno_pager = true\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proxy != "http://127.0.0.1:50000" || cfg.Pager != "less -R" || cfg.Theme != "dark" || cfg.Width != 90 || !cfg.NoPager {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestProxyValueEnvWins(t *testing.T) {
	t.Setenv("TERMDOC_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	cfg := &Config{Proxy: "http://from-config:1"}
	if got := cfg.ProxyValue(); got != "http://from-config:1" {
		t.Errorf("ProxyValue() = %q", got)
	}
	t.Setenv("HTTPS_PROXY", "http://from-env:2")
	if got := cfg.ProxyValue(); got != "http://from-env:2" {
		t.Errorf("ProxyValue() = %q, want env value", got)
	}
}
