package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestQuerySelectorSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"proxies": {
				"机场中转": {"all": ["DIRECT", "香港01"], "now": "香港01"},
				"ChatGPT": {"all": ["DIRECT"], "now": "DIRECT"},
				"OTHER": {"all": ["X"], "now": "X"}
			}
		}`))
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	s := &coreSupervisor{api: newCoreAPIClient(clashAPIConfig{ExternalController: parsed.Host}, newLogger(""))}

	snapshot := s.querySelectorSnapshot([]string{"机场中转", "ChatGPT", "Missing"})

	if got := snapshot.States["机场中转"]; got != "香港01" {
		t.Errorf("now = %q, want 香港01", got)
	}
	if got := snapshot.Members["机场中转"]; len(got) != 2 || got[0] != "DIRECT" || got[1] != "香港01" {
		t.Errorf("members = %v", got)
	}
	if got := snapshot.States["ChatGPT"]; got != "DIRECT" {
		t.Errorf("ChatGPT now = %q, want DIRECT", got)
	}
	if _, ok := snapshot.States["Missing"]; ok {
		t.Error("unknown group should not produce a state")
	}
	if _, ok := snapshot.States["OTHER"]; ok {
		t.Error("unrequested group should not be included")
	}
}

func TestQuerySelectorSnapshotUnavailable(t *testing.T) {
	s := &coreSupervisor{api: newCoreAPIClient(clashAPIConfig{}, newLogger(""))}
	snapshot := s.querySelectorSnapshot([]string{"Proxy"})
	if len(snapshot.States) != 0 || len(snapshot.Members) != 0 {
		t.Errorf("snapshot should be empty without an API, got %+v", snapshot)
	}
}
