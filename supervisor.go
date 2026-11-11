package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	maxCaptureBytes     = 8 * 1024
	readerDrainTimeout  = 5 * time.Second
	postKillWait        = time.Second
	gracefulStopTimeout = 10 * time.Second

	// Application exit uses a shorter budget: nobody consumes the captured
	// output anymore, and a slow stop only delays the process termination.
	exitStopTimeout  = 3 * time.Second
	exitDrainTimeout = time.Second

	apiCheckDelay         = 500 * time.Millisecond
	apiCheckInterval      = 500 * time.Millisecond
	apiCheckTimeout       = 60 * time.Second
	connectionsFlushLimit = 2 * time.Second
	selectorQueryTimeout  = time.Second
)

type coreState int32

const (
	csStopped coreState = iota
	csStarting
	csRunning
	csStopping
	csFailed
)

var coreStateNames = [...]string{"Stopped", "Starting", "Running", "Stopping", "Failed"}

type coreEventKind int

const (
	cekState coreEventKind = iota
	cekError
	cekAPIReady
	cekConfigModes
	cekSelectorDone
	cekModeDone
	cekSelectorStates
)

type coreEvent struct {
	Kind           coreEventKind
	State          coreState
	Msg            string
	RequestID      int
	Mode           string
	Modes          []string
	SelectorNow    map[string]string
	SelectorSerial int
}

type selectorTask struct {
	Name  string
	Value string
}

type coreCommandKind int

const (
	cmdNone coreCommandKind = iota
	cmdStart
	cmdSetSelectors
	cmdSetMode
	cmdQuerySelectorStates
)

var coreCommandNames = [...]string{"None", "Start", "SetSelectors", "SetMode", "QuerySelectorStates"}

type coreCommand struct {
	Kind           coreCommandKind
	ConfigText     string
	SelectorTasks  []selectorTask
	RequestID      int
	Mode           string
	SelectorNames  []string
	SelectorSerial int
}

type coreSupervisor struct {
	exePath    string
	homeDir    string
	externalUI string
	logger     *logger
	api        *coreAPIClient
	modes      []string
	injectAPI  bool
	onEvent    func(coreEvent)

	commands chan coreCommand
	stopCh   chan struct{}
	done     chan struct{}
	stopOnce sync.Once

	state int32
	// Set when the application is exiting, so the core gets a shorter
	// graceful-stop budget than an in-app restart.
	exiting int32

	job        windows.Handle
	cmd        *exec.Cmd
	processID  uint32
	reader     *outputReader
	waitCh     chan struct{}
	lastOut    string
	tempConfig string

	apiCheckActive bool
	apiNextCheck   time.Time
	apiDeadline    time.Time
}

func newCoreSupervisor(exePath, homeDir, externalUI string, cfg clashAPIConfig, injectAPI bool, modes []string,
	l *logger, onEvent func(coreEvent)) *coreSupervisor {
	s := &coreSupervisor{
		exePath:    exePath,
		homeDir:    homeDir,
		externalUI: externalUI,
		logger:     l,
		api:        newCoreAPIClient(cfg, l),
		modes:      slices.Clone(modes),
		injectAPI:  injectAPI,
		onEvent:    onEvent,
		commands:   make(chan coreCommand, 100),
		stopCh:     make(chan struct{}),
		done:       make(chan struct{}),
	}
	atomic.StoreInt32(&s.state, int32(csStopped))
	go s.run()
	return s
}

func (s *coreSupervisor) currentState() coreState {
	return coreState(atomic.LoadInt32(&s.state))
}

func (s *coreSupervisor) run() {
	defer close(s.done)
	s.log("Supervisor started.")

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		// Check the stop signal first so a queued command cannot delay an
		// application exit.
		if s.stopping() {
			s.log("Supervisor stopping...")
			s.stopGraceful()
			s.log("Supervisor stopped.")
			return
		}

		s.checkProcessStatus()
		s.checkAPIStatus()

		select {
		case <-s.stopCh:
			// Handled at the top of the next iteration.
		case cmd := <-s.commands:
			s.handleCommand(cmd)
		case <-ticker.C:
		}
	}
}

