package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/caarlos0/env/v11"
)

// DefaultConfigFile 是可选的配置文件（一键安装脚本生成），每行 KEY=VALUE；已设置的环境变量优先。
// 面板服务和命令行子命令（如 xray-panel passwd）读同一个文件，因此不用在命令行重复指定数据目录。
const DefaultConfigFile = "/etc/xray-panel/config.env"

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
	// GHProxy 是下载 GitHub Release（自动安装 Xray）时使用的加速前缀，如 https://gh-proxy.com/
	GHProxy string `env:"GH_PROXY"`

	// ConfigFile 是实际读取的配置文件路径（不存在时为空）
	ConfigFile string
}

func Load() (*Settings, error) {
	path := os.Getenv("PANEL_CONFIG")
	if path == "" {
		path = DefaultConfigFile
	}
	loaded, err := loadEnvFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Settings{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	if loaded {
		cfg.ConfigFile = path
	}
	// 空的 PANEL_PASSWORD（如 compose 中 ${PANEL_PASSWORD:-}）视为未设置
	if cfg.PanelPassword != nil && *cfg.PanelPassword == "" {
		cfg.PanelPassword = nil
	}
	return cfg, nil
}

// loadEnvFile 把文件中的 KEY=VALUE 设置为环境变量（已存在的环境变量不覆盖）。文件不存在不算错误。
func loadEnvFile(path string) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if _, set := os.LookupEnv(k); !set && k != "" {
			os.Setenv(k, v)
		}
	}
	return true, sc.Err()
}

func (s *Settings) StatePath() string {
	return filepath.Join(s.DataDir, "panel.json")
}

func (s *Settings) ConfigPath() string {
	return filepath.Join(s.DataDir, "config.json")
}

// PasswordResetPath 是 xray-panel passwd 写入的待生效密码，面板运行中会在几秒内或下次登录前读取。
func (s *Settings) PasswordResetPath() string { return filepath.Join(s.DataDir, PasswordResetFile) }

// InitialPasswordPath 保存首次启动随机生成的密码，修改密码后删除。
func (s *Settings) InitialPasswordPath() string { return filepath.Join(s.DataDir, InitialPasswordFile) }

// XrayInstallDir 是面板自动安装 Xray 的目录（xray + geoip.dat + geosite.dat）。
func (s *Settings) XrayInstallDir() string { return filepath.Join(s.DataDir, "bin") }

const (
	PasswordResetFile   = "password-reset.json"
	InitialPasswordFile = "initial-password.txt"
)
