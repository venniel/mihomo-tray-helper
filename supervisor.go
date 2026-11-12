package main

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
