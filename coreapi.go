package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

const selectorSnapshotTimeout = 500 * time.Millisecond

type selectorSnapshot struct {
	States  map[string]string
	Members map[string][]string
}

// querySelectorSnapshot reads the current member lists and selections directly
// from the core. It is called on the UI thread right before a menu is built so
// provider-backed groups show their real members.
func (s *coreSupervisor) querySelectorSnapshot(names []string) selectorSnapshot {
	snapshot := selectorSnapshot{
		States:  make(map[string]string, len(names)),
		Members: make(map[string][]string, len(names)),
	}
	if len(names) == 0 || !s.api.isConfigured() {
		return snapshot
	}

	text, err := s.api.request("GET", "/proxies", "", selectorSnapshotTimeout)
	if err != nil {
		return snapshot
	}

	var payload struct {
		Proxies map[string]struct {
			All []string `json:"all"`
			Now string   `json:"now"`
		} `json:"proxies"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return snapshot
	}

	for _, name := range names {
		proxy, ok := payload.Proxies[name]
		if !ok {
			continue
		}
		if len(proxy.All) > 0 {
			snapshot.Members[name] = slices.Clone(proxy.All)
		}
		if proxy.Now != "" {
			snapshot.States[name] = proxy.Now
		}
	}
	return snapshot
}

func (s *coreSupervisor) initAPICheck() {
	if !s.api.isConfigured() {
		s.apiCheckActive = false
		return
	}

	now := time.Now()
	s.apiCheckActive = true
	s.apiNextCheck = now.Add(apiCheckDelay)
	s.apiDeadline = now.Add(apiCheckTimeout)
}

func (s *coreSupervisor) checkAPIStatus() {
	if !s.apiCheckActive {
		return
	}
	if s.cmd == nil {
		s.apiCheckActive = false
		return
	}

	now := time.Now()
	if now.Before(s.apiNextCheck) {
		return
	}
	if !now.Before(s.apiDeadline) {
		s.log("API check timeout, stopping checks.")
		s.apiCheckActive = false
		return
	}
	s.apiNextCheck = now.Add(apiCheckInterval)

	if s.api.checkReady() {
		s.log("API ready.")
		s.apiCheckActive = false
		s.notifyEvent(coreEvent{Kind: cekAPIReady})
		s.doQueryConfig()
	}
}

func (s *coreSupervisor) doQueryConfig() {
	text, err := s.api.request("GET", "/configs", "", time.Second)
	if err != nil {
		s.log(strings.TrimSpace("Config query failed. " + err.Error()))
		return
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil || obj == nil {
		return
	}

	event := coreEvent{Kind: cekConfigModes, Modes: slices.Clone(s.modes)}
	if mode, ok := obj["mode"].(string); ok {
		event.Mode = mode
	}

	s.log(strings.TrimSpace(fmt.Sprintf("Clash mode: %s, available modes: %d.", event.Mode, len(event.Modes))))
	s.notifyEvent(event)
}

func (s *coreSupervisor) doSetMode(mode string) {
	body, _ := json.Marshal(map[string]string{"mode": mode})
	if _, err := s.api.request("PATCH", "/configs", string(body), time.Second); err != nil {
		s.notifyEvent(coreEvent{Kind: cekError, Msg: "Failed to switch mode. " + err.Error()})
		return
	}
	s.notifyEvent(coreEvent{Kind: cekModeDone, Mode: mode})
}

func (s *coreSupervisor) doQuerySelectorStates(names []string, serial int) {
	states := make(map[string]string, len(names))

	text, err := s.api.request("GET", "/proxies", "", selectorQueryTimeout)
	if err != nil {
		// Core is likely unavailable; keep the cached values.
		s.notifyEvent(coreEvent{Kind: cekSelectorStates, SelectorNow: states, SelectorSerial: serial})
		return
	}

	var payload struct {
		Proxies map[string]struct {
			Now string `json:"now"`
		} `json:"proxies"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		s.notifyEvent(coreEvent{Kind: cekSelectorStates, SelectorNow: states, SelectorSerial: serial})
		return
	}

	for _, name := range names {
		if proxy, ok := payload.Proxies[name]; ok && proxy.Now != "" {
			states[name] = proxy.Now
		}
	}
	s.notifyEvent(coreEvent{Kind: cekSelectorStates, SelectorNow: states, SelectorSerial: serial})
}

func (s *coreSupervisor) doSetSelectors(tasks []selectorTask, requestID int) {
	hasError := false
	for _, task := range tasks {
		body, _ := json.Marshal(map[string]string{"name": task.Value})
		if _, err := s.api.request("PUT", "/proxies/"+url.PathEscape(task.Name), string(body), time.Second); err != nil {
			hasError = true
			s.log(strings.TrimSpace(fmt.Sprintf("Selector update failed for %q. %s", task.Name, err.Error())))
		}
	}

	if hasError {
		s.notifyEvent(coreEvent{Kind: cekError, Msg: "Selector update failed."})
	}

	s.notifyEvent(coreEvent{Kind: cekSelectorDone, RequestID: requestID})

	if _, err := s.api.request("DELETE", "/connections", "", connectionsFlushLimit); err != nil {
		s.log(strings.TrimSpace("Connections flush failed. " + err.Error()))
	}
}
