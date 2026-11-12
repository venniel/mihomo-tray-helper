package main

import (
	"sync/atomic"
	"testing"
)

func TestTunActive(t *testing.T) {
	d := &drover{supervisor: &coreSupervisor{}}
	u := &mainUI{d: d}

	if u.tunActive() {
		t.Error("TUN should be inactive before it is selected")
	}

	u.isTunActive = true
	if u.tunActive() {
		t.Error("TUN should be inactive while the core is stopped")
	}

	atomic.StoreInt32(&d.supervisor.state, int32(csRunning))
	if !u.tunActive() {
		t.Error("TUN should be active with a running core")
	}

	u.d = nil
	if u.tunActive() {
		t.Error("TUN should be inactive without a drover")
	}
}

func TestTrayTip(t *testing.T) {
	d := &drover{supervisor: &coreSupervisor{}}
	u := &mainUI{d: d}

	if got := u.trayTip(); got != appTitle {
		t.Errorf("idle tooltip = %q, want %q", got, appTitle)
	}

	u.isSystemProxyActive = true
	if got, want := u.trayTip(), "mihomo:系统代理"; got != want {
		t.Errorf("system proxy tooltip = %q, want %q", got, want)
	}

	u.isTunActive = true
	if got, want := u.trayTip(), "mihomo:系统代理"; got != want {
		t.Errorf("tun while stopped = %q, want %q", got, want)
	}

	atomic.StoreInt32(&d.supervisor.state, int32(csRunning))
	if got, want := u.trayTip(), "mihomo:TUN 模式 + 系统代理"; got != want {
		t.Errorf("tun and system proxy tooltip = %q, want %q", got, want)
	}

	u.isSystemProxyActive = false
	if got, want := u.trayTip(), "mihomo:TUN 模式"; got != want {
		t.Errorf("tun tooltip = %q, want %q", got, want)
	}
}
