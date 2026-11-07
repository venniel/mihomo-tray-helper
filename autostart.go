package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const autostartTaskName = "Nexil"

const autostartTaskXML = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Author>%s</Author>
    <URI>\Nexil</URI>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%s</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>false</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
    </Exec>
  </Actions>
</Task>`

type autostartAction int

const (
	aaCheck autostartAction = iota
	aaEnable
	aaDisable
)

type autostartState int

const (
	asUnknown autostartState = iota
	asDisabled
	asEnabled
)

type autostartResult struct {
	Action   autostartAction
	Success  bool
	State    autostartState
	ErrorMsg string
}

type taskXML struct {
	Actions struct {
		Exec struct {
			Command string `xml:"Command"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

func runAutostart(action autostartAction, done func(autostartResult)) {
	go func() {
		res := autostartResult{Action: action, Success: true, State: asUnknown}

		switch action {
		case aaEnable:
			if err := enableAutostart(); err != nil {
				res.Success = false
				res.ErrorMsg = err.Error()
			}
		case aaDisable:
			if err := disableAutostart(); err != nil {
				res.Success = false
				res.ErrorMsg = err.Error()
			}
		}

		state, err := probeAutostartState()
		if err != nil {
			if action == aaCheck {
				res.Success = false
				res.ErrorMsg = err.Error()
			}
		} else {
			res.State = state
		}

		done(res)
	}()
}

func enableAutostart() error {
	userName, err := currentUserSAM()
	if err != nil || userName == "" {
		return fmt.Errorf("Failed to resolve current user name.")
	}

	escapedUser := xmlEscape(userName)
	escapedExe := xmlEscape(currentExecutablePath())
	xmlText := fmt.Sprintf(autostartTaskXML, escapedUser, escapedUser, escapedUser, escapedExe)

	file, err := os.CreateTemp("", "Nexil-task-*.xml")
	if err != nil {
		return fmt.Errorf("Failed to register autostart task.")
	}
	path := file.Name()
	defer os.Remove(path)

	data := append([]byte{0xFF, 0xFE}, encodeUTF16LE(xmlText)...)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("Failed to register autostart task.")
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("Failed to register autostart task.")
	}

	if !runSchTasks("/Create", "/TN", autostartTaskName, "/XML", path, "/F") {
		return fmt.Errorf("Failed to register autostart task.")
	}
	return nil
}

func disableAutostart() error {
	if runSchTasks("/Delete", "/TN", autostartTaskName, "/F") {
		return nil
	}

	if state, err := probeAutostartState(); err == nil && state == asDisabled {
		return nil
	}
	return fmt.Errorf("Failed to remove autostart task.")
}

func probeAutostartState() (autostartState, error) {
	output, ok := queryAutostartTask()
	if !ok {
		return asDisabled, nil
	}

	text := decodeSchTasksOutput(output)
	var task taskXML
	decoder := xml.NewDecoder(strings.NewReader(text))
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	if err := decoder.Decode(&task); err != nil {
		return asUnknown, fmt.Errorf("Failed to check autostart task.")
	}

	command := strings.TrimSpace(task.Actions.Exec.Command)
	if strings.EqualFold(command, strings.TrimSpace(currentExecutablePath())) {
		return asEnabled, nil
	}
	return asDisabled, nil
}

func queryAutostartTask() ([]byte, bool) {
	cmd := exec.Command("schtasks.exe", "/Query", "/TN", autostartTaskName, "/XML")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	output, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	return output, true
}

func runSchTasks(args ...string) bool {
	cmd := exec.Command("schtasks.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func currentUserSAM() (string, error) {
	buffer := make([]uint16, 256)
	size := uint32(len(buffer))
	if err := windows.GetUserNameEx(windows.NameSamCompatible, &buffer[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer), nil
}

func decodeSchTasksOutput(data []byte) string {
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return utf16BytesToString(data[2:], false)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return utf16BytesToString(data[2:], true)
	case utf8Valid(data):
		return string(data)
	default:
		return ansiToUTF8(data)
	}
}

func encodeUTF16LE(text string) []byte {
	u := make([]uint16, 0, len(text))
	for _, r := range text {
		if r > 0xFFFF {
			r -= 0x10000
			u = append(u, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			u = append(u, uint16(r))
		}
	}
	out := make([]byte, 0, len(u)*2)
	for _, v := range u {
		out = append(out, byte(v), byte(v>>8))
	}
	return out
}

func xmlEscape(text string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(text))
	return buf.String()
}
