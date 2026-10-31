package model

import (
	"encoding/json"
	"testing"
)

func TestAuthMarshalJSON(t *testing.T) {
	a := Auth{User: "admin", Password: "secret123"}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["user"] != "admin" {
		t.Errorf("user = %s, want admin", m["user"])
	}
	if m["pass"] != "secret123" {
		t.Errorf("pass = %s, want secret123", m["pass"])
	}
	if _, ok := m["password"]; ok {
		t.Error("JSON should not contain 'password' key")
	}
}

func TestAuthUnmarshalJSON_Pass(t *testing.T) {
	raw := `{"user":"admin","pass":"secret123"}`
	var a Auth
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatal(err)
	}
	if a.User != "admin" || a.Password != "secret123" {
		t.Errorf("got user=%s password=%s", a.User, a.Password)
	}
}

func TestAuthUnmarshalJSON_Password(t *testing.T) {
	raw := `{"user":"admin","password":"secret456"}`
	var a Auth
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatal(err)
	}
	if a.Password != "secret456" {
		t.Errorf("password = %s, want secret456", a.Password)
	}
}

func TestPanelStateRoundtrip(t *testing.T) {
	state := &PanelState{
		Inbounds: []Inbound{
			{Tag: "in-0", Protocol: "socks", Listen: "0.0.0.0", Port: 10808, UDP: true},
		},
		DefaultOutbound: "direct",
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var restored PanelState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Inbounds) != 1 {
		t.Fatalf("expected 1 inbound, got %d", len(restored.Inbounds))
	}
	if restored.Inbounds[0].Port != 10808 {
		t.Errorf("port = %d, want 10808", restored.Inbounds[0].Port)
	}
}
