package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type configSelector struct {
	Name         string
	Outbounds    []string
	DefaultIndex int
}

type clashAPIConfig struct {
	ExternalController string
	ExternalUI         string
	Secret             string
}

func (c clashAPIConfig) IsConfigured() bool {
	return strings.TrimSpace(c.ExternalController) != ""
}

// ConnectAddress returns the address the HTTP client should dial. A controller
// bound to a wildcard address is reached through loopback instead.
func (c clashAPIConfig) ConnectAddress() string {
	addr := strings.TrimSpace(c.ExternalController)
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	addr = strings.TrimSuffix(addr, "/")

	switch {
	case strings.HasPrefix(addr, "0.0.0.0:"):
		addr = "127.0.0.1:" + addr[len("0.0.0.0:"):]
	case strings.HasPrefix(addr, "[::]:"):
		addr = "127.0.0.1:" + addr[len("[::]:"):]
	case strings.HasPrefix(addr, ":"):
		addr = "127.0.0.1" + addr
	case addr == "0.0.0.0", addr == "::", addr == "[::]":
		addr = "127.0.0.1"
	}
	return addr
}

func (c clashAPIConfig) DashboardURL() string {
	if !c.IsConfigured() {
		return ""
	}
	if ui := strings.TrimSpace(c.ExternalUI); hasPrefixFold(ui, "http://") || hasPrefixFold(ui, "https://") {
		return ui
	}

	base := "http://" + c.ConnectAddress() + "/ui/"
	if c.Secret != "" {
		base += "?secret=" + url.QueryEscape(c.Secret)
	}
	return base
}

type mihomoConfig struct {
	ClashAPI  clashAPIConfig
	InjectAPI bool
	Selectors []configSelector
	Modes     []string

	ProxyHost    string
	ProxyPort    int
	ProxyIsSocks bool

	HasTun         bool
	TextWithTun    string
	TextWithoutTun string
}

func readConfigText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("Configuration file not found.")
		}
		return "", fmt.Errorf("Failed to read configuration file.")
	}
	return utf8TextFromBytes(data), nil
}

func utf8TextFromBytes(data []byte) string {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return string(data[3:])
	}
	return string(data)
}

func readMihomoConfig(text string) (mihomoConfig, error) {
	var cfg mihomoConfig

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return cfg, fmt.Errorf("Configuration file is corrupted or contains invalid YAML.")
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return cfg, fmt.Errorf("Invalid configuration: expected a YAML mapping at the top level.")
	}
	root := doc.Content[0]

	mixedPort := intFromNode(mappingChild(root, "mixed-port"))
	httpPort := intFromNode(mappingChild(root, "port"))
	socksPort := intFromNode(mappingChild(root, "socks-port"))
	switch {
	case mixedPort > 0:
		cfg.ProxyPort = mixedPort
	case httpPort > 0:
		cfg.ProxyPort = httpPort
	case socksPort > 0:
		cfg.ProxyPort = socksPort
		cfg.ProxyIsSocks = true
	}

	cfg.ProxyHost = proxyHostFromBindAddress(stringFromNode(mappingChild(root, "bind-address")))

	if groups := mappingChild(root, "proxy-groups"); groups != nil && groups.Kind == yaml.SequenceNode {
		for _, group := range groups.Content {
			if group.Kind != yaml.MappingNode {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(stringFromNode(mappingChild(group, "type"))), "select") {
				continue
			}
			name := strings.TrimSpace(stringFromNode(mappingChild(group, "name")))
			if name == "" {
				continue
			}
			var outbounds []string
			if list := mappingChild(group, "proxies"); list != nil && list.Kind == yaml.SequenceNode {
				for _, item := range list.Content {
					if value := strings.TrimSpace(stringFromNode(item)); value != "" {
						outbounds = append(outbounds, value)
					}
				}
			}
			if len(outbounds) == 0 {
				continue
			}
			cfg.Selectors = append(cfg.Selectors, configSelector{
				Name:         name,
				Outbounds:    outbounds,
				DefaultIndex: -1,
			})
		}
	}

	tun := mappingChild(root, "tun")
	cfg.HasTun = tun != nil && tun.Kind == yaml.MappingNode

	cfg.ClashAPI = clashAPIConfig{
		ExternalController: strings.TrimSpace(stringFromNode(mappingChild(root, "external-controller"))),
		ExternalUI:         strings.TrimSpace(stringFromNode(mappingChild(root, "external-ui"))),
		Secret:             stringFromNode(mappingChild(root, "secret")),
	}
	if !cfg.ClashAPI.IsConfigured() && len(cfg.Selectors) > 0 {
		cfg.ClashAPI.ExternalController = "127.0.0.1:9090"
		cfg.ClashAPI.Secret = newGUIDString()
		cfg.InjectAPI = true
	}

	cfg.Modes = []string{"rule", "global", "direct"}
	if mappingChild(root, "script") != nil {
		cfg.Modes = append(cfg.Modes, "script")
	}

	cfg.TextWithTun = text
	cfg.TextWithoutTun = text
	if cfg.HasTun {
		withTun, err := renderConfigWithTun(text, true)
		if err != nil {
			return cfg, fmt.Errorf("Failed to serialize configuration.")
		}
		withoutTun, err := renderConfigWithTun(text, false)
		if err != nil {
			return cfg, fmt.Errorf("Failed to serialize configuration.")
		}
		cfg.TextWithTun = withTun
		cfg.TextWithoutTun = withoutTun
	}

	return cfg, nil
}

