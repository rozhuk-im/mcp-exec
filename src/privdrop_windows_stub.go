//go:build windows
// +build windows

package mcpxec

import (
	"errors"
)

// On Windows there is no setuid/setgid, so [security] user and group stay a
// startup error there (Run refuses them; the README platform matrix says the same).
func applyCredentials(uid, gid int) error {
	_ = uid
	_ = gid
	return errors.New("user and group are not supported on Windows")
}
