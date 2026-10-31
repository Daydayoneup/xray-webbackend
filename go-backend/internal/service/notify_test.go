package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xray-panel/internal/model"
)

type captured struct {
	path, query string
	body        map[string]any
}

func notifyServer(t *testing.T, reply string) (*httptest.Server, func() []captured) {
	var mu sync.Mutex
	var got []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(b, &m)
		mu.Lock()
		got = append(got, captured{r.URL.Path, r.URL.RawQuery, m})
		mu.Unlock()
		w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []captured { mu.Lock(); defer mu.Unlock(); return append([]captured(nil), got...) }
}

func TestNotifyBotFormatsAndSignatures(t *testing.T) {
	fixed := time.Unix(1700000000, 0)
	notifyNow = func() time.Time { return fixed }
	defer func() { notifyNow = time.Now }()

	srv, got := notifyServer(t, `{"code":0,"errcode":0}`)
	cfg := model.NotifySettings{
		FeishuURL: srv.URL + "/feishu", FeishuSecret: "fs",
		DingTalkURL: srv.URL + "/ding?access_token=abc", DingTalkSecret: "ds",
		WeComURL: srv.URL + "/wecom?key=k", WebhookURL: srv.URL + "/hook",
	}
	if err := Notify(cfg, nil, "标题", "内容"); err != nil {
		t.Fatal(err)
	}
	reqs := map[string]captured{}
	for _, c := range got() {
		reqs[c.path] = c
	}
	if len(reqs) != 4 {
		t.Fatalf("requests = %+v", got())
	}

	fs := reqs["/feishu"]
	if fs.body["msg_type"] != "text" || fs.body["content"].(map[string]any)["text"] != "标题\n内容" || fs.body["timestamp"] != "1700000000" {
		t.Errorf("feishu body = %v", fs.body)
	}
	h := hmac.New(sha256.New, []byte("1700000000\nfs"))
	if want := base64.StdEncoding.EncodeToString(h.Sum(nil)); fs.body["sign"] != want {
		t.Errorf("feishu sign = %v, want %s", fs.body["sign"], want)
	}

	dd := reqs["/ding"]
	if dd.body["msgtype"] != "text" || dd.body["text"].(map[string]any)["content"] != "标题\n内容" {
		t.Errorf("dingtalk body = %v", dd.body)
	}
	h = hmac.New(sha256.New, []byte("ds"))
	h.Write([]byte("1700000000000\nds"))
	if !strings.Contains(dd.query, "access_token=abc&timestamp=1700000000000&sign=") ||
		!strings.Contains(dd.query, strings.ReplaceAll(base64.StdEncoding.EncodeToString(h.Sum(nil)), "+", "%2B")[:10]) {
		t.Errorf("dingtalk query = %s", dd.query)
	}

	if wc := reqs["/wecom"]; wc.body["msgtype"] != "text" || wc.query != "key=k" {
		t.Errorf("wecom = %+v", wc)
	}
	if wh := reqs["/hook"]; wh.body["title"] != "标题" || wh.body["body"] != "内容" {
		t.Errorf("webhook = %v", wh.body)
	}
}

// 飞书/钉钉/企业微信出错时 HTTP 仍返回 200，必须解析返回体。
func TestNotifyBotErrorCodes(t *testing.T) {
	feishu, _ := notifyServer(t, `{"code":19021,"msg":"sign match fail or timestamp is not within one hour from current time"}`)
	ding, _ := notifyServer(t, `{"errcode":310000,"errmsg":"keywords not in content"}`)
	err := Notify(model.NotifySettings{FeishuURL: feishu.URL, DingTalkURL: ding.URL}, nil, "t", "b")
	if err == nil || !strings.Contains(err.Error(), "飞书：错误码 19021") || !strings.Contains(err.Error(), "钉钉：错误码 310000") {
		t.Errorf("err = %v", err)
	}
	if err := Notify(model.NotifySettings{}, nil, "t", "b"); err != ErrNoChannel {
		t.Errorf("no channel err = %v", err)
	}
	if ValidateNotify(model.NotifySettings{WeComURL: "qyapi.weixin.qq.com/x"}) == nil {
		t.Error("url without scheme should be rejected")
	}
}
