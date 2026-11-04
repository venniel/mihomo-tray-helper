package main

import (
	"os"
	"strings"
	"sync"
	"time"
)

type logger struct {
	mu   sync.Mutex
	file *os.File
}

func newLogger(filePath string) *logger {
	l := &logger{}
	if filePath == "" {
		return l
	}
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		l.file = f
	}
	return l
}

func (l *logger) log(section, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}

	prefix := ""
	if section != "" {
		prefix = "[" + section + "] "
	}
	line := strings.TrimSpace(time.Now().Format("2006-01-02 15:04:05.000")+" "+prefix+message) + "\r\n"
	if _, err := l.file.WriteString(line); err != nil {
		l.closeLocked()
	}
}

func (l *logger) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closeLocked()
}

func (l *logger) closeLocked() {
	if l.file == nil {
		return
	}
	_ = l.file.Close()
	l.file = nil
}
