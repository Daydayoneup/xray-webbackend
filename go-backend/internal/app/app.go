package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	"xray-panel/internal/auth"
	"xray-panel/internal/model"
	"xray-panel/internal/service"
	"xray-panel/internal/store"
)

type Config struct {
	Store                     store.Store
	XrayProc                  service.XrayProc
	ConfigPath                string
	PanelPort                 int
	Password                  *string
	SubscriptionAllowInternal bool
}

type App struct {
	store    store.Store
	xray     service.XrayProc
	sessions *auth.SessionStore
	state    *model.PanelState
	config   Config
}

func New(cfg Config) (*App, error) {
	state, err := cfg.Store.Load()
	if err != nil {
		return nil, fmt.Errorf("加载状态失败: %w", err)
	}

	a := &App{
		store:    cfg.Store,
		xray:     cfg.XrayProc,
		sessions: auth.NewSessionStore(7 * 86400),
		state:    state,
		config:   cfg,
	}

	a.ensurePassword()
	return a, nil
}

func (a *App) ensurePassword() {
	if a.state.Password != nil {
		return
	}
	pw := ""
	src := "随机生成"
	if a.config.Password != nil && *a.config.Password != "" {
		pw = *a.config.Password
		src = "环境变量 PANEL_PASSWORD"
	} else {
		b := make([]byte, 9)
		rand.Read(b)
		pw = hex.EncodeToString(b)
	}
	rec, _ := auth.HashPassword(pw)
	a.state.Password = &model.PasswordRec{Salt: rec.Salt, Hash: rec.Hash}
	a.store.Save(a.state)
	slog.Info(fmt.Sprintf("面板登录密码(%s): %s", src, pw))
}

func (a *App) State() *model.PanelState    { return a.state }
func (a *App) Sessions() *auth.SessionStore { return a.sessions }
func (a *App) Xray() service.XrayProc       { return a.xray }
func (a *App) Store() store.Store           { return a.store }
func (a *App) Persist() error               { return a.store.Save(a.state) }
func (a *App) Config() Config               { return a.config }

func (a *App) BalancerTags() map[string]bool {
	tags := map[string]bool{}
	for _, b := range a.state.Balancers {
		tags[b.Tag] = true
	}
	return tags
}

func (a *App) OutboundTags() map[string]bool {
	tags := map[string]bool{"direct": true, "block": true}
	for _, n := range a.state.Nodes {
		tags[n.Tag] = true
	}
	for _, b := range a.state.Balancers {
		tags[b.Tag] = true
	}
	for _, p := range a.state.Proxies {
		tags[p.Tag] = true
	}
	return tags
}

func (a *App) PruneDangling() {
	valid := a.OutboundTags()
	if !valid[a.state.DefaultOutbound] {
		if len(a.state.Nodes) > 0 {
			a.state.DefaultOutbound = a.state.Nodes[0].Tag
		} else {
			a.state.DefaultOutbound = "direct"
		}
	}
	var kept []model.Rule
	for _, r := range a.state.Rules {
		if valid[r.Outbound] {
			kept = append(kept, r)
		}
	}
	a.state.Rules = kept
}

func (a *App) OutboundLabel(tag string) string {
	switch tag {
	case "direct":
		return "直连"
	case "block":
		return "阻断"
	}
	for _, n := range a.state.Nodes {
		if n.Tag == tag {
			return n.Name
		}
	}
	for _, b := range a.state.Balancers {
		if b.Tag == tag {
			return "⚖ " + b.Name + "(自动)"
		}
	}
	for _, p := range a.state.Proxies {
		if p.Tag == tag {
			return "🛰 " + p.Name + "(落地)"
		}
	}
	return tag
}

func (a *App) Dirty(applied map[string]any) bool {
	if len(a.state.Nodes) == 0 {
		return false
	}
	if applied == nil {
		return true
	}
	draft := service.BuildConfig(a.state)
	key := func(c map[string]any) string {
		data, _ := json.Marshal(map[string]any{
			"i": c["inbounds"], "o": c["outbounds"],
			"r": c["routing"], "ob": c["observatory"],
		})
		return string(data)
	}
	return key(draft) != key(applied)
}
