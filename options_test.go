package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOptionsDefaults(t *testing.T) {
	dir := t.TempDir()
	opts := loadOptions(filepath.Join(dir, optionsFilename))

	if want := filepath.Join(dir, "mihomo.exe"); opts.CoreFile != want {
		t.Errorf("CoreFile = %q, want %q", opts.CoreFile, want)
	}
	if want := filepath.Join(dir, "config.yaml"); opts.ConfigFile != want {
		t.Errorf("ConfigFile = %q, want %q", opts.ConfigFile, want)
	}
	if opts.SystemProxyAuto {
		t.Error("SystemProxyAuto should default to false")
	}
	if opts.TunStartMode != tsmOff {
		t.Errorf("TunStartMode = %v, want off", opts.TunStartMode)
	}
	if opts.SelectorMenuLayout != smlAuto {
		t.Errorf("SelectorMenuLayout = %v, want auto", opts.SelectorMenuLayout)
	}
}

func TestLoadOptionsValues(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, optionsFilename)
	content := "[" + optionsSection + "]\r\n" +
		"mihomo-core = core\\mihomo-windows-amd64-v1.exe\r\n" +
		"config-file = profile.yml\r\n" +
		"tun-start-mode = on\r\n" +
		"system-proxy-auto = 1\r\n" +
		"selector-menu-layout = nested\r\n" +
		"log-file = drover.log\r\n"
	if err := os.WriteFile(ini, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := loadOptions(ini)
	if want := filepath.Join(dir, "core", "mihomo-windows-amd64-v1.exe"); opts.CoreFile != want {
		t.Errorf("CoreFile = %q, want %q", opts.CoreFile, want)
	}
	if want := filepath.Join(dir, "profile.yml"); opts.ConfigFile != want {
		t.Errorf("ConfigFile = %q, want %q", opts.ConfigFile, want)
	}
	if opts.TunStartMode != tsmOn {
		t.Errorf("TunStartMode = %v, want on", opts.TunStartMode)
	}
	if !opts.SystemProxyAuto {
		t.Error("SystemProxyAuto should be true")
	}
	if opts.SelectorMenuLayout != smlNested {
		t.Errorf("SelectorMenuLayout = %v, want nested", opts.SelectorMenuLayout)
	}
	if want := filepath.Join(dir, "drover.log"); opts.LogFile != want {
		t.Errorf("LogFile = %q, want %q", opts.LogFile, want)
	}
}

func TestLoadOptionsAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, optionsFilename)
	core := filepath.Join(dir, "bin", "mihomo.exe")
	config := filepath.Join(dir, "cfg", "custom.yml")
	content := "[" + optionsSection + "]\r\n" +
		"mihomo-core = " + core + "\r\n" +
		"config-file = " + config + "\r\n"
	if err := os.WriteFile(ini, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := loadOptions(ini)
	if opts.CoreFile != core {
		t.Errorf("CoreFile = %q, want %q", opts.CoreFile, core)
	}
	if opts.ConfigFile != config {
		t.Errorf("ConfigFile = %q, want %q", opts.ConfigFile, config)
	}
}

func TestLoadOptionsEmptyPathsUseDefaults(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, optionsFilename)
	content := "[" + optionsSection + "]\r\nmihomo-core =\r\nconfig-file =\r\n"
	if err := os.WriteFile(ini, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := loadOptions(ini)
	if want := filepath.Join(dir, "mihomo.exe"); opts.CoreFile != want {
		t.Errorf("CoreFile = %q, want %q", opts.CoreFile, want)
	}
	if want := filepath.Join(dir, "config.yaml"); opts.ConfigFile != want {
		t.Errorf("ConfigFile = %q, want %q", opts.ConfigFile, want)
	}
}

func TestSaveOptionsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, optionsFilename)
	original := "\uFEFF; keep me\r\n[" + optionsSection + "]\r\n" +
		"mihomo-core = old.exe\r\n" +
		"config-file = old.yaml\r\n" +
		"selector-menu-layout = auto\r\n" +
		"system-proxy-auto = 1\r\n"
	if err := os.WriteFile(ini, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	coreFile := filepath.Join(dir, "core", "mihomo-windows-amd64.exe")
	configFile := filepath.Join(dir, "cfg", "new.yml")
	if err := saveOptions(ini, coreFile, configFile, smlFlat); err != nil {
		t.Fatal(err)
	}

	text, _, ok := readTextLines(ini)
	if !ok {
		t.Fatal("ini not readable after save")
	}
	joined := strings.Join(text, "\n")
	if !strings.Contains(joined, "; keep me") {
		t.Error("comment was not preserved")
	}

	raw, err := os.ReadFile(ini)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 3 || raw[0] != 0xEF || raw[1] != 0xBB || raw[2] != 0xBF {
		t.Error("BOM was not preserved")
	}

	opts := loadOptions(ini)
	if opts.CoreFile != coreFile {
		t.Errorf("CoreFile = %q, want %q", opts.CoreFile, coreFile)
	}
	if opts.ConfigFile != configFile {
		t.Errorf("ConfigFile = %q, want %q", opts.ConfigFile, configFile)
	}
	if opts.SelectorMenuLayout != smlFlat {
		t.Errorf("SelectorMenuLayout = %v, want flat", opts.SelectorMenuLayout)
	}
	if !opts.SystemProxyAuto {
		t.Error("unrelated settings should survive saveOptions")
	}
}

