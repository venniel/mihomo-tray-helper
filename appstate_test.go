package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAppStateMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := newAppState(path)

	if _, ok := s.getBool("tun"); ok {
		t.Error("missing state should have no boolean values")
	}
}

func TestAppStateCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newAppState(path)
	if _, ok := s.getBool("tun"); ok {
		t.Error("corrupt state should be ignored")
	}
}

func TestAppStateBoolPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := newAppState(path)

	if err := s.setBool("tun", true); err != nil {
		t.Fatal(err)
	}
	if err := s.setBool("systemProxy", false); err != nil {
		t.Fatal(err)
	}

	if v, ok := s.getBool("tun"); !ok || !v {
		t.Errorf("getBool(tun) = %v, %v", v, ok)
	}

	reloaded := newAppState(path)
	if v, ok := reloaded.getBool("tun"); !ok || !v {
		t.Errorf("reloaded getBool(tun) = %v, %v", v, ok)
	}
	if v, ok := reloaded.getBool("systemProxy"); !ok || v {
		t.Errorf("reloaded getBool(systemProxy) = %v, %v", v, ok)
	}
}

func TestAppStatePreservesUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	initial := `{"custom":"value"}`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newAppState(path)
	if err := s.setBool("tun", true); err != nil {
		t.Fatal(err)
	}

	reloaded := newAppState(path)
	if v, ok := reloaded.root["custom"]; !ok || v != "value" {
		t.Errorf("unknown key lost: %v, %v", v, ok)
	}
}

func TestAppStateDropsLegacySelectors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	initial := `{"custom":"value","selectors":{"proxy-select":"Proxy1"}}`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newAppState(path)
	if _, ok := s.root["selectors"]; ok {
		t.Error("legacy selectors key should not be loaded")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
	if _, ok := root["selectors"]; ok {
		t.Error("legacy selectors key should be removed")
	}
	if v, ok := root["custom"]; !ok || v != "value" {
		t.Errorf("unknown key lost: %v, %v", v, ok)
	}
}
