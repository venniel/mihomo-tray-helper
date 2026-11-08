package main

import (
	"sync/atomic"
	"testing"
)

func TestTrayTip(t *testing.T) {
	d := &drover{supervisor: &coreSupervisor{}}
	u := &mainUI{d: d}

	if got := u.trayTip(); got != appTitle {
		t.Errorf("idle tooltip = %q, want %q", got, appTitle)
	}

	u.isSystemProxyActive = true
	if got, want := u.trayTip(), appTitle+":系统代理"; got != want {
		t.Errorf("system proxy tooltip = %q, want %q", got, want)
	}

	u.isTunActive = true
	if got, want := u.trayTip(), appTitle+":系统代理"; got != want {
		t.Errorf("tun while stopped = %q, want %q", got, want)
	}

	atomic.StoreInt32(&d.supervisor.state, int32(csRunning))
	if got, want := u.trayTip(), appTitle+":TUN 模式 + 系统代理"; got != want {
		t.Errorf("tun and system proxy tooltip = %q, want %q", got, want)
	}

	u.isSystemProxyActive = false
	if got, want := u.trayTip(), appTitle+":TUN 模式"; got != want {
		t.Errorf("tun tooltip = %q, want %q", got, want)
	}
}