func (s *coreSupervisor) stopping() bool {
	select {
	case <-s.stopCh:
		return true
	default:
		return false
	}
}

func (s *coreSupervisor) requestStart(configText string) {
	s.post(coreCommand{Kind: cmdStart, ConfigText: configText})
}

func (s *coreSupervisor) requestSetSelectors(tasks []selectorTask, requestID int) bool {
	return s.post(coreCommand{Kind: cmdSetSelectors, SelectorTasks: tasks, RequestID: requestID})
}

func (s *coreSupervisor) requestSetMode(mode string) bool {
	return s.post(coreCommand{Kind: cmdSetMode, Mode: mode})
}

func (s *coreSupervisor) requestQuerySelectorStates(names []string, serial int) bool {
	return s.post(coreCommand{Kind: cmdQuerySelectorStates, SelectorNames: names, SelectorSerial: serial})
}

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

func (s *coreSupervisor) post(cmd coreCommand) bool {
	select {
	case s.commands <- cmd:
		return true
	case <-s.stopCh:
		return false
	}
}

func (s *coreSupervisor) stop() {
	atomic.StoreInt32(&s.exiting, 1)
	s.stopOnce.Do(func() { close(s.stopCh) })
	<-s.done
}

func (s *coreSupervisor) stopTimeouts() (graceful, drain time.Duration) {
	if atomic.LoadInt32(&s.exiting) != 0 {
		return exitStopTimeout, exitDrainTimeout
	}
	return gracefulStopTimeout, readerDrainTimeout
}

func (s *coreSupervisor) handleCommand(cmd coreCommand) {
	s.log(fmt.Sprintf("Command: %s.", coreCommandNames[cmd.Kind]))

	switch cmd.Kind {
	case cmdStart:
		s.doStart(cmd.ConfigText)
	case cmdSetSelectors:
		s.doSetSelectors(cmd.SelectorTasks, cmd.RequestID)
	case cmdSetMode:
		s.doSetMode(cmd.Mode)
	case cmdQuerySelectorStates:
		s.doQuerySelectorStates(cmd.SelectorNames, cmd.SelectorSerial)
	}
}

func (s *coreSupervisor) doStart(configText string) {
	s.stopGraceful()
	s.lastOut = ""
	s.setStateAndNotify(csStarting, "")

	if err := s.startProcess(configText); err != nil {
		s.cleanupProcess()
		s.setStateAndNotify(csFailed, s.buildFailureMessage(err.Error(), s.lastOut))
		return
	}

	s.log(fmt.Sprintf("Process created, PID: %d.", s.processID))
	s.setStateAndNotify(csRunning, "")
	s.initAPICheck()
}

func (s *coreSupervisor) startProcess(configText string) error {
	if !fileExists(s.exePath) {
		return errors.New("mihomo executable not found.")
	}

	tempPath, err := writeTempConfig(configText)
	if err != nil {
		return err
	}
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("CreateJobObject failed: %v", err)
	}
	closeJob := true
	defer func() {
		if closeJob {
			_ = windows.CloseHandle(job)
		}
	}()

	var jobInfo windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	jobInfo.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&jobInfo)), uint32(unsafe.Sizeof(jobInfo))); err != nil {
		return fmt.Errorf("SetInformationJobObject failed: %v", err)
	}

	outR, outW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("CreatePipe (output) failed: %v", err)
	}

	args := []string{"-f", tempPath, "-d", s.homeDir}
	if s.externalUI != "" {
		args = append(args, "-ext-ui", s.externalUI)
	}
	if s.injectAPI {
		args = append(args, "-ext-ctl", s.api.cfg.ExternalController)
		if s.api.cfg.Secret != "" {
			args = append(args, "-secret", s.api.cfg.Secret)
		}
	}

	cmd := exec.Command(s.exePath, args...)
	cmd.Dir = s.homeDir
	cmd.Stdout = outW
	cmd.Stderr = outW
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_CONSOLE | windows.CREATE_NEW_PROCESS_GROUP,
	}

	if err := cmd.Start(); err != nil {
		_ = outR.Close()
		_ = outW.Close()
		return fmt.Errorf("CreateProcess failed: %v", err)
	}
	_ = outW.Close()

	processHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = outR.Close()
		return fmt.Errorf("OpenProcess failed: %v", err)
	}
	assignErr := windows.AssignProcessToJobObject(job, processHandle)
	_ = windows.CloseHandle(processHandle)
	if assignErr != nil {
		_ = cmd.Process.Kill()
		_ = outR.Close()
		return fmt.Errorf("AssignProcessToJobObject failed: %v", assignErr)
	}

	closeJob = false
	removeTemp = false
	s.job = job
	s.cmd = cmd
	s.processID = uint32(cmd.Process.Pid)
	s.tempConfig = tempPath
	s.waitCh = make(chan struct{}, 1)
	go func(c *exec.Cmd, ch chan struct{}) {
		_ = c.Wait()
		ch <- struct{}{}
	}(cmd, s.waitCh)
	s.reader = startOutputReader(outR)

	return nil
}

