package app

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"xray-panel/internal/auth"
	"xray-panel/internal/config"
	"xray-panel/internal/model"
)

// 登录密码的初始化与重置。
//
//   - 首次启动：用 PANEL_PASSWORD，没有则随机生成，随机密码同时写入数据目录的 initial-password.txt；
//     PANEL_PASSWORD 只在首次初始化时生效，之后修改它不会改变密码（启动时会提示）。
//   - 忘记密码：xray-panel passwd [新密码] 把新密码的哈希写入 password-reset.json，
//     运行中的面板在下次登录前（或 30 秒内）读取并生效，同时让所有已登录会话失效；面板未运行时在下次启动时生效。
//     这样命令行不需要和运行中的面板争写 panel.json。

// RandomPassword 生成 12 位随机密码（只含易辨认的字符）。
func RandomPassword() string {
	const chars = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 12)
	rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// WritePasswordReset 由 xray-panel passwd 调用：写入待生效的新密码（只存哈希）。
func WritePasswordReset(dataDir, pw string, initial bool) error {
	if len(pw) < 6 {
		return fmt.Errorf("密码至少 6 位")
	}
	rec, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	rec.Initial = initial
	data, _ := json.Marshal(rec)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dataDir, config.PasswordResetFile), data)
}

func (a *App) dataPath(name string) string {
	if a.config.DataDir == "" {
		return ""
	}
	return filepath.Join(a.config.DataDir, name)
}

func (a *App) ensurePassword() {
	if a.applyPasswordReset() || a.state.Password != nil {
		a.warnStaleEnvPassword()
		return
	}
	pw, initial := "", false
	if a.config.Password != nil && *a.config.Password != "" {
		pw = *a.config.Password
		slog.Info("面板登录密码已设置为环境变量 PANEL_PASSWORD 的值")
	} else {
		pw, initial = RandomPassword(), true
	}
	// 先写 initial-password.txt 再保存：安装脚本看到 panel.json 中有密码时，密码文件一定已就绪
	where := ""
	if initial {
		if p := a.dataPath(config.InitialPasswordFile); p != "" && os.WriteFile(p, []byte(pw+"\n"), 0600) == nil {
			where = "，已同时保存到 " + p
		}
	}
	rec, _ := auth.HashPassword(pw)
	rec.Initial = initial
	a.state.Password = rec
	a.store.Save(a.state)
	if initial {
		slog.Info(fmt.Sprintf("面板登录密码（随机生成）: %s%s；登录后请在「设置 → 账户与服务器」中修改", pw, where))
	}
}

// warnStaleEnvPassword：已有密码时 PANEL_PASSWORD 不再生效，与当前密码不一致时提示，避免以为改环境变量就能改密码。
func (a *App) warnStaleEnvPassword() {
	if a.config.Password == nil || *a.config.Password == "" || auth.VerifyPassword(a.state.Password, *a.config.Password) {
		return
	}
	slog.Warn("PANEL_PASSWORD 只在首次启动时生效，当前登录密码与它不同；如需重置密码请执行 xray-panel passwd")
}

// applyPasswordReset 读取 xray-panel passwd 写入的新密码。返回是否有新密码生效。
func (a *App) applyPasswordReset() bool {
	p := a.dataPath(config.PasswordResetFile)
	if p == "" {
		return false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	var rec model.PasswordRec
	if json.Unmarshal(data, &rec) != nil || rec.Salt == "" || rec.Hash == "" {
		slog.Warn("忽略无效的密码重置文件", "path", p)
		os.Remove(p)
		return false
	}
	a.store.Lock()
	a.state.Password = &rec
	err = a.store.Save(a.state)
	a.store.Unlock()
	if err != nil {
		slog.Error("保存新密码失败", "error", err)
		return false
	}
	os.Remove(p)
	a.sessions.Clear()
	if !rec.Initial {
		a.removeInitialPassword()
	}
	slog.Info("登录密码已通过 xray-panel passwd 重置，已登录的会话需要重新登录")
	return true
}

func (a *App) removeInitialPassword() {
	if p := a.dataPath(config.InitialPasswordFile); p != "" {
		os.Remove(p)
	}
}

// CheckPasswordReset 在登录前调用，让 passwd 命令设置的新密码立即可用。
func (a *App) CheckPasswordReset() { a.applyPasswordReset() }

// SetPassword 由「修改密码」调用。调用方需持有 Store 锁。
func (a *App) SetPassword(pw string) error {
	rec, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	a.state.Password = rec
	if err := a.Persist(); err != nil {
		return err
	}
	a.removeInitialPassword()
	return nil
}

// PasswordInitial 表示当前仍是自动生成的初始密码。
func (a *App) PasswordInitial() bool {
	a.store.Lock()
	defer a.store.Unlock()
	return a.state.Password != nil && a.state.Password.Initial
}
