package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xray-panel/internal/auth"
	"xray-panel/internal/config"
	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

func TestRandomInitialPassword(t *testing.T) {
	dir := t.TempDir()
	a, err := New(Config{Store: &memStore{state: &model.PanelState{}}, XrayProc: &service.FakeXray{},
		ConfigPath: filepath.Join(dir, "config.json"), DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, config.InitialPasswordFile))
	if err != nil {
		t.Fatal("initial-password.txt not written:", err)
	}
	pw := strings.TrimSpace(string(data))
	if !auth.VerifyPassword(a.State().Password, pw) || !a.PasswordInitial() {
		t.Fatal("random password should be stored and marked initial")
	}
	a.Store().Lock()
	if err := a.SetPassword("changed-pw"); err != nil {
		t.Fatal(err)
	}
	a.Store().Unlock()
	if a.PasswordInitial() {
		t.Fatal("changed password should clear the initial flag")
	}
	if _, err := os.Stat(filepath.Join(dir, config.InitialPasswordFile)); !os.IsNotExist(err) {
		t.Fatal("initial-password.txt should be removed after change")
	}
}

func TestEnvPasswordNotInitial(t *testing.T) {
	a, _, _ := newTestApp(t, &model.PanelState{})
	if a.PasswordInitial() {
		t.Fatal("PANEL_PASSWORD should not be marked initial")
	}
	if _, err := os.Stat(a.dataPath(config.InitialPasswordFile)); !os.IsNotExist(err) {
		t.Fatal("no initial-password.txt for env password")
	}
}

func TestPasswordReset(t *testing.T) {
	a, _, _ := newTestApp(t, &model.PanelState{})
	tok := a.Sessions().Create()
	if err := WritePasswordReset(a.config.DataDir, "short", false); err == nil {
		t.Fatal("short password should be rejected")
	}
	if err := WritePasswordReset(a.config.DataDir, "new-secret", false); err != nil {
		t.Fatal(err)
	}
	a.CheckPasswordReset()
	if !auth.VerifyPassword(a.State().Password, "new-secret") || auth.VerifyPassword(a.State().Password, "pw") {
		t.Fatal("reset password not applied")
	}
	if a.Sessions().Valid(tok) {
		t.Fatal("sessions should be cleared after reset")
	}
	if _, err := os.Stat(a.dataPath(config.PasswordResetFile)); !os.IsNotExist(err) {
		t.Fatal("reset file should be consumed")
	}

	// 面板未运行时写入的重置文件在启动时生效
	WritePasswordReset(a.config.DataDir, "boot-secret", true)
	b, err := New(Config{Store: a.store, XrayProc: &service.FakeXray{}, ConfigPath: a.config.ConfigPath, DataDir: a.config.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyPassword(b.State().Password, "boot-secret") || !b.PasswordInitial() {
		t.Fatal("reset on startup not applied")
	}
}

func TestResolveXrayBin(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	cfgBin := filepath.Join(dir, "configured-xray")
	installDir := filepath.Join(dir, "bin")
	if p, ok := ResolveXrayBin(cfgBin, installDir); ok || p != filepath.Join(installDir, "xray") {
		t.Fatalf("missing everywhere: got %s %v", p, ok)
	}
	os.MkdirAll(installDir, 0755)
	os.WriteFile(filepath.Join(installDir, "xray"), []byte("x"), 0755)
	if p, ok := ResolveXrayBin(cfgBin, installDir); !ok || p != filepath.Join(installDir, "xray") {
		t.Fatalf("installed: got %s %v", p, ok)
	}
	os.WriteFile(cfgBin, []byte("x"), 0755)
	if p, ok := ResolveXrayBin(cfgBin, installDir); !ok || p != cfgBin {
		t.Fatalf("configured wins: got %s %v", p, ok)
	}
}

func TestAssetDirFallsBackToXrayDir(t *testing.T) {
	a, _, _ := newTestApp(t, &model.PanelState{})
	binDir := t.TempDir()
	os.WriteFile(filepath.Join(binDir, "geoip.dat"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(binDir, "geosite.dat"), []byte("x"), 0644)
	a.config.XrayAssetDir = filepath.Join(t.TempDir(), "missing")
	a.config.XrayBin = filepath.Join(binDir, "xray")
	if got := a.AssetDir(); got != binDir {
		t.Fatalf("AssetDir = %s, want %s", got, binDir)
	}
}

func TestInstallXrayFailureStatus(t *testing.T) {
	a, _, fx := newTestApp(t, &model.PanelState{})
	a.config.XrayInstallDir = t.TempDir()
	a.download = func(string) ([]byte, error) { return nil, os.ErrDeadlineExceeded }
	if err := a.InstallXray(); err == nil {
		t.Fatal("want error")
	}
	if st := a.XrayInstall(); st.State != "failed" || st.Error == "" {
		t.Fatalf("status = %+v", st)
	}
	if fx.Running() {
		t.Fatal("xray should not start after failed install")
	}
}
