package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// 自动安装 Xray-core：从官方 GitHub Release 下载对应系统的 zip，校验 .dgst 中的 SHA-256，
// 解压出 xray、geoip.dat、geosite.dat 到目标目录。面板启动时找不到 Xray 和 xray-panel install-xray 共用。

const xrayReleaseBase = "https://github.com/XTLS/Xray-core/releases/latest/download/"

// XrayReleaseAsset 返回官方 Release 中对应系统的文件名。
func XrayReleaseAsset(goos, goarch string) (string, error) {
	osName := map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows", "freebsd": "freebsd"}[goos]
	arch := map[string]string{"amd64": "64", "386": "32", "arm64": "arm64-v8a", "arm": "arm32-v7a",
		"riscv64": "riscv64", "loong64": "loong64", "mips64le": "mips64le", "s390x": "s390x"}[goarch]
	if osName == "" || arch == "" {
		return "", fmt.Errorf("不支持自动安装 Xray 的系统 %s/%s，请手动安装并设置 XRAY_BIN", goos, goarch)
	}
	return "Xray-" + osName + "-" + arch + ".zip", nil
}

// XrayBinName 是 zip 中的可执行文件名。
func XrayBinName(goos string) string {
	if goos == "windows" {
		return "xray.exe"
	}
	return "xray"
}

// DownloadXray 下载并安装最新版 Xray 到 dir，返回可执行文件路径。get 负责实际的 HTTP 下载；
// ghProxy 为可选的 GitHub 加速前缀（如 https://ghfast.top/）。
func DownloadXray(get func(url string) ([]byte, error), ghProxy, dir string) (string, error) {
	asset, err := XrayReleaseAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	u := xrayReleaseBase + asset
	if ghProxy != "" {
		u = strings.TrimRight(ghProxy, "/") + "/" + u
	}
	zipData, err := get(u)
	if err != nil {
		return "", fmt.Errorf("下载 %s 失败: %v", asset, err)
	}
	dgst, err := get(u + ".dgst")
	if err != nil {
		return "", fmt.Errorf("下载校验文件失败: %v", err)
	}
	return InstallXrayZip(zipData, dgst, dir, XrayBinName(runtime.GOOS))
}

// InstallXrayZip 校验并解压。先写临时文件再 rename，正在运行的旧版本不受影响（重启后生效）。
func InstallXrayZip(zipData, dgst []byte, dir, binName string) (string, error) {
	want := parseDgstSHA256(dgst)
	if want == "" {
		return "", fmt.Errorf("校验文件中没有 SHA2-256")
	}
	sum := sha256.Sum256(zipData)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, want) {
		return "", fmt.Errorf("文件校验失败（SHA-256 不一致），可能下载不完整或被篡改")
	}
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return "", fmt.Errorf("解压失败: %v", err)
	}
	wanted := map[string]os.FileMode{binName: 0755, "geoip.dat": 0644, "geosite.dat": 0644}
	files := map[string][]byte{}
	for _, f := range zr.File {
		name := filepath.Base(f.Name)
		if _, ok := wanted[name]; !ok || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 200<<20))
		rc.Close()
		if err != nil {
			return "", err
		}
		files[name] = data
	}
	if len(files[binName]) == 0 {
		return "", fmt.Errorf("压缩包中没有 %s", binName)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	for name, data := range files {
		dst := filepath.Join(dir, name)
		tmp := dst + ".tmp"
		if err := os.WriteFile(tmp, data, wanted[name]); err != nil {
			return "", err
		}
		os.Chmod(tmp, wanted[name])
		if err := os.Rename(tmp, dst); err != nil {
			os.Remove(tmp)
			return "", err
		}
	}
	return filepath.Join(dir, binName), nil
}

// parseDgstSHA256 解析 Xray Release 的 .dgst 文件（每行 "SHA2-256= hex"）。
func parseDgstSHA256(dgst []byte) string {
	for _, line := range strings.Split(string(dgst), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "SHA2-256" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
