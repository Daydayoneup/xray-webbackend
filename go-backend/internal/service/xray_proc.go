package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ProcStatus 是 Xray 进程的实时状态。
type ProcStatus struct {
	State     string `json:"state"` // running | restarting | crashed | stopped
	Running   bool   `json:"running"`
	PID       int    `json:"pid,omitempty"`
	StartedAt int64  `json:"started_at,omitempty"`
	Restarts  int    `json:"restarts"`  // 本次启动以来的自动重启次数
	LastExit  string `json:"last_exit"` // 最近一次退出原因
	Version   string `json:"version"`
}

type XrayProc interface {
	Running() bool
	Status() ProcStatus
	Start(configPath string)
	Stop()
	Restart(configPath string) bool
	TestConfig(cfgJSON []byte) (bool, string)
	Logs(n int) []string
	// Probe 通过临时 Xray 实例对每个出站做真实连接测速，nil 表示不可用。
	Probe(outbounds []map[string]any, timeout time.Duration) ([]*int, error)
	// SetAssetDir 设置 geoip.dat/geosite.dat 所在目录（XRAY_LOCATION_ASSET），下次启动生效
	SetAssetDir(dir string)
	// SetAccessHandler 设置访问日志回调：Xray 输出的访问日志交给 h 处理，不再进入日志缓冲。
	SetAccessHandler(h func(AccessEvent))
}

const (
	maxFailStreak  = 5
	stableRunTime  = 10 * time.Second
	restartCheckIn = time.Second
)

type xrayProc struct {
	bin     string
	workdir string
	logs    *ringLog

	mu         sync.Mutex
	cmd        *exec.Cmd
	done       chan struct{}
	configPath string
	desired    bool // 用户期望 Xray 运行
	gaveUp     bool // 连续崩溃过多，停止自动重启
	failStreak int
	restarts   int
	startedAt  time.Time
	lastExit   string
	version    string
	assetDir   string
}

func (x *xrayProc) SetAccessHandler(h func(AccessEvent)) { x.logs.setAccess(h) }

func (x *xrayProc) SetAssetDir(dir string) {
	x.mu.Lock()
	x.assetDir = dir
	x.mu.Unlock()
}

func (x *xrayProc) env() []string {
	env := os.Environ()
	if x.assetDir != "" {
		env = append(env, "XRAY_LOCATION_ASSET="+x.assetDir)
	}
	return env
}

func NewXrayProc(bin, workdir string) XrayProc {
	x := &xrayProc{bin: bin, workdir: workdir, logs: newRingLog(500)}
	go x.loadVersion()
	return x
}

func (x *xrayProc) loadVersion() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, x.bin, "version").Output()
	if err != nil {
		return
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	// "Xray 25.1.30 (Xray, Penetrates Everything.) ..." → "Xray 25.1.30"
	if f := strings.Fields(line); len(f) >= 2 {
		line = f[0] + " " + f[1]
	}
	x.mu.Lock()
	x.version = line
	x.mu.Unlock()
}

func (x *xrayProc) Running() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.cmd != nil
}

func (x *xrayProc) Status() ProcStatus {
	x.mu.Lock()
	defer x.mu.Unlock()
	st := ProcStatus{Restarts: x.restarts, LastExit: x.lastExit, Version: x.version}
	switch {
	case x.cmd != nil:
		st.State, st.Running = "running", true
		st.PID = x.cmd.Process.Pid
		st.StartedAt = x.startedAt.Unix()
	case x.desired && !x.gaveUp:
		st.State = "restarting"
	case x.desired && x.gaveUp:
		st.State = "crashed"
	default:
		st.State = "stopped"
	}
	return st
}

func (x *xrayProc) Logs(n int) []string { return x.logs.Last(n) }

func (x *xrayProc) startLocked() {
	cmd := exec.Command(x.bin, "-config", x.configPath)
	cmd.Env = x.env()
	w := io.MultiWriter(os.Stdout, x.logs)
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Start(); err != nil {
		x.lastExit = "启动失败: " + err.Error()
		x.gaveUp = true
		fmt.Fprintf(x.logs, "[panel] %s\n", x.lastExit)
		slog.Error("Xray 启动失败", "error", err)
		return
	}
	done := make(chan struct{})
	x.cmd, x.done, x.startedAt = cmd, done, time.Now()
	go x.wait(cmd, done)
	if x.version == "" { // 启动时还没安装 Xray（自动安装后首次启动）
		go x.loadVersion()
	}
}

func (x *xrayProc) wait(cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()
	defer close(done)
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.cmd != cmd { // 被 Stop 主动停止
		return
	}
	x.cmd = nil
	x.lastExit = describeExit(err)
	fmt.Fprintf(x.logs, "[panel] Xray 进程退出: %s\n", x.lastExit)
	if !x.desired {
		return
	}
	if time.Since(x.startedAt) < stableRunTime {
		x.failStreak++
	} else {
		x.failStreak = 1
	}
	if x.failStreak > maxFailStreak {
		x.gaveUp = true
		slog.Error("Xray 连续崩溃，已停止自动重启", "exit", x.lastExit)
		fmt.Fprintf(x.logs, "[panel] 连续 %d 次快速崩溃，已停止自动重启\n", maxFailStreak)
		return
	}
	delay := time.Duration(1<<uint(x.failStreak-1)) * time.Second
	slog.Warn("Xray 异常退出，将自动重启", "exit", x.lastExit, "delay", delay)
	time.AfterFunc(delay, func() {
		x.mu.Lock()
		defer x.mu.Unlock()
		if x.desired && !x.gaveUp && x.cmd == nil {
			x.restarts++
			x.startLocked()
		}
	})
}

