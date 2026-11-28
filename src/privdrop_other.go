//go:build !linux && !darwin && !windows && !unix
// +build !linux,!darwin,!windows,!unix

package mcpxec

import (
	"errors"
)

// On these platforms there is no setuid/setgid to apply, so [security] user and
// group stay a startup error (Run refuses them — see the README platform matrix).
func applyCredentials(uid, gid int) error {
	_ = uid
	_ = gid
	return errors.New("user and group are not supported on this platform")
}
