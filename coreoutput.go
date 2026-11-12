package main

import (
	"bytes"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

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
