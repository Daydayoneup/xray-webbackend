package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type XrayProc interface {
	Running() bool
	Start(configPath string)
	Stop()
	Restart(configPath string) bool
	TestConfig(cfgJSON []byte) (bool, string)
}

type xrayProc struct {
	bin     string
	workdir string
	mu      sync.Mutex
	cmd     *exec.Cmd
}

func NewXrayProc(bin, workdir string) XrayProc {
	return &xrayProc{bin: bin, workdir: workdir}
}

func (x *xrayProc) Running() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.cmd == nil || x.cmd.Process == nil {
		return false
	}
	return x.cmd.ProcessState == nil
}

func (x *xrayProc) Start(configPath string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if _, err := os.Stat(configPath); err != nil {
		return
	}
	x.cmd = exec.Command(x.bin, "-config", configPath)
	x.cmd.Stdout = os.Stdout
	x.cmd.Stderr = os.Stderr
	x.cmd.Start()
}

func (x *xrayProc) Stop() {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.cmd == nil || x.cmd.Process == nil {
		return
	}
	x.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- x.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		x.cmd.Process.Kill()
		<-done
	}
	x.cmd = nil
}

func (x *xrayProc) Restart(configPath string) bool {
	x.Stop()
	x.Start(configPath)
	time.Sleep(500 * time.Millisecond)
	return x.Running()
}

func (x *xrayProc) TestConfig(cfgJSON []byte) (bool, string) {
	os.MkdirAll(x.workdir, 0755)
	tmp := filepath.Join(x.workdir, "config.test.json")
	if err := os.WriteFile(tmp, cfgJSON, 0644); err != nil {
		return false, fmt.Sprintf("写入测试配置失败: %v", err)
	}
	defer os.Remove(tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, x.bin, "-test", "-config", tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, string(out)
	}
	return cmd.ProcessState.ExitCode() == 0, string(out)
}

// FakeXray is a test double implementing XrayProc
type FakeXray struct {
	Alive   bool
	LastCfg []byte
}

func (f *FakeXray) Running() bool                     { return f.Alive }
func (f *FakeXray) Start(_ string)                     { f.Alive = true }
func (f *FakeXray) Stop()                              { f.Alive = false }
func (f *FakeXray) Restart(_ string) bool              { f.Alive = true; return true }
func (f *FakeXray) TestConfig(cfgJSON []byte) (bool, string) { f.LastCfg = cfgJSON; return true, "" }