func TestSaveRuntimeState(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, optionsFilename)
	original := "; keep me\r\n[" + optionsSection + "]\r\n" +
		"mihomo-core = mihomo.exe\r\n" +
		"config-file = config.yaml\r\n" +
		"tun-start-mode = off\r\n" +
		"system-proxy-auto = 0\r\n"
	if err := os.WriteFile(ini, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := saveRuntimeState(ini, true, true); err != nil {
		t.Fatal(err)
	}
	opts := loadOptions(ini)
	if opts.TunStartMode != tsmOn {
		t.Errorf("TunStartMode = %v, want on", opts.TunStartMode)
	}
	if !opts.SystemProxyAuto {
		t.Error("SystemProxyAuto should be true")
	}
	text, _, ok := readTextLines(ini)
	if !ok {
		t.Fatal("ini not readable after save")
	}
	if !strings.Contains(strings.Join(text, "\n"), "; keep me") {
		t.Error("comment was not preserved")
	}

	if err := saveRuntimeState(ini, false, false); err != nil {
		t.Fatal(err)
	}
	opts = loadOptions(ini)
	if opts.TunStartMode != tsmOff || opts.SystemProxyAuto {
		t.Errorf("state was not cleared: tun=%v proxy=%v", opts.TunStartMode, opts.SystemProxyAuto)
	}
}

func TestSaveRuntimeStateCreatesFile(t *testing.T) {
	ini := filepath.Join(t.TempDir(), optionsFilename)
	if err := saveRuntimeState(ini, true, false); err != nil {
		t.Fatal(err)
	}

	opts := loadOptions(ini)
	if opts.TunStartMode != tsmOn {
		t.Errorf("TunStartMode = %v, want on", opts.TunStartMode)
	}
	if opts.SystemProxyAuto {
		t.Error("SystemProxyAuto should be false")
	}
}

func TestParseHelpers(t *testing.T) {
	for _, value := range []string{"1", "true", "YES", " on "} {
		if !parseBool(value, false) {
			t.Errorf("parseBool(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"0", "false", "No", "off"} {
		if parseBool(value, true) {
			t.Errorf("parseBool(%q) = true, want false", value)
		}
	}
	if !parseBool("garbage", true) || parseBool("garbage", false) {
		t.Error("parseBool should return the fallback for unknown values")
	}

	if parseTunStartMode("on") != tsmOn || parseTunStartMode("off") != tsmOff || parseTunStartMode("") != tsmOff {
		t.Error("parseTunStartMode mismatch")
	}
	if parseTunStartMode("garbage") != tsmOn {
		t.Error("parseTunStartMode should treat unknown values as on")
	}

	cases := map[string]selectorMenuLayout{
		"flat":    smlFlat,
		"nested":  smlNested,
		"auto":    smlAuto,
		"unknown": smlAuto,
	}
	for value, want := range cases {
		if got := parseSelectorMenuLayout(value); got != want {
			t.Errorf("parseSelectorMenuLayout(%q) = %v, want %v", value, got, want)
		}
	}
	if layoutToValue(smlFlat) != "flat" || layoutToValue(smlNested) != "nested" || layoutToValue(smlAuto) != "auto" {
		t.Error("layoutToValue mismatch")
	}

	if got := includeTrailingPathDelimiter(`C:\core`); got != `C:\core\` {
		t.Errorf("includeTrailingPathDelimiter = %q", got)
	}
	if got := includeTrailingPathDelimiter(`C:\core\`); got != `C:\core\` {
		t.Errorf("includeTrailingPathDelimiter = %q", got)
	}
	if got := includeTrailingPathDelimiter(""); got != string(os.PathSeparator) {
		t.Errorf("includeTrailingPathDelimiter(\"\") = %q", got)
	}
}

func TestINIHelpers(t *testing.T) {
	lines := []string{
		"; comment",
		"[other]",
		"key=value",
		"[" + optionsSection + "]",
		"quoted = \" hello \"",
		"single = 'x'",
		"# inline comment line",
	}

	if v, ok := iniGet(lines, optionsSection, "quoted"); !ok || v != "hello" {
		t.Errorf("quoted value = %q, %v", v, ok)
	}
	if v, ok := iniGet(lines, optionsSection, "single"); !ok || v != "x" {
		t.Errorf("single-quoted value = %q, %v", v, ok)
	}
	if _, ok := iniGet(lines, optionsSection, "key"); ok {
		t.Error("key from another section should not be found")
	}
	if v, ok := iniGet(lines, "OTHER", "KEY"); !ok || v != "value" {
		t.Errorf("section/key matching should be case-insensitive, got %q, %v", v, ok)
	}

	updated := iniSet(lines, optionsSection, "quoted", "new")
	if v, _ := iniGet(updated, optionsSection, "quoted"); v != "new" {
		t.Errorf("iniSet update failed, got %q", v)
	}
	if !strings.Contains(strings.Join(updated, "\n"), "; comment") {
		t.Error("iniSet should keep unrelated lines")
	}

	added := iniSet(updated, optionsSection, "brand-new", "1")
	if v, ok := iniGet(added, optionsSection, "brand-new"); !ok || v != "1" {
		t.Errorf("iniSet insert failed, got %q, %v", v, ok)
	}
	if len(added) != len(updated)+1 {
		t.Errorf("iniSet should append one line, got %d lines (was %d)", len(added), len(updated))
	}

	created := iniSet(nil, optionsSection, "mihomo-core", `C:\mihomo.exe`)
	if v, ok := iniGet(created, optionsSection, "mihomo-core"); !ok || v != `C:\mihomo.exe` {
		t.Errorf("iniSet on empty file failed, got %q, %v", v, ok)
	}
}
