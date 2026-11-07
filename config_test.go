package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const sampleYAML = `
mixed-port: 7890
bind-address: '*'
mode: rule
external-controller: '127.0.0.1:9090'
external-ui: ui
secret: s3cret
tun:
  enable: true
  stack: mixed
proxy-groups:
  - name: Proxy
    type: select
    proxies: [Proxy1, Proxy2]
  - name: Auto
    type: url-test
    proxies: [Proxy1]
proxies:
  - {name: Proxy1, type: trojan}
  - {name: Proxy2, type: trojan}
rules:
  - 'MATCH,Proxy'
`

func parseDocument(t *testing.T, text string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("generated YAML is invalid: %v", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		t.Fatal("generated YAML has no mapping root")
	}
	return doc.Content[0]
}

func tunEnabled(t *testing.T, text string) bool {
	t.Helper()
	root := parseDocument(t, text)
	tun := mappingChild(root, "tun")
	if tun == nil || tun.Kind != yaml.MappingNode {
		t.Fatal("rendered config has no tun mapping")
	}
	enable := mappingChild(tun, "enable")
	if enable == nil {
		t.Fatal("rendered config has no tun.enable")
	}
	return enable.Value == "true"
}

func TestReadMihomoConfig(t *testing.T) {
	cfg, err := readMihomoConfig(sampleYAML)
	if err != nil {
		t.Fatalf("readMihomoConfig: %v", err)
	}

	if cfg.ProxyHost != "127.0.0.1" || cfg.ProxyPort != 7890 || cfg.ProxyIsSocks {
		t.Errorf("proxy = %q:%d socks=%v", cfg.ProxyHost, cfg.ProxyPort, cfg.ProxyIsSocks)
	}
	if !cfg.HasTun {
		t.Error("tun section was not detected")
	}
	if len(cfg.Selectors) != 1 {
		t.Fatalf("selectors = %d, want 1 (only select groups)", len(cfg.Selectors))
	}
	sel := cfg.Selectors[0]
	if sel.Name != "Proxy" || len(sel.Outbounds) != 2 || sel.DefaultIndex != -1 {
		t.Errorf("selector parsed incorrectly: %+v", sel)
	}
	if !cfg.ClashAPI.IsConfigured() || cfg.ClashAPI.ExternalController != "127.0.0.1:9090" ||
		cfg.ClashAPI.Secret != "s3cret" || cfg.ClashAPI.ExternalUI != "ui" {
		t.Errorf("clash api parsed incorrectly: %+v", cfg.ClashAPI)
	}
	if cfg.InjectAPI {
		t.Error("API should not be injected when external-controller is present")
	}
	if err := checkMihomoConfig(cfg); err != nil {
		t.Errorf("checkMihomoConfig: %v", err)
	}
	if strings.Join(cfg.Modes, ",") != "rule,global,direct" {
		t.Errorf("modes = %v", cfg.Modes)
	}

	if !tunEnabled(t, cfg.TextWithTun) {
		t.Error("TextWithTun should enable tun")
	}
	if tunEnabled(t, cfg.TextWithoutTun) {
		t.Error("TextWithoutTun should disable tun")
	}
	if !strings.Contains(cfg.TextWithoutTun, "7890") || !strings.Contains(cfg.TextWithoutTun, "MATCH,Proxy") {
		t.Error("rendered config lost unrelated settings")
	}
}

func TestReadMihomoConfigProviderOnlyGroups(t *testing.T) {
	text := `
mixed-port: 7890
proxy-groups:
  - name: ProviderOnly
    type: select
    use: [nodes]
  - name: Mixed
    type: select
    proxies: [DIRECT]
    use: [nodes]
`
	cfg, err := readMihomoConfig(text)
	if err != nil {
		t.Fatalf("readMihomoConfig: %v", err)
	}
	if len(cfg.Selectors) != 2 {
		t.Fatalf("selectors = %d, want 2", len(cfg.Selectors))
	}
	if cfg.Selectors[0].Name != "ProviderOnly" || len(cfg.Selectors[0].Outbounds) != 0 {
		t.Errorf("provider-only group parsed incorrectly: %+v", cfg.Selectors[0])
	}
	if cfg.Selectors[1].Name != "Mixed" || len(cfg.Selectors[1].Outbounds) != 1 {
		t.Errorf("mixed group parsed incorrectly: %+v", cfg.Selectors[1])
	}
}

func TestReadMihomoConfigInjectsAPI(t *testing.T) {
	text := `
mixed-port: 7890
proxy-groups:
  - name: Proxy
    type: select
    proxies: [A, B]
`
	cfg, err := readMihomoConfig(text)
	if err != nil {
		t.Fatalf("readMihomoConfig: %v", err)
	}
	if !cfg.InjectAPI {
		t.Error("API should be injected when no controller is configured")
	}
	if cfg.ClashAPI.ExternalController != defaultExternalController || cfg.ClashAPI.Secret != defaultExternalSecret {
		t.Errorf("injected API = %+v", cfg.ClashAPI)
	}

	noGroups := "mixed-port: 7890\n"
	cfg, err = readMihomoConfig(noGroups)
	if err != nil {
		t.Fatalf("readMihomoConfig: %v", err)
	}
	if !cfg.InjectAPI || !cfg.ClashAPI.IsConfigured() {
		t.Error("API should be injected even without select groups")
	}
	if cfg.ClashAPI.ExternalController != defaultExternalController || cfg.ClashAPI.Secret != defaultExternalSecret {
		t.Errorf("injected API = %+v", cfg.ClashAPI)
	}
}

