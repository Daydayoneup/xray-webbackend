package service

import "testing"

func TestParseAccessLine(t *testing.T) {
	cases := []struct {
		line string
		want AccessEvent
	}{
		{"2025/01/02 15:04:05.123456 from 1.2.3.4:51234 accepted tcp:www.google.com:443 [in-2 >> node-ab12cd34] email: alice@in-2",
			AccessEvent{IP: "1.2.3.4", Port: "51234", Network: "tcp", Dest: "www.google.com:443", Inbound: "in-2", Outbound: "node-ab12cd34", Email: "alice@in-2"}},
		{"2025/01/02 15:04:05 from tcp:[2001:db8::1]:6000 accepted udp:8.8.8.8:53 [in-0 -> direct]",
			AccessEvent{IP: "2001:db8::1", Port: "6000", Network: "udp", Dest: "8.8.8.8:53", Inbound: "in-0", Outbound: "direct"}},
		{"2023/05/20 10:00:00 127.0.0.1:5000 accepted tcp:example.com:80 [in-1 >> auto-0]",
			AccessEvent{IP: "127.0.0.1", Port: "5000", Network: "tcp", Dest: "example.com:80", Inbound: "in-1", Outbound: "auto-0"}},
	}
	for _, c := range cases {
		got, ok := ParseAccessLine(c.line)
		if !ok || got != c.want {
			t.Errorf("ParseAccessLine(%q)\n got %+v %v\nwant %+v", c.line, got, ok, c.want)
		}
	}
	for _, bad := range []string{
		"2025/01/02 15:04:05 [Warning] core: Xray 25.1.30 started",
		"from 1.2.3.4:5 rejected  proxy/vless/encoding: invalid request user id",
		"[panel] Xray 进程退出: 退出码 1",
	} {
		if _, ok := ParseAccessLine(bad); ok {
			t.Errorf("should not parse %q", bad)
		}
	}
}

func TestRingLogRoutesAccessLines(t *testing.T) {
	r := newRingLog(10)
	var got []AccessEvent
	r.setAccess(func(ev AccessEvent) { got = append(got, ev) })
	r.Write([]byte("core started\nfrom 1.2.3.4:5 accepted tcp:a.com:443 [in-0 >> direct]\nwarn"))
	r.Write([]byte("ing\n"))
	if len(got) != 1 || got[0].IP != "1.2.3.4" {
		t.Fatalf("access events = %+v", got)
	}
	lines := r.Last(0)
	if len(lines) != 2 {
		t.Fatalf("access lines must not enter the log buffer: %v", lines)
	}
}
