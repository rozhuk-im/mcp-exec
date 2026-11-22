//go:build windows
// +build windows

package mcpxec

import (
	"errors"
	"io"
)

// SyslogLogInfo mirrors log/syslog's LOG_INFO for the Windows stub.
const SyslogLogInfo int = 6

// On Windows log/syslog cannot connect, so log_mode=syslog fails at startup.
func dialSyslog(network, address string, priority int, tag string) (io.WriteCloser, error) {
	return nil, errors.New("syslog is not supported on Windows")
}

func newSyslog(priority int, tag string) (io.WriteCloser, error) {
	return nil, errors.New("syslog is not supported on Windows")
}
