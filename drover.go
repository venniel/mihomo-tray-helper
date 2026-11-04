package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

const (
	stateFilename       = "mihomo-go.state.json"
	stateKeyTun         = "tun"
	stateKeySystemProxy = "system-proxy"
)

type droverEventKind int

const (
	dekError droverEventKind = iota
	dekRunning
	dekCoreEvent
	dekClashModes
)

type droverEvent struct {
	Kind      droverEventKind
	Msg       string
	CoreEvent coreEvent
}

type drover struct {
	supervisor   *coreSupervisor
	logger       *logger
	state        *appState
	options      droverOptions
	configPath   string
	mihomoConfig mihomoConfig
	selectors    []configSelector

	currentProcessDir string
	isElevated        bool
	isTunActive       bool
	wantsTun          bool
	clashMode         string
	clashModes        []string

	selectorStates       map[string]string
	selectorQueryPending bool
	selectorSerial       int

	mu                sync.Mutex
	dispatch          func(func())
	pendingOps        []func()
	onEvent           func(droverEvent)
	pendingEvents     []droverEvent
	notifyHwnd        uintptr
	shutdownRequested bool
	shutdownCompleted bool
	destroying        bool
}

func newDrover(flags appFlags) (*drover, error) {
	d := &drover{}
	d.currentProcessDir = includeTrailingPathDelimiter(filepath.Dir(currentExecutablePath()))
	d.options = loadOptions(d.currentProcessDir + optionsFilename)
	d.logger = newLogger(d.options.LogFile)

	fail := func(err error) (*drover, error) {
		d.logger.close()
		return nil, err
	}

	configPath := d.options.ConfigFile
	text, err := readConfigText(configPath)
	if err != nil {
		return fail(err)
	}
	cfg, err := readMihomoConfig(text)
	if err != nil {
		return fail(err)
	}
	if err := checkMihomoConfig(cfg); err != nil {
		return fail(err)
	}
	d.configPath = configPath
	d.mihomoConfig = cfg
	d.selectors = append([]configSelector(nil), cfg.Selectors...)
	d.clashModes = append([]string(nil), cfg.Modes...)

	d.state = newAppState(d.currentProcessDir + stateFilename)

	if wantsTun, ok := d.state.getBool(stateKeyTun); ok {
		d.wantsTun = wantsTun
	} else {
		d.wantsTun = cfg.HasTun && d.options.TunStartMode == tsmOn
		d.storeTun(d.wantsTun)
	}

	if flags.has(afTun) {
		d.wantsTun = true
	}

	d.isElevated = isProcessElevated()

	corePath := d.options.CoreFile
	if !fileExists(corePath) {
		return fail(errors.New("mihomo executable not found."))
	}

	d.supervisor = newCoreSupervisor(corePath, filepath.Dir(d.configPath),
		cfg.ClashAPI, cfg.InjectAPI, cfg.Modes, d.logger, d.dispatchCoreEvent)
	d.startCore(d.canUseTun() && d.wantsTun)

	return d, nil
}

func (d *drover) setDispatch(dispatch func(func())) {
	d.mu.Lock()
	d.dispatch = dispatch
	ops := d.pendingOps
	d.pendingOps = nil
	d.mu.Unlock()

	for _, op := range ops {
		dispatch(op)
	}
}

func (d *drover) setOnEvent(handler func(droverEvent)) {
	d.mu.Lock()
	d.onEvent = handler
	events := d.pendingEvents
	d.pendingEvents = nil
	d.mu.Unlock()

	for _, ev := range events {
		handler(ev)
	}
}

func (d *drover) postToUI(fn func()) {
	d.mu.Lock()
	if d.dispatch != nil {
		dispatch := d.dispatch
		d.mu.Unlock()
		dispatch(fn)
		return
	}
	d.pendingOps = append(d.pendingOps, fn)
	d.mu.Unlock()
}

func (d *drover) dispatchCoreEvent(ev coreEvent) {
	d.postToUI(func() { d.handleCoreEvent(ev) })
}

func (d *drover) handleCoreEvent(ev coreEvent) {
	d.mu.Lock()
	stopping := d.destroying || d.shutdownRequested
	d.mu.Unlock()
	if stopping {
		return
	}

	switch ev.Kind {
	case cekState:
		switch ev.State {
		case csRunning:
			d.notifyEvent(droverEvent{Kind: dekRunning})
		case csFailed:
			d.notifyEvent(droverEvent{Kind: dekError, Msg: ev.Msg})
		}

	case cekError:
		d.notifyEvent(droverEvent{Kind: dekError, Msg: ev.Msg})

	case cekAPIReady:
		d.refreshSelectorStates()

	case cekConfigModes, cekModeDone:
		d.clashMode = ev.Mode
		if len(ev.Modes) > 0 {
			d.clashModes = ev.Modes
		}
		d.notifyEvent(droverEvent{Kind: dekClashModes})

	case cekSelectorStates:
		d.selectorQueryPending = false
		if ev.SelectorSerial != d.selectorSerial {
			return
		}
		if d.selectorStates == nil {
			d.selectorStates = map[string]string{}
		}
		for name, now := range ev.SelectorNow {
			d.selectorStates[name] = now
		}

	case cekSelectorDone:
		d.notifyEvent(droverEvent{Kind: dekCoreEvent, CoreEvent: ev})
	}
}

func (d *drover) notifyEvent(ev droverEvent) {
	d.mu.Lock()
	if d.onEvent == nil {
		d.pendingEvents = append(d.pendingEvents, ev)
		d.mu.Unlock()
		return
	}
	handler := d.onEvent
	d.mu.Unlock()
	handler(ev)
}