func describeExit(err error) string {
	if err == nil {
		return "正常退出(exit 0)"
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return "被信号终止: " + ws.Signal().String()
		}
		return fmt.Sprintf("退出码 %d", ee.ExitCode())
	}
	return err.Error()
}

func (x *xrayProc) Start(configPath string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.configPath = configPath
	if _, err := os.Stat(configPath); err != nil {
		return
	}
	x.desired, x.gaveUp, x.failStreak, x.restarts = true, false, 0, 0
	if x.cmd != nil {
		return
	}
	x.startLocked()
}

func (x *xrayProc) Stop() {
	x.mu.Lock()
	x.desired = false
	cmd, done := x.cmd, x.done
	x.cmd = nil
	x.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		<-done
	}
}

// Restart 重启并观察约 1 秒，返回 Xray 是否仍在运行（启动即崩溃视为失败）。
func (x *xrayProc) Restart(configPath string) bool {
	x.Stop()
	x.Start(configPath)
	deadline := time.Now().Add(restartCheckIn)
	for time.Now().Before(deadline) {
		if !x.Running() {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return x.Running()
}

func (x *xrayProc) TestConfig(cfgJSON []byte) (bool, string) {
	os.MkdirAll(x.workdir, 0755)
	tmp := filepath.Join(x.workdir, "config.test.json")
	if err := os.WriteFile(tmp, cfgJSON, 0600); err != nil {
		return false, fmt.Sprintf("写入测试配置失败: %v", err)
	}
	defer os.Remove(tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, x.bin, "-test", "-config", tmp)
	x.mu.Lock()
	cmd.Env = x.env()
	x.mu.Unlock()
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) == 0 {
			return false, err.Error()
		}
		return false, string(out)
	}
	return cmd.ProcessState.ExitCode() == 0, string(out)
}

func (x *xrayProc) Probe(outbounds []map[string]any, timeout time.Duration) ([]*int, error) {
	return probeOutbounds(x.bin, x.workdir, outbounds, timeout)
}

// ---- 日志环形缓冲 ----

type ringLog struct {
	mu      sync.Mutex
	max     int
	lines   []string
	partial []byte
	access  func(AccessEvent)
}

func (r *ringLog) setAccess(h func(AccessEvent)) {
	r.mu.Lock()
	r.access = h
	r.mu.Unlock()
}

func newRingLog(max int) *ringLog { return &ringLog{max: max} }

func (r *ringLog) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data := append(r.partial, p...)
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(data[:i]), "\r")
		data = data[i+1:]
		if r.access != nil {
			if ev, ok := ParseAccessLine(line); ok {
				r.access(ev)
				continue
			}
		}
		r.lines = append(r.lines, time.Now().Format("01-02 15:04:05 ")+line)
	}
	r.partial = append([]byte(nil), data...)
	if over := len(r.lines) - r.max; over > 0 {
		r.lines = append([]string(nil), r.lines[over:]...)
	}
	return len(p), nil
}

func (r *ringLog) Last(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 || n > len(r.lines) {
		n = len(r.lines)
	}
	return append([]string{}, r.lines[len(r.lines)-n:]...)
}

// FakeXray is a test double implementing XrayProc
type FakeXray struct {
	Alive     bool
	LastCfg   []byte
	FailStart bool // Restart 后进程立即退出（模拟端口被占用等）
	Restarts  int
	Access    func(AccessEvent)
}

func (f *FakeXray) SetAccessHandler(h func(AccessEvent)) { f.Access = h }

func (f *FakeXray) Running() bool { return f.Alive }
func (f *FakeXray) Status() ProcStatus {
	st := ProcStatus{State: "stopped", Running: f.Alive}
	if f.Alive {
		st.State = "running"
	}
	return st
}
func (f *FakeXray) Start(_ string) { f.Alive = !f.FailStart }
func (f *FakeXray) Stop()          { f.Alive = false }
func (f *FakeXray) Restart(_ string) bool {
	f.Restarts++
	f.Alive = !f.FailStart
	return f.Alive
}
func (f *FakeXray) TestConfig(cfgJSON []byte) (bool, string) { f.LastCfg = cfgJSON; return true, "" }
func (f *FakeXray) Logs(int) []string                        { return []string{"fake log"} }
func (f *FakeXray) SetAssetDir(string)                       {}
func (f *FakeXray) Probe(obs []map[string]any, _ time.Duration) ([]*int, error) {
	out := make([]*int, len(obs))
	for i, ob := range obs {
		if ob != nil {
			v := 100 + i
			out[i] = &v
		}
	}
	return out, nil
}
