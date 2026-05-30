package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xray-panel/internal/app"
	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

type memStore struct {
	state *model.PanelState
}

func (m *memStore) Load() (*model.PanelState, error) {
	if m.state == nil {
		m.state = &model.PanelState{
			Subscriptions: []model.Subscription{},
			Inbounds: []model.Inbound{
				{Tag: "in-0", Protocol: "socks", Listen: "0.0.0.0", Port: 10808, UDP: true},
				{Tag: "in-1", Protocol: "http", Listen: "0.0.0.0", Port: 10809},
			},
		}
	}
	return m.state, nil
}
func (m *memStore) Save(state *model.PanelState) error { m.state = state; return nil }
func (m *memStore) Lock()                               {}
func (m *memStore) Unlock()                             {}

func setupTestApp(t *testing.T) *Server {
	t.Helper()
	pw := "testpw"
	a, err := app.New(app.Config{
		Store:      &memStore{},
		XrayProc:   &service.FakeXray{Alive: true},
		ConfigPath: "/tmp/test-config.json",
		PanelPort:  2017,
		Password:   &pw,
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(a)
}

func TestLoginSuccess(t *testing.T) {
	srv := setupTestApp(t)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"password": "testpw"})
	resp, err := http.Post(ts.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["token"] == nil || result["token"] == "" {
		t.Error("token should not be empty")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	srv := setupTestApp(t)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"password": "wrong"})
	resp, _ := http.Post(ts.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestMeWithoutAuth(t *testing.T) {
	srv := setupTestApp(t)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	resp, _ := http.Get(ts.URL + "/api/auth/me")
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestCreateInbound(t *testing.T) {
	srv := setupTestApp(t)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// Login first
	loginBody, _ := json.Marshal(map[string]string{"password": "testpw"})
	loginResp, _ := http.Post(ts.URL+"/api/auth/login", "application/json", bytes.NewReader(loginBody))
	var loginResult map[string]any
	json.NewDecoder(loginResp.Body).Decode(&loginResult)
	token := loginResult["token"].(string)

	// Create inbound
	body, _ := json.Marshal(map[string]any{"protocol": "socks", "port": 1080})
	req, _ := http.NewRequest("POST", ts.URL+"/api/inbounds", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
}
