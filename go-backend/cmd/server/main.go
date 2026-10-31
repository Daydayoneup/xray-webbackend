package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // scratch 镜像没有时区数据：通知时间、按天流量统计依赖 TZ

	"xray-panel/internal/app"
	"xray-panel/internal/config"
	"xray-panel/internal/handler"
	"xray-panel/internal/service"
	"xray-panel/internal/store"
)

// version 由构建参数注入：-ldflags "-X main.version=v1.2.3"
var version = "dev"

const usage = `xray-panel —— Xray-core Web 管理面板

用法：
  xray-panel                 启动面板（默认）
  xray-panel passwd [密码]    重置登录密码；不填则随机生成。面板运行中也可以执行，几秒内生效
  xray-panel install-xray    下载 / 更新官方 Xray-core 到数据目录的 bin/
  xray-panel version         显示版本

配置来自环境变量，也可以写在 /etc/xray-panel/config.env（每行 KEY=VALUE，PANEL_CONFIG 可指定其他路径）。
常用：PANEL_PORT（默认 2017）、PANEL_DATA_DIR（默认 /data/xray）、PANEL_PASSWORD（仅首次启动生效）、
XRAY_BIN、GH_PROXY（下载 Xray 的 GitHub 加速前缀）。
`

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "":
		serve()
	case "passwd", "password", "reset-password":
		os.Exit(passwd(os.Args[2:]))
	case "install-xray":
		os.Exit(installXray())
	case "version", "-v", "--version":
		fmt.Println("xray-panel", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "未知命令 %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func mustConfig() *config.Settings {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "加载配置失败:", err)
		os.Exit(1)
	}
	return cfg
}

func passwd(args []string) int {
	cfg := mustConfig()
	pw, random := "", false
	switch len(args) {
	case 0:
		pw, random = app.RandomPassword(), true
	case 1:
		pw = args[0]
	default:
		fmt.Fprintln(os.Stderr, "用法: xray-panel passwd [新密码]")
		return 2
	}
	if _, err := os.Stat(cfg.StatePath()); err != nil {
		fmt.Fprintf(os.Stderr, "数据目录 %s 中没有面板数据（%v）。\n请确认 PANEL_DATA_DIR 与面板服务一致（Docker 部署请用 docker exec 在容器内执行）。\n", cfg.DataDir, err)
		return 1
	}
	if err := app.WritePasswordReset(cfg.DataDir, pw, random); err != nil {
		fmt.Fprintln(os.Stderr, "重置失败:", err)
		return 1
	}
	if random {
		fmt.Println("新密码:", pw)
	} else {
		fmt.Println("新密码已设置")
	}
	fmt.Println("面板运行中会在 30 秒内（或下次登录时）生效，已登录的设备需要重新登录；面板未运行时在下次启动时生效。")
	return 0
}

func installXray() int {
	cfg := mustConfig()
	dir := cfg.XrayInstallDir()
	fmt.Println("正在下载官方 Xray-core 到", dir, "...")
	client := &http.Client{Timeout: 5 * time.Minute}
	get := func(u string) ([]byte, error) {
		resp, err := client.Get(u)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		var buf []byte
		buf, err = readAllLimit(resp, 200<<20)
		return buf, err
	}
	bin, err := service.DownloadXray(get, cfg.GHProxy, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "安装失败:", err)
		fmt.Fprintln(os.Stderr, "网络不通时可设置 GH_PROXY=https://<加速域名>/ 或 HTTPS_PROXY 后重试")
		return 1
	}
	fmt.Println("已安装", bin)
	if used, ok := app.ResolveXrayBin(cfg.XrayBin, dir); ok && used != bin {
		fmt.Printf("注意：面板当前会使用 %s（XRAY_BIN），如需使用刚安装的版本请设置 XRAY_BIN=%s\n", used, bin)
	}
	return 0
}

func serve() {
	cfg := mustConfig()
	if cfg.ConfigFile != "" {
		slog.Info("已读取配置文件", "path", cfg.ConfigFile)
	}
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		slog.Error("创建数据目录失败（可用 PANEL_DATA_DIR 指定其他目录）", "dir", cfg.DataDir, "error", err)
		os.Exit(1)
	}

	xrayBin, haveXray := app.ResolveXrayBin(cfg.XrayBin, cfg.XrayInstallDir())
	st := store.NewJSONStore(cfg.StatePath(), cfg.SocksPort, cfg.HTTPPort)
	xp := service.NewXrayProc(xrayBin, cfg.DataDir)

	a, err := app.New(app.Config{
		Store:                     st,
		XrayProc:                  xp,
		ConfigPath:                cfg.ConfigPath(),
		PanelPort:                 cfg.PanelPort,
		Password:                  cfg.PanelPassword,
		SubscriptionAllowInternal: cfg.SubscriptionAllowInternal,
		DataDir:                   cfg.DataDir,
		MetricsPort:               cfg.MetricsPort,
		XrayAssetDir:              cfg.XrayAssetDir,
		XrayBin:                   xrayBin,
		XrayInstallDir:            cfg.XrayInstallDir(),
		GHProxy:                   cfg.GHProxy,
		Version:                   version,
	})
	if err != nil {
		slog.Error("初始化失败", "error", err)
		os.Exit(1)
	}

	if haveXray {
		a.Xray().Start(cfg.ConfigPath())
	} else {
		go a.InstallXray() // 面板先启动，Xray 下载完成后自动启动
	}

	srv := handler.NewServer(a)
	httpServer := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.PanelListen, cfg.PanelPort),
		Handler: srv.Routes(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info(fmt.Sprintf("面板已启动 http://%s:%d", cfg.PanelListen, cfg.PanelPort), "version", version, "xray", xrayBin)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("服务异常退出", "error", err)
			os.Exit(1)
		}
	}()

	go a.RunScheduler(ctx, 5*time.Minute)
	go a.RunBackground(ctx)

	<-ctx.Done()
	slog.Info("正在关闭...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpServer.Shutdown(shutdownCtx)
	a.Xray().Stop()
	slog.Info("已关闭")
}
