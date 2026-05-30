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

	"xray-panel/internal/app"
	"xray-panel/internal/config"
	"xray-panel/internal/handler"
	"xray-panel/internal/service"
	"xray-panel/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("加载配置失败", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		slog.Error("创建数据目录失败", "error", err)
		os.Exit(1)
	}

	st := store.NewJSONStore(cfg.StatePath(), cfg.SocksPort, cfg.HTTPPort)
	xp := service.NewXrayProc(cfg.XrayBin, cfg.DataDir)

	a, err := app.New(app.Config{
		Store:                     st,
		XrayProc:                  xp,
		ConfigPath:                cfg.ConfigPath(),
		PanelPort:                 cfg.PanelPort,
		Password:                  cfg.PanelPassword,
		SubscriptionAllowInternal: cfg.SubscriptionAllowInternal,
	})
	if err != nil {
		slog.Error("初始化失败", "error", err)
		os.Exit(1)
	}

	a.Xray().Start(cfg.ConfigPath())

	srv := handler.NewServer(a)
	httpServer := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.PanelListen, cfg.PanelPort),
		Handler: srv.Routes(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info(fmt.Sprintf("面板已启动 http://%s:%d", cfg.PanelListen, cfg.PanelPort))
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("服务异常退出", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("正在关闭...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpServer.Shutdown(shutdownCtx)
	a.Xray().Stop()
	slog.Info("已关闭")
}