func checkMihomoConfig(cfg mihomoConfig) error {
	if cfg.ProxyPort < 1 {
		return fmt.Errorf("No suitable mixed-port or port found for the system proxy.")
	}
	return nil
}

func proxyHostFromBindAddress(value string) string {
	host := strings.TrimSpace(value)
	if index := strings.IndexByte(host, ','); index >= 0 {
		host = strings.TrimSpace(host[:index])
	}
	switch strings.ToLower(host) {
	case "", "*", "0.0.0.0", "::", "[::]":
		return "127.0.0.1"
	}
	return host
}

func renderConfigWithTun(text string, enable bool) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return "", err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", fmt.Errorf("invalid root")
	}
	root := doc.Content[0]

	tun := mappingChild(root, "tun")
	if tun == nil || tun.Kind != yaml.MappingNode {
		tun = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMappingNode(root, "tun", tun)
	}
	setMappingBool(tun, "enable", enable)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		enc.Close()
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func mappingChild(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if strings.EqualFold(m.Content[i].Value, key) {
			return m.Content[i+1]
		}
	}
	return nil
}

func setMappingNode(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if strings.EqualFold(m.Content[i].Value, key) {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value)
}

func setMappingBool(m *yaml.Node, key string, value bool) {
	scalar := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(value)}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if strings.EqualFold(m.Content[i].Value, key) {
			m.Content[i+1] = scalar
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		scalar)
}

func stringFromNode(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func intFromNode(n *yaml.Node) int {
	if n == nil || n.Kind != yaml.ScalarNode {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(n.Value))
	if err != nil {
		return 0
	}
	return value
}

func newGUIDString() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "{00000000-0000-0000-0000-000000000000}"
	}
	b[6] = (b[6] & 0x0F) | 0x40
	b[8] = (b[8] & 0x3F) | 0x80
	return fmt.Sprintf("{%08X-%04X-%04X-%04X-%012X}",
		uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]),
		uint16(b[4])<<8|uint16(b[5]),
		uint16(b[6])<<8|uint16(b[7]),
		uint16(b[8])<<8|uint16(b[9]),
		uint64(b[10])<<40|uint64(b[11])<<32|uint64(b[12])<<24|uint64(b[13])<<16|uint64(b[14])<<8|uint64(b[15]))
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}
