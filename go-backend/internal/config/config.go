package config

import (
	"path/filepath"

	"github.com/caarlos0/env/v11"
)

type Settings struct {
	PanelPort                 int     `env:"PANEL_PORT" envDefault:"2017"`
	PanelListen               string  `env:"PANEL_LISTEN" envDefault:"0.0.0.0"`
	DataDir                   string  `env:"PANEL_DATA_DIR" envDefault:"/data/xray"`
	XrayBin                   string  `env:"XRAY_BIN" envDefault:"/usr/local/bin/xray"`
	PanelPassword             *string `env:"PANEL_PASSWORD"`
	SocksPort                 int     `env:"SOCKS_PORT" envDefault:"10808"`
	HTTPPort                  int     `env:"HTTP_PORT" envDefault:"10809"`
	SubscriptionAllowInternal bool    `env:"SUBSCRIPTION_ALLOW_INTERNAL"`
	// MetricsPort 是 Xray metrics（流量统计/节点探测结果）的本机端口，0 表示关闭
	MetricsPort int `env:"METRICS_PORT" envDefault:"10085"`
	// XrayAssetDir 是镜像自带的 geo 数据目录；面板更新后的数据放在 $PANEL_DATA_DIR/geo 并优先使用
	XrayAssetDir string `env:"XRAY_LOCATION_ASSET" envDefault:"/usr/local/share/xray"`
}

func Load() (*Settings, error) {
	cfg := &Settings{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *Settings) StatePath() string {
	return filepath.Join(s.DataDir, "panel.json")
}

func (s *Settings) ConfigPath() string {
	return filepath.Join(s.DataDir, "config.json")
}