func writeTempConfig(configText string) (string, error) {
	file, err := os.CreateTemp("", "MTH-*.yaml")
	if err != nil {
		return "", fmt.Errorf("Failed to create a temporary configuration file: %v", err)
	}
	path := file.Name()
	if _, err := file.WriteString(configText); err != nil {
		file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("Failed to write the temporary configuration file: %v", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("Failed to write the temporary configuration file: %v", err)
	}
	return path, nil
}

func (s *coreSupervisor) sendGracefulSignal(processID uint32) bool {
	pFreeConsole.Call()
	if r, _, err := pAttachConsole.Call(uintptr(processID)); r == 0 {
		s.log(strings.TrimSpace("AttachConsole failed. " + err.Error()))
		return false
	}
	defer pFreeConsole.Call()

	pSetConsoleCtrlHandler.Call(0, 1)
	err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, processID)
	time.Sleep(10 * time.Millisecond)
	pSetConsoleCtrlHandler.Call(0, 0)
	if err != nil {
		s.log(strings.TrimSpace("GenerateConsoleCtrlEvent failed. " + err.Error()))
		return false
	}
	return true
}

func (s *coreSupervisor) stopGraceful() {
	if s.cmd == nil {
		s.cleanupProcess()
		return
	}
	gracefulTimeout, _ := s.stopTimeouts()
	started := time.Now()
	s.setStateAndNotify(csStopping, "")

	kill := true
	sent := s.sendGracefulSignal(s.processID)
	if sent {
		s.log("Graceful shutdown signal sent.")
		select {
		case <-s.waitCh:
			kill = false
		case <-time.After(gracefulTimeout):
		}
	}

	elapsed := time.Since(started).Round(time.Millisecond)
	if kill {
		if sent {
			s.log(fmt.Sprintf("Graceful shutdown timed out after %s, forcing termination.", elapsed))
		} else {
			s.log("Graceful shutdown signal could not be delivered, forcing termination.")
		}
		_ = s.cmd.Process.Kill()
		select {
		case <-s.waitCh:
		case <-time.After(postKillWait):
			s.log("Process did not report exit after termination.")
		}
	} else {
		s.log(fmt.Sprintf("Graceful shutdown complete in %s.", elapsed))
	}

	s.cleanupProcess()
	s.setStateAndNotify(csStopped, "")
}

func (s *coreSupervisor) checkProcessStatus() {
	if s.cmd == nil {
		return
	}

	select {
	case <-s.waitCh:
		s.cleanupProcess()
		s.setStateAndNotify(csFailed, s.buildFailureMessage("mihomo process exited unexpectedly.", s.lastOut))
	default:
	}
}

