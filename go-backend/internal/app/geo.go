package app

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"xray-panel/internal/service"
)

var GeoSources = map[string]struct {
	Name    string
	GeoIP   string
	GeoSite string
}{
	"loyalsoldier": {"Loyalsoldier 增强版（推荐）",
		"https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat",
		"https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat"},
	"v2fly": {"v2fly 官方",
		"https://github.com/v2fly/geoip/releases/latest/download/geoip.dat",
		"https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat"},
}

const maxGeoSize = 100 << 20

func (a *App) userGeoDir() string { return filepath.Join(a.config.DataDir, "geo") }

func hasGeoFiles(dir string) bool {
	for _, f := range []string{"geoip.dat", "geosite.dat"} {
		if st, err := os.Stat(filepath.Join(dir, f)); err != nil || st.Size() == 0 {
			return false
		}
	}
	return dir != ""
}

// AssetDir 返回 Xray 使用的 geo 数据目录：面板下载的数据优先，否则用镜像自带的。
func (a *App) AssetDir() string {
	if a.config.DataDir != "" && hasGeoFiles(a.userGeoDir()) {
		return a.userGeoDir()
	}
	return a.config.XrayAssetDir
}

// GeoDB 返回规则测试用的 geo 数据读取器。
func (a *App) GeoDB() *service.GeoDB {
	a.geoMu.Lock()
	defer a.geoMu.Unlock()
	dir := a.AssetDir()
	if a.geoDB == nil || a.geoDB.Dir != dir {
		a.geoDB = service.NewGeoDB(dir)
	}
	return a.geoDB
}

// GeoFileInfo 描述当前使用的 geo 数据文件。
type GeoFileInfo struct {
	Dir     string `json:"dir"`
	Custom  bool   `json:"custom"` // 是否为面板下载的数据
	GeoIP   int64  `json:"geoip_size"`
	GeoSite int64  `json:"geosite_size"`
	ModTime int64  `json:"mod_time"`
}

func (a *App) GeoInfo() GeoFileInfo {
	dir := a.AssetDir()
	info := GeoFileInfo{Dir: dir, Custom: dir == a.userGeoDir() && a.config.DataDir != ""}
	if st, err := os.Stat(filepath.Join(dir, "geoip.dat")); err == nil {
		info.GeoIP, info.ModTime = st.Size(), st.ModTime().Unix()
	}
	if st, err := os.Stat(filepath.Join(dir, "geosite.dat")); err == nil {
		info.GeoSite = st.Size()
	}
	return info
}

// localProxyURL 找到一个可用的本机 HTTP 入站，用于直连失败时经代理访问外网（如 GitHub、Telegram）。
func (a *App) localProxyURL() *url.URL {
	if !a.xray.Running() {
		return nil
	}
	a.store.Lock()
	defer a.store.Unlock()
	for i := range a.state.Inbounds {
		ib := &a.state.Inbounds[i]
		if ib.Protocol != "http" {
			continue
		}
		u := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(ib.Port))}
		for _, usr := range service.InboundUsers(a.state, ib) {
			if !usr.Disabled && usr.Name != "" {
				u.User = url.UserPassword(usr.Name, usr.Secret)
				break
			}
		}
		return u
	}
	return nil
}

// withFallback 先直连执行 fn，失败后经本机代理重试一次。
func (a *App) withFallback(fn func(c *http.Client) error) error {
	err := fn(&http.Client{Timeout: 30 * time.Second})
	if err == nil {
		return nil
	}
	if pu := a.localProxyURL(); pu != nil {
		c := &http.Client{Timeout: 120 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
		if err2 := fn(c); err2 == nil {
			return nil
		} else {
			return fmt.Errorf("直连失败（%v），经本机代理也失败（%v）", err, err2)
		}
	}
	return err
}

func (a *App) httpDownload(u string) ([]byte, error) {
	var data []byte
	err := a.withFallback(func(c *http.Client) error {
		if c.Timeout < 5*time.Minute {
			c.Timeout = 5 * time.Minute
		}
		resp, err := c.Get(u)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxGeoSize))
		return err
	})
	return data, err
}

// UpdateGeo 下载并替换 geo 数据，成功后若 Xray 在运行则重启使其生效。
func (a *App) UpdateGeo() (GeoFileInfo, error) {
	a.store.Lock()
	src := a.state.Geo.Source
	a.store.Unlock()
	s, ok := GeoSources[src]
	if !ok {
		s = GeoSources["loyalsoldier"]
	}
	fail := func(err error) (GeoFileInfo, error) {
		a.store.Lock()
		a.state.Geo.LastError = err.Error()
		a.Persist()
		a.store.Unlock()
		a.notifyOnce("geo_update", "geo:fail:"+err.Error(), time.Hour, "规则数据库更新失败", err.Error())
		return a.GeoInfo(), err
	}
	ip, err := a.download(s.GeoIP)
	if err != nil {
		return fail(fmt.Errorf("下载 geoip.dat 失败: %v", err))
	}
	site, err := a.download(s.GeoSite)
	if err != nil {
		return fail(fmt.Errorf("下载 geosite.dat 失败: %v", err))
	}
	if err := service.ValidateGeoFiles(ip, site); err != nil {
		return fail(err)
	}
	dir := a.userGeoDir()
	os.MkdirAll(dir, 0755)
	if err := writeAtomic(filepath.Join(dir, "geoip.dat"), ip); err != nil {
		return fail(err)
	}
	if err := writeAtomic(filepath.Join(dir, "geosite.dat"), site); err != nil {
		return fail(err)
	}
	a.xray.SetAssetDir(a.AssetDir())
	a.store.Lock()
	a.state.Geo.UpdatedAt, a.state.Geo.LastError = time.Now().Unix(), ""
	a.Persist()
	a.store.Unlock()
	if a.xray.Running() {
		a.applyMu.Lock()
		a.xray.Restart(a.config.ConfigPath)
		a.applyMu.Unlock()
	}
	slog.Info("规则数据库已更新", "source", src)
	a.notifyOnce("geo_update", "geo:ok", time.Hour, "规则数据库已更新",
		fmt.Sprintf("geoip %.1f MB，geosite %.1f MB", float64(len(ip))/1e6, float64(len(site))/1e6))
	return a.GeoInfo(), nil
}

func (a *App) autoUpdateGeo() {
	a.store.Lock()
	g := a.state.Geo
	a.store.Unlock()
	if g.AutoUpdateDays <= 0 || time.Now().Unix()-g.UpdatedAt < int64(g.AutoUpdateDays)*86400 {
		return
	}
	if _, err := a.UpdateGeo(); err != nil {
		slog.Warn("规则数据库自动更新失败", "error", err)
	}
}
