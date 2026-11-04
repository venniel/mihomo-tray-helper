package main

import (
	"strings"
)

type appFlag int

const (
	afTun appFlag = iota
	afRestart
	afAutostartEnable
	afAutostartDisable
)

var appFlagSwitches = [...]string{"tun", "restart", "autostart-enable", "autostart-disable"}

type appFlags uint32

func parseAppFlags(args []string) appFlags {
	var flags appFlags
	for _, arg := range args {
		name := arg
		if len(name) > 0 && (name[0] == '-' || name[0] == '/') {
			name = name[1:]
		}
		for flag, switchName := range appFlagSwitches {
			if strings.EqualFold(name, switchName) {
				flags |= 1 << flag
			}
		}
	}
	return flags
}

func (f appFlags) has(flag appFlag) bool {
	return f&(1<<flag) != 0
}

func flagsToCmdLine(flags appFlags) string {
	var parts []string
	for flag, switchName := range appFlagSwitches {
		if flags.has(appFlag(flag)) {
			parts = append(parts, "-"+switchName)
		}
	}
	return strings.Join(parts, " ")
}