func (s *coreSupervisor) cleanupProcess() {
	if s.job != 0 {
		_ = windows.CloseHandle(s.job)
		s.job = 0
	}

	s.processID = 0
	s.cmd = nil

	if s.reader != nil {
		_, drainTimeout := s.stopTimeouts()
		output, drained := s.reader.drain(drainTimeout)
		s.lastOut = output
		if !drained {
			s.log("Output reader still blocked; leaking it to keep supervisor responsive.")
		}
		s.reader = nil
	}

	if s.tempConfig != "" {
		_ = os.Remove(s.tempConfig)
		s.tempConfig = ""
	}
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

func (s *coreSupervisor) setStateAndNotify(state coreState, msg string) {
	s.log(strings.TrimSpace(fmt.Sprintf("State: %s. %s", coreStateNames[state], msg)))
	atomic.StoreInt32(&s.state, int32(state))
	s.notifyEvent(coreEvent{Kind: cekState, State: state, Msg: msg})
}

func (s *coreSupervisor) notifyEvent(event coreEvent) {
	if s.onEvent != nil {
		s.onEvent(event)
	}
}

func (s *coreSupervisor) buildFailureMessage(base, captured string) string {
	if captured == "" {
		return base
	}
	if critical := extractCriticalError(captured); critical != "" {
		return base + "\r\n" + critical
	}
	return base
}

func (s *coreSupervisor) log(message string) {
	s.logger.log("Core", message)
}

// --- Output capture ------------------------------------------------------

type captureBuffer struct {
	mu      sync.Mutex
	data    []byte
	wrapped bool
}

func (b *captureBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(p) >= maxCaptureBytes {
		b.data = append(b.data[:0], p[len(p)-maxCaptureBytes:]...)
		b.wrapped = true
		return len(p), nil
	}

	if len(b.data)+len(p) <= maxCaptureBytes {
		b.data = append(b.data, p...)
	} else {
		keep := maxCaptureBytes - len(p)
		b.data = append(b.data[:0], b.data[len(b.data)-keep:]...)
		b.data = append(b.data, p...)
		b.wrapped = true
	}
	return len(p), nil
}

func (b *captureBuffer) text() string {
	b.mu.Lock()
	snapshot := slices.Clone(b.data)
	wrapped := b.wrapped
	b.mu.Unlock()

	if len(snapshot) == 0 {
		return ""
	}

	endIdx := bytes.LastIndexByte(snapshot, '\n')
	if endIdx < 0 {
		return ""
	}

	startIdx := 0
	if wrapped {
		first := bytes.IndexByte(snapshot[:endIdx+1], '\n')
		if first < 0 {
			return ""
		}
		startIdx = first + 1
		if startIdx > endIdx {
			return ""
		}
	}

	return strings.TrimSpace(string(snapshot[startIdx : endIdx+1]))
}

type outputReader struct {
	file *os.File
	cap  *captureBuffer
	done chan struct{}
}

func startOutputReader(file *os.File) *outputReader {
	r := &outputReader{file: file, cap: &captureBuffer{}, done: make(chan struct{})}
	go func() {
		_, _ = io.Copy(r.cap, file)
		_ = file.Close()
		close(r.done)
	}()
	return r
}

func (r *outputReader) drain(timeout time.Duration) (string, bool) {
	select {
	case <-r.done:
		return r.cap.text(), true
	case <-time.After(timeout):
	}

	_ = windows.CancelIoEx(windows.Handle(r.file.Fd()), nil)
	select {
	case <-r.done:
		return r.cap.text(), true
	case <-time.After(postKillWait):
		return r.cap.text(), false
	}
}

// --- Critical error extraction ------------------------------------------

func isFatalLine(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "level=fatal") ||
		strings.Contains(lower, "level=panic") ||
		strings.HasPrefix(line, "panic:") ||
		strings.HasPrefix(line, "fatal error:") ||
		strings.HasPrefix(line, "FATA[") ||
		strings.HasPrefix(line, "PANIC")
}

func isErrorLine(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "level=error") || strings.HasPrefix(line, "ERRO[")
}

func extractCriticalError(captured string) string {
	const maxResultChars = 2000

	if captured == "" {
		return ""
	}

	lines := strings.Split(captured, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}

	lastIdx := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if isFatalLine(lines[i]) {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 {
		for i := len(lines) - 1; i >= 0; i-- {
			if isErrorLine(lines[i]) {
				lastIdx = i
				break
			}
		}
	}
	if lastIdx < 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(lines[lastIdx])
	for i := lastIdx + 1; i < len(lines); i++ {
		builder.WriteString("\r\n")
		builder.WriteString(lines[i])
		if builder.Len() > maxResultChars {
			break
		}
	}
	return builder.String()
}
