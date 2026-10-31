package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{"PANEL_PORT", "PANEL_LISTEN", "PANEL_DATA_DIR",
		"XRAY_BIN", "PANEL_PASSWORD", "SOCKS_PORT", "HTTP_PORT",
		"SUBSCRIPTION_ALLOW_INTERNAL"} {
		os.Unsetenv(k)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PanelPort != 2017 {
		t.Errorf("PanelPort = %d, want 2017", cfg.PanelPort)
	}
	if cfg.SocksPort != 10808 {
		t.Errorf("SocksPort = %d, want 10808", cfg.SocksPort)
	}
	if cfg.HTTPPort != 10809 {
		t.Errorf("HTTPPort = %d, want 10809", cfg.HTTPPort)
	}
	if cfg.PanelListen != "0.0.0.0" {
		t.Errorf("PanelListen = %s, want 0.0.0.0", cfg.PanelListen)
	}
	if cfg.PanelPassword != nil {
		t.Errorf("PanelPassword should be nil, got %v", cfg.PanelPassword)
	}
	if cfg.SubscriptionAllowInternal {
		t.Errorf("SubscriptionAllowInternal should be false")
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("PANEL_PORT", "3000")
	os.Setenv("PANEL_PASSWORD", "secret")
	os.Setenv("SUBSCRIPTION_ALLOW_INTERNAL", "1")
	defer os.Unsetenv("PANEL_PORT")
	defer os.Unsetenv("PANEL_PASSWORD")
	defer os.Unsetenv("SUBSCRIPTION_ALLOW_INTERNAL")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PanelPort != 3000 {
		t.Errorf("PanelPort = %d, want 3000", cfg.PanelPort)
	}
	if cfg.PanelPassword == nil || *cfg.PanelPassword != "secret" {
		t.Errorf("PanelPassword = %v, want 'secret'", cfg.PanelPassword)
	}
	if !cfg.SubscriptionAllowInternal {
		t.Errorf("SubscriptionAllowInternal should be true")
	}
}

func TestPaths(t *testing.T) {
	cfg := &Settings{DataDir: "/data/xray"}
	if cfg.StatePath() != "/data/xray/panel.json" {
		t.Errorf("StatePath = %s", cfg.StatePath())
	}
	if cfg.ConfigPath() != "/data/xray/config.json" {
		t.Errorf("ConfigPath = %s", cfg.ConfigPath())
	}
}

func TestLoadConfigFile(t *testing.T) {
	path := t.TempDir() + "/config.env"
	os.WriteFile(path, []byte("# 注释\nPANEL_PORT=4000\nexport PANEL_DATA_DIR=\"/var/lib/xp\"\nHTTP_PORT='1234'\nbad line\n"), 0600)
	t.Setenv("PANEL_CONFIG", path)
	t.Setenv("HTTP_PORT", "5555") // 环境变量优先
	os.Unsetenv("PANEL_PORT")
	os.Unsetenv("PANEL_DATA_DIR")
	t.Cleanup(func() { os.Unsetenv("PANEL_PORT"); os.Unsetenv("PANEL_DATA_DIR") })
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PanelPort != 4000 || cfg.DataDir != "/var/lib/xp" || cfg.HTTPPort != 5555 || cfg.ConfigFile != path {
		t.Fatalf("got %+v", cfg)
	}
}

func TestEmptyPasswordIsUnset(t *testing.T) {
	t.Setenv("PANEL_CONFIG", t.TempDir()+"/none.env")
	t.Setenv("PANEL_PASSWORD", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PanelPassword != nil {
		t.Fatal("empty PANEL_PASSWORD should be nil")
	}
}
