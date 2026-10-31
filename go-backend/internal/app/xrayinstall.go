package app

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"xray-panel/internal/service"
)

// XrayInstallStatus 是自动安装 Xray 的进度，显示在顶栏。
type XrayInstallStatus struct {
	State string `json:"state"` // "" 不需要安装 | installing | failed | done
	Error string `json:"error,omitempty"`
}

// ResolveXrayBin 决定使用哪个 Xray：XRAY_BIN → 自动安装目录 → PATH 中的 xray。
// 都没有时返回自动安装目录中的路径和 false，由 InstallXray 下载到这里。
func ResolveXrayBin(configured, installDir string) (string, bool) {
	exists := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	}
	if configured != "" && exists(configured) {
		return configured, true
	}
	local := filepath.Join(installDir, service.XrayBinName(runtime.GOOS))
	if installDir != "" && exists(local) {
		return local, true
	}
	if p, err := exec.LookPath("xray"); err == nil {
		return p, true
	}
	if installDir == "" {
		return configured, false
	}
	return local, false
}

func (a *App) XrayInstall() XrayInstallStatus {
	a.installMu.Lock()
	defer a.installMu.Unlock()
	return a.install
}

// InstallXray 下载官方 Xray 到 XrayInstallDir 并启动。已在安装中时直接返回。
// 直连失败时经本机代理重试（首次安装时 Xray 还没运行，只能直连；可设 HTTPS_PROXY 或 GH_PROXY）。
func (a *App) InstallXray() error {
	a.installMu.Lock()
	if a.install.State == "installing" {
		a.installMu.Unlock()
		return fmt.Errorf("正在安装 Xray")
	}
	a.install = XrayInstallStatus{State: "installing"}
	a.installMu.Unlock()

	slog.Info("未找到 Xray，正在从 GitHub 下载官方版本", "dir", a.config.XrayInstallDir)
	start := time.Now()
	bin, err := service.DownloadXray(a.download, a.config.GHProxy, a.config.XrayInstallDir)

	a.installMu.Lock()
	if err != nil {
		a.install = XrayInstallStatus{State: "failed", Error: err.Error()}
	} else {
		a.install = XrayInstallStatus{State: "done"}
	}
	a.installMu.Unlock()
	if err != nil {
		slog.Error("自动安装 Xray 失败（网络问题可在面板顶栏重试；或设置 GH_PROXY / HTTPS_PROXY 后重启面板；也可手动安装并设置 XRAY_BIN）", "error", err)
		return err
	}
	if bin != a.config.XrayBin {
		slog.Warn("Xray 安装路径与预期不同", "bin", bin, "want", a.config.XrayBin)
	}
	slog.Info("Xray 安装完成", "bin", bin, "elapsed", time.Since(start).Round(time.Second))
	a.xray.SetAssetDir(a.AssetDir())
	a.xray.Start(a.config.ConfigPath)
	return nil
}