func TestReadMihomoConfigModes(t *testing.T) {
	cfg, err := readMihomoConfig("mixed-port: 1080\nscript:\n  code: |\n    def main(ctx, md):\n      return md\n")
	if err != nil {
		t.Fatalf("readMihomoConfig: %v", err)
	}
	if strings.Join(cfg.Modes, ",") != "rule,global,direct,script" {
		t.Errorf("modes with script = %v", cfg.Modes)
	}
}

func TestReadMihomoConfigPortPreference(t *testing.T) {
	cases := []struct {
		text  string
		port  int
		socks bool
	}{
		{"mixed-port: 7890\nport: 8080\nsocks-port: 1080\n", 7890, false},
		{"port: 8080\nsocks-port: 1080\n", 8080, false},
		{"socks-port: 1080\n", 1080, true},
	}
	for _, c := range cases {
		cfg, err := readMihomoConfig(c.text)
		if err != nil {
			t.Fatalf("readMihomoConfig(%q): %v", c.text, err)
		}
		if cfg.ProxyPort != c.port || cfg.ProxyIsSocks != c.socks {
			t.Errorf("text %q => %d socks=%v, want %d socks=%v",
				c.text, cfg.ProxyPort, cfg.ProxyIsSocks, c.port, c.socks)
		}
	}
}

func TestProxyHostFromBindAddress(t *testing.T) {
	cases := map[string]string{
		"":            "127.0.0.1",
		"*":           "127.0.0.1",
		"0.0.0.0":     "127.0.0.1",
		"::":          "127.0.0.1",
		"[::]":        "127.0.0.1",
		"127.0.0.1":   "127.0.0.1",
		"192.168.1.5": "192.168.1.5",
		"1.2.3.4, ::": "1.2.3.4",
	}
	for value, want := range cases {
		if got := proxyHostFromBindAddress(value); got != want {
			t.Errorf("proxyHostFromBindAddress(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestReadMihomoConfigErrors(t *testing.T) {
	if _, err := readMihomoConfig("mixed-port: [unclosed"); err == nil {
		t.Error("expected an error for invalid YAML")
	}
	if _, err := readMihomoConfig("- a\n- b\n"); err == nil {
		t.Error("expected an error for a non-mapping root")
	}

	cfg, err := readMihomoConfig("mode: rule\n")
	if err != nil {
		t.Fatalf("readMihomoConfig: %v", err)
	}
	if err := checkMihomoConfig(cfg); err == nil {
		t.Error("expected an error when there is no proxy port")
	}
}

func TestConfigWithoutTun(t *testing.T) {
	text := "mixed-port: 7890\n"
	cfg, err := readMihomoConfig(text)
	if err != nil {
		t.Fatalf("readMihomoConfig: %v", err)
	}
	if cfg.HasTun {
		t.Error("tun should not be detected")
	}
	if cfg.TextWithTun != text || cfg.TextWithoutTun != text {
		t.Error("config without tun should be used as-is")
	}
}

func TestClashAPIDashboardURL(t *testing.T) {
	if got := (clashAPIConfig{}).DashboardURL(); got != "" {
		t.Errorf("DashboardURL without controller = %q, want empty", got)
	}

	cfg := clashAPIConfig{ExternalController: "127.0.0.1:9090"}
	if got, want := cfg.DashboardURL(), onlineDashboardURL+"?hostname=127.0.0.1&port=9090"; got != want {
		t.Errorf("DashboardURL = %q, want %q", got, want)
	}

	cfg.ExternalController = "0.0.0.0:9090"
	if got, want := cfg.DashboardURL(), onlineDashboardURL+"?hostname=127.0.0.1&port=9090"; got != want {
		t.Errorf("DashboardURL = %q, want %q", got, want)
	}
	if got, want := cfg.ConnectAddress(), "127.0.0.1:9090"; got != want {
		t.Errorf("ConnectAddress = %q, want %q", got, want)
	}

	cfg = clashAPIConfig{ExternalController: "127.0.0.1:9090", Secret: "a b&c"}
	if got, want := cfg.DashboardURL(), onlineDashboardURL+"?hostname=127.0.0.1&port=9090&secret=a+b%26c"; got != want {
		t.Errorf("DashboardURL = %q, want %q", got, want)
	}

	cfg = clashAPIConfig{ExternalController: "127.0.0.1:9090", ExternalUI: "ui", Secret: "s3cret"}
	if got, want := cfg.DashboardURL(), "http://127.0.0.1:9090/ui/?secret=s3cret"; got != want {
		t.Errorf("DashboardURL with local external-ui = %q, want %q", got, want)
	}

	cfg = clashAPIConfig{ExternalController: "127.0.0.1:9090", ExternalUI: " https://example.com/ui/ "}
	if got, want := cfg.DashboardURL(), "https://example.com/ui/"; got != want {
		t.Errorf("DashboardURL with URL external-ui = %q, want %q", got, want)
	}
}

func TestRenderKeepsCommentsAndFlowStyle(t *testing.T) {
	text := "# leading comment\nmixed-port: 7890\ntun:\n  enable: false\nproxies:\n  - {name: A, type: direct}\n"
	cfg, err := readMihomoConfig(text)
	if err != nil {
		t.Fatalf("readMihomoConfig: %v", err)
	}
	if !tunEnabled(t, cfg.TextWithTun) {
		t.Error("tun should be enabled in TextWithTun")
	}
	if !strings.Contains(cfg.TextWithTun, "leading comment") {
		t.Error("comments should survive rendering")
	}
	if !strings.Contains(cfg.TextWithTun, "{name: A, type: direct}") {
		t.Error("flow style should survive rendering")
	}
}
