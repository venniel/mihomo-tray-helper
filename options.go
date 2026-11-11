package main

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	optionsFilename = "MTH.ini"
	optionsSection  = "MTH"

	legacyOptionsFilename = "mihomo-go.ini"
	legacyOptionsSection  = "mihomo-go"
)

// migrateLegacyOptions copies the previous settings file to MTH.ini once, so
// existing installations keep their core, config and runtime selections. The
// old file is left untouched.
func migrateLegacyOptions(dir string) {
	newPath := filepath.Join(dir, optionsFilename)
	if fileExists(newPath) {
		return
	}

	oldPath := filepath.Join(dir, legacyOptionsFilename)
	lines, hadBOM, ok := readTextLines(oldPath)
	if !ok {
		return
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if strings.EqualFold(name, legacyOptionsSection) {
				lines[i] = "[" + optionsSection + "]"
			}
		}
	}

	_ = writeTextLines(newPath, lines, hadBOM)
}

type tunStartMode int

const (
	tsmOn tunStartMode = iota
	tsmOff
)

type selectorMenuLayout int

const (
	smlAuto selectorMenuLayout = iota
	smlFlat
	smlNested
)

type droverOptions struct {
	CoreFile           string
	ConfigFile         string
	SystemProxyAuto    bool
	TunStartMode       tunStartMode
	SelectorMenuLayout selectorMenuLayout
	LogFile            string
}

func loadOptions(filename string) droverOptions {
	currentDir := includeTrailingPathDelimiter(filepath.Dir(filename))
	opts := droverOptions{
		CoreFile:           filepath.Join(currentDir, "mihomo.exe"),
		ConfigFile:         filepath.Join(currentDir, "config.yaml"),
		SystemProxyAuto:    false,
		TunStartMode:       tsmOff,
		SelectorMenuLayout: smlAuto,
	}

	lines, _, ok := readTextLines(filename)
	if !ok {
		return opts
	}

	if value, found := iniGet(lines, optionsSection, "mihomo-core"); found {
		opts.CoreFile = resolveOptionPath(currentDir, value, "mihomo.exe")
	}

	if value, found := iniGet(lines, optionsSection, "config-file"); found {
		opts.ConfigFile = resolveOptionPath(currentDir, value, "config.yaml")
	}

	if value, found := iniGet(lines, optionsSection, "tun-start-mode"); found {
		opts.TunStartMode = parseTunStartMode(value)
	}
	if value, found := iniGet(lines, optionsSection, "system-proxy-auto"); found {
		opts.SystemProxyAuto = parseBool(value, false)
	}
	if value, found := iniGet(lines, optionsSection, "selector-menu-layout"); found {
		opts.SelectorMenuLayout = parseSelectorMenuLayout(value)
	}
	if value, found := iniGet(lines, optionsSection, "log-file"); found {
		if value != "" && !strings.Contains(value, ":") {
			value = currentDir + value
		}
		opts.LogFile = value
	}

	return opts
}

func resolveOptionPath(baseDir, value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(baseDir, value)
}

func saveOptions(filename, coreFile, configFile string, layout selectorMenuLayout) error {
	lines, hadBOM, _ := readTextLines(filename)
	if lines == nil {
		lines = []string{}
	}

	lines = iniSet(lines, optionsSection, "mihomo-core", coreFile)
	lines = iniSet(lines, optionsSection, "config-file", configFile)
	lines = iniSet(lines, optionsSection, "selector-menu-layout", layoutToValue(layout))

	return writeTextLines(filename, lines, hadBOM)
}

// saveRuntimeState stores the current TUN and system proxy selections in the
// program's INI file so they survive a restart without a separate state file.
func saveRuntimeState(filename string, tun bool, systemProxy bool) error {
	lines, hadBOM, _ := readTextLines(filename)
	if lines == nil {
		lines = []string{}
	}

	tunValue := "off"
	if tun {
		tunValue = "on"
	}
	proxyValue := "0"
	if systemProxy {
		proxyValue = "1"
	}

	lines = iniSet(lines, optionsSection, "tun-start-mode", tunValue)
	lines = iniSet(lines, optionsSection, "system-proxy-auto", proxyValue)

	return writeTextLines(filename, lines, hadBOM)
}

func writeTextLines(filename string, lines []string, hadBOM bool) error {
	text := strings.Join(lines, "\r\n")
	if !strings.HasSuffix(text, "\r\n") {
		text += "\r\n"
	}
	if hadBOM {
		text = "\uFEFF" + text
	}
	return os.WriteFile(filename, []byte(text), 0o644)
}

func parseTunStartMode(value string) tunStartMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "off", "0":
		return tsmOff
	default:
		return tsmOn
	}
}

func parseSelectorMenuLayout(value string) selectorMenuLayout {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "flat":
		return smlFlat
	case "nested":
		return smlNested
	default:
		return smlAuto
	}
}

func layoutToValue(layout selectorMenuLayout) string {
	switch layout {
	case smlFlat:
		return "flat"
	case smlNested:
		return "nested"
	default:
		return "auto"
	}
}

func parseBool(value string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func includeTrailingPathDelimiter(path string) string {
	if path == "" {
		return string(os.PathSeparator)
	}
	last := path[len(path)-1]
	if last != '\\' && last != '/' {
		return path + string(os.PathSeparator)
	}
	return path
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// --- INI helpers ---------------------------------------------------------

func readTextLines(filename string) ([]string, bool, bool) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, false, false
	}
	text, hadBOM := decodeTextBytes(data)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{}, hadBOM, true
	}
	return strings.Split(text, "\n"), hadBOM, true
}

func iniGet(lines []string, section, key string) (string, bool) {
	inSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			inSection = strings.EqualFold(name, section)
			continue
		}
		if !inSection {
			continue
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq < 0 {
			continue
		}
		name := strings.TrimSpace(trimmed[:eq])
		if !strings.EqualFold(name, key) {
			continue
		}
		value := strings.TrimSpace(trimmed[eq+1:])
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = strings.TrimSpace(value[1 : len(value)-1])
			}
		}
		return value, true
	}
	return "", false
}

func iniSet(lines []string, section, key, value string) []string {
	sectionStart := -1
	sectionEnd := len(lines)
	inSection := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if inSection {
				sectionEnd = i
				break
			}
			if strings.EqualFold(name, section) {
				sectionStart = i
				inSection = true
			}
		}
	}

	newLine := key + "=" + strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ")

	if sectionStart < 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "["+section+"]", newLine)
		return lines
	}

	for i := sectionStart + 1; i < sectionEnd; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq < 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(trimmed[:eq]), key) {
			lines[i] = newLine
			return lines
		}
	}

	insertAt := sectionEnd
	for insertAt > sectionStart+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}
	lines = append(lines, "")
	copy(lines[insertAt+1:], lines[insertAt:])
	lines[insertAt] = newLine
	return lines
}
