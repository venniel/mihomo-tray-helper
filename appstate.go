package main

import (
	"encoding/json"
	"os"
	"sync"
)

type appState struct {
	mu       sync.Mutex
	filePath string
	root     map[string]any
}

func newAppState(filePath string) *appState {
	s := &appState{
		filePath: filePath,
		root:     map[string]any{},
	}
	s.load()
	return s
}

func (s *appState) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.root = map[string]any{}

	data, err := os.ReadFile(s.filePath)
	if err != nil || len(data) == 0 {
		return
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return
	}
	// Selector choices are owned by the core now; drop data from older versions.
	if _, ok := root["selectors"]; ok {
		delete(root, "selectors")
		s.root = root
		_ = s.save()
		return
	}
	s.root = root
}

func (s *appState) buildJSONText() []byte {
	data, err := json.MarshalIndent(s.root, "", "  ")
	if err != nil {
		return []byte("{}")
	}
	return data
}

func (s *appState) save() error {
	return os.WriteFile(s.filePath, s.buildJSONText(), 0o644)
}

func (s *appState) getBool(name string) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.root[name].(bool)
	return value, ok
}

func (s *appState) setBool(name string, value bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.root[name] = value
	return s.save()
}
