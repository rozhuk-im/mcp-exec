//go:build !linux && !darwin && !windows && !unix
// +build !linux,!darwin,!windows,!unix

package mcpxec

import (
	"errors"
	"io"
)

// SyslogLogInfo mirrors log/syslog's LOG_INFO for platforms without syslog support.
const SyslogLogInfo int = 6

func dialSyslog(network, address string, priority int, tag string) (io.WriteCloser, error) {
	return nil, errors.New("syslog is not supported on this platform")
}

func newSyslog(priority int, tag string) (io.WriteCloser, error) {
	return nil, errors.New("syslog is not supported on this platform")
}
