package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewInstallLoadsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.json")
	s := NewJSONStore(path, 10808, 10809)

	state, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Inbounds) != 2 {
		t.Fatalf("expected 2 default inbounds, got %d", len(state.Inbounds))
	}
	if state.Inbounds[0].Tag != "in-0" {
		t.Errorf("first inbound tag = %s, want in-0", state.Inbounds[0].Tag)
	}
	if state.Inbounds[0].Port != 10808 {
		t.Errorf("first inbound port = %d, want 10808", state.Inbounds[0].Port)
	}
	if state.Inbounds[0].Protocol != "socks" {
		t.Errorf("first inbound protocol = %s, want socks", state.Inbounds[0].Protocol)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.json")
	s := NewJSONStore(path, 10808, 10809)

	state, _ := s.Load()
	state.DefaultOutbound = "direct"
	state.InboundSeq = 5
	if err := s.Save(state); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DefaultOutbound != "direct" {
		t.Errorf("default_outbound = %s, want direct", loaded.DefaultOutbound)
	}
	if loaded.InboundSeq != 5 {
		t.Errorf("inbound_seq = %d, want 5", loaded.InboundSeq)
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.json")
	s := NewJSONStore(path, 10808, 10809)

	state, _ := s.Load()
	state.DefaultOutbound = "node-0"
	s.Save(state)

	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error(".tmp file should not exist after successful save")
	}
}

func TestMigrationSeedsRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.json")
	old := map[string]any{
		"rules": []any{
			map[string]any{"type": "domain-suffix", "value": "google.com", "outbound": "direct"},
		},
	}
	data, _ := json.Marshal(old)
	os.WriteFile(path, data, 0644)

	s := NewJSONStore(path, 10808, 10809)
	state, _ := s.Load()
	if len(state.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(state.Rules))
	}
	if state.Rules[0].ID != 1 {
		t.Errorf("rule id = %d, want 1", state.Rules[0].ID)
	}
	if !state.Rules[0].Enabled {
		t.Error("rule should default to enabled")
	}
}
