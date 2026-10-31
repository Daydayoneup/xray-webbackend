package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const probeTarget = "http://www.gstatic.com/generate_204"

func freePorts(n int) ([]int, error) {
	ls := make([]net.Listener, 0, n)
	defer func() {
		for _, l := range ls {
			l.Close()
		}
	}()
	ports := make([]int, 0, n)
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		ls = append(ls, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports, nil
}

var badProbeTag = regexp.MustCompile(`tag probe-out-(\d+)`)

// probeOutbounds 启动一个临时 Xray：每个出站配一个本地 HTTP 入站，
// 再经由该入站请求 generate_204，得到“经过代理”的真实延迟。
// 某个出站配置不被 Xray 接受（如订阅里的异常节点）时，Xray 会整体启动失败：
// 从错误信息中找出该出站，记为不可用后去掉它重试，其余节点照常测速。
func probeOutbounds(bin, workdir string, obs []map[string]any, timeout time.Duration) ([]*int, error) {
	obs = append([]map[string]any(nil), obs...)
	for attempt := 0; ; attempt++ {
		res, err := probeOnce(bin, workdir, obs, timeout)
		if err == nil || attempt >= len(obs) {
			return res, err
		}
		m := badProbeTag.FindStringSubmatch(err.Error())
		if m == nil {
			return res, err
		}
		i, _ := strconv.Atoi(m[1])
		if i < 0 || i >= len(obs) || obs[i] == nil {
			return res, err
		}
		obs[i] = nil // 结果为 nil：不可用
	}
}

func probeOnce(bin, workdir string, obs []map[string]any, timeout time.Duration) ([]*int, error) {
	results := make([]*int, len(obs))
	idx := []int{}
	for i, ob := range obs {
		if ob != nil {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return results, nil
	}
	ports, err := freePorts(len(idx))
	if err != nil {
		return nil, fmt.Errorf("分配测速端口失败: %v", err)
	}
	inbounds := []any{}
	outbounds := []any{map[string]any{"tag": "probe-block", "protocol": "blackhole"}}
	rules := []any{}
	portOf := map[int]int{}
	for k, i := range idx {
		ob := deepCopyMap(obs[i])
		if ss, ok := ob["streamSettings"].(map[string]any); ok {
			if so, ok := ss["sockopt"].(map[string]any); ok {
				delete(so, "dialerProxy")
			}
		}
		in, out := fmt.Sprintf("probe-in-%d", i), fmt.Sprintf("probe-out-%d", i)
		ob["tag"] = out
		outbounds = append(outbounds, ob)
		inbounds = append(inbounds, map[string]any{
			"tag": in, "listen": "127.0.0.1", "port": ports[k], "protocol": "http", "settings": map[string]any{},
		})
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{in}, "outboundTag": out})
		portOf[i] = ports[k]
	}
	cfg := map[string]any{
		"log":       map[string]any{"loglevel": "none"},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing":   map[string]any{"rules": rules},
	}
	data, _ := json.Marshal(cfg)
	os.MkdirAll(workdir, 0755)
	path := filepath.Join(workdir, "probe.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	defer os.Remove(path)

	var stderr bytes.Buffer
	cmd := exec.Command(bin, "-config", path)
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动测速进程失败: %v", err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	defer func() {
		cmd.Process.Kill()
		<-exited
	}()

	firstAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(ports[0]))
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		select {
		case <-exited:
			return nil, fmt.Errorf("测速进程启动失败: %s", strings.TrimSpace(stderr.String()))
		default:
		}
		if c, err := net.DialTimeout("tcp", firstAddr, 200*time.Millisecond); err == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		return nil, fmt.Errorf("测速进程启动超时")
	}

	sem := make(chan struct{}, 32)
	var wg sync.WaitGroup
	for _, i := range idx {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pu, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", portOf[i]))
			client := &http.Client{
				Timeout:   timeout,
				Transport: &http.Transport{Proxy: http.ProxyURL(pu), DisableKeepAlives: true},
			}
			start := time.Now()
			resp, err := client.Get(probeTarget)
			if err != nil {
				return
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
				ms := int(time.Since(start).Milliseconds())
				results[i] = &ms
			}
		}(i)
	}
	wg.Wait()
	return results, nil
}
