//go:build unix
// +build unix

package mcpxec

import (
	"io"
	"log/syslog"
)

// SyslogLogInfo mirrors log/syslog's LOG_INFO on platforms where syslog is available —
// Linux, the BSDs (FreeBSD, DragonFlyBSD, OpenBSD, NetBSD) and macOS. The device is
// handled by log/syslog: /dev/log on Linux and the BSDs, /var/run/log on macOS.
const SyslogLogInfo int = int(syslog.LOG_INFO)

func dialSyslog(network, address string, priority int, tag string) (io.WriteCloser, error) {
	w, err := syslog.Dial(network, address, syslog.Priority(priority), tag)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func newSyslog(priority int, tag string) (io.WriteCloser, error) {
	w, err := syslog.New(syslog.Priority(priority), tag)
	if err != nil {
		return nil, err
	}
	return w, nil
}
