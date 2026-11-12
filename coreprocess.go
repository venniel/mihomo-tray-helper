package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

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