func (d *drover) setNotifyHandle(hwnd uintptr) {
	d.mu.Lock()
	d.notifyHwnd = hwnd
	d.mu.Unlock()
}

func (d *drover) setSelectorMenuLayout(layout selectorMenuLayout) {
	d.options.SelectorMenuLayout = layout
}

func (d *drover) needsElevation() bool {
	return d.wantsTun && d.mihomoConfig.HasTun && !d.isElevated
}

func (d *drover) storedSystemProxy() (bool, bool) {
	return d.state.getBool(stateKeySystemProxy)
}

func (d *drover) storeSystemProxy(value bool) {
	if err := d.state.setBool(stateKeySystemProxy, value); err != nil {
		d.log(strings.TrimSpace("Failed to persist system proxy state. " + err.Error()))
	}
}

func (d *drover) storeTun(value bool) {
	d.wantsTun = value
	if err := d.state.setBool(stateKeyTun, value); err != nil {
		d.log(strings.TrimSpace("Failed to persist TUN state. " + err.Error()))
	}
}

func (d *drover) editSelector(selectorIdx, outboundIdx, requestID int) bool {
	if selectorIdx < 0 || selectorIdx >= len(d.selectors) {
		return false
	}
	selector := d.selectors[selectorIdx]
	if outboundIdx < 0 || outboundIdx >= len(selector.Outbounds) {
		return false
	}

	d.selectorSerial++
	d.selectors[selectorIdx].DefaultIndex = outboundIdx
	if d.selectorStates == nil {
		d.selectorStates = map[string]string{}
	}
	d.selectorStates[selector.Name] = selector.Outbounds[outboundIdx]

	task := selectorTask{Name: selector.Name, Value: selector.Outbounds[outboundIdx]}
	return d.supervisor.requestSetSelectors([]selectorTask{task}, requestID)
}

// refreshSelectorStates asks the core for the active outbound of every selector.
// The request runs in the background so opening the menu is never blocked.
func (d *drover) refreshSelectorStates() {
	if len(d.selectors) == 0 || d.selectorQueryPending {
		return
	}
	names := make([]string, 0, len(d.selectors))
	for _, selector := range d.selectors {
		names = append(names, selector.Name)
	}
	d.selectorQueryPending = d.supervisor.requestQuerySelectorStates(names, d.selectorSerial)
}

func (d *drover) selectorNow(name string) (string, bool) {
	value, ok := d.selectorStates[name]
	return value, ok
}

func (d *drover) setClashMode(mode string) bool {
	return d.supervisor.requestSetMode(mode)
}

func (d *drover) enableSystemProxy() bool {
	return enableSystemProxy(d.mihomoConfig.ProxyHost, d.mihomoConfig.ProxyPort, d.mihomoConfig.ProxyIsSocks)
}

func (d *drover) disableSystemProxy() bool {
	return disableSystemProxy()
}

func (d *drover) startCore(useTun bool) {
	if !d.canUseTun() {
		useTun = false
	}
	d.isTunActive = useTun

	if useTun {
		if removed := removeStaleWintunAdapters(); removed > 0 {
			d.log(fmt.Sprintf("Removed %d stale Wintun adapter(s).", removed))
		}
	}

	configText := d.mihomoConfig.TextWithoutTun
	if useTun {
		configText = d.mihomoConfig.TextWithTun
	}
	d.supervisor.requestStart(configText)
}

func (d *drover) canUseTun() bool {
	return d.mihomoConfig.HasTun && d.isElevated
}

func (d *drover) shutdown() bool {
	d.mu.Lock()
	if d.shutdownCompleted {
		d.mu.Unlock()
		return true
	}
	needRequest := !d.shutdownRequested
	d.shutdownRequested = true
	d.mu.Unlock()

	if needRequest {
		d.requestShutdownWorkers()
	}

	d.mu.Lock()
	completed := d.shutdownCompleted
	d.mu.Unlock()
	return completed
}

func (d *drover) stopForShutdown() {
	d.mu.Lock()
	if d.shutdownCompleted {
		d.mu.Unlock()
		return
	}
	d.shutdownRequested = true
	d.mu.Unlock()

	if d.supervisor != nil {
		d.supervisor.stop()
	}
	d.completeShutdown()
}

func (d *drover) requestShutdownWorkers() {
	var wg sync.WaitGroup

	if d.supervisor != nil {
		wg.Add(1)
		go func() {
			d.supervisor.stop()
			wg.Done()
		}()
	}

	go func() {
		wg.Wait()
		d.completeShutdown()
	}()
}

func (d *drover) completeShutdown() {
	d.mu.Lock()
	if d.shutdownCompleted {
		d.mu.Unlock()
		return
	}
	d.shutdownCompleted = true
	d.mu.Unlock()

	if d.logger != nil {
		d.logger.close()
	}
	d.postCanClose()
}

func (d *drover) postCanClose() {
	d.mu.Lock()
	hwnd := d.notifyHwnd
	d.mu.Unlock()

	if hwnd == 0 {
		return
	}
	if r, _, _ := pIsWindow.Call(hwnd); r == 0 {
		return
	}
	postMessage(hwnd, wmDroverCanClose, 0, 0)
}

func (d *drover) destroy() {
	d.mu.Lock()
	d.destroying = true
	alreadyRequested := d.shutdownRequested
	d.mu.Unlock()

	// Shutdown workers are already running (normal exit or the close
	// watchdog); waiting here could hang the process indefinitely.
	if alreadyRequested {
		return
	}
	d.stopForShutdown()
}

func (d *drover) log(message string) {
	d.logger.log("Drover", message)
}
