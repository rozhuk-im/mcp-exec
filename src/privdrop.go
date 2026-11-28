package mcpxec

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
)

// [security] user and group — the credentials the process drops to before it serves.
// The two settings are read like every other one (the section, then the env override);
// the resolving lives here, the applying is platform code — src/privdrop_unix.go and
// the stubs for the platforms without setuid/setgid. The drop is the last thing before
// a transport starts serving: the log file, a socket in a root-owned directory, a
// certificate and a privileged port are all things the process can only open while it
// still has its privileges.

// ResolveCredentials turns the two settings into the IDs to drop to. A value of all
// digits is that ID itself — no database entry is needed for it, which is what lets a
// container without an /etc/passwd drop to a number. Anything else is a name in the
// system database, and a name that cannot be found is an error: a setting the operator
// wrote should not fail silently. A name gives both halves of its entry, so naming a
// user and leaving the group empty takes that user's own primary group along — a drop
// that named only the user would otherwise leave the process in the group it started
// in, which is the root group when the drop starts from root. An empty setting means
// "stay what you are".
func ResolveCredentials(userSetting, groupSetting string) (int, int, error) {
	uid, gid := os.Getuid(), os.Getgid()

	var entry *user.User

	if userSetting != "" {
		id, found, err := resolveUser(userSetting)
		if err != nil {
			return 0, 0, err
		}
		uid, entry = id, found
	}

	switch {
	case groupSetting != "":
		id, err := resolveGroup(groupSetting)
		if err != nil {
			return 0, 0, err
		}
		gid = id
	case entry != nil:
		id, err := strconv.Atoi(entry.Gid)
		if err != nil {
			return 0, 0, fmt.Errorf("[security] user %q: the database entry has no numeric group (%v)", userSetting, err)
		}
		gid = id
	}

	return uid, gid, nil
}

// resolveUser reads one [security] user value and resolveGroup one [security] group
// value: all digits are the ID itself, anything else is a name, looked up in the
// system database. resolveUser also hands back the entry a name resolved to, for the
// caller that wants the group it carries.
func resolveUser(value string) (int, *user.User, error) {
	if id, err := strconv.Atoi(value); err == nil {
		return id, nil, nil
	}

	found, err := user.Lookup(value)
	if err != nil {
		return 0, nil, fmt.Errorf("[security] user %q is not a user this system knows (%v)", value, err)
	}
	id, err := strconv.Atoi(found.Uid)
	if err != nil {
		return 0, nil, fmt.Errorf("[security] user %q: the database answered no numeric id (%v)", value, err)
	}

	return id, found, nil
}

func resolveGroup(value string) (int, error) {
	if id, err := strconv.Atoi(value); err == nil {
		return id, nil
	}

	found, err := user.LookupGroup(value)
	if err != nil {
		return 0, fmt.Errorf("[security] group %q is not a group this system knows (%v)", value, err)
	}
	id, err := strconv.Atoi(found.Gid)
	if err != nil {
		return 0, fmt.Errorf("[security] group %q: the database answered no numeric id (%v)", value, err)
	}

	return id, nil
}

// DropPrivileges applies [security] user and group: resolve the two settings, then hand
// the IDs to the platform. Nothing happens when neither setting is set, and nothing
// happens when they name who the process already is — asking for the user you already
// are is not an error, that is what a configuration shared by several machines looks
// like. Any other answer is a change, and a change belongs to root: a process that is
// not root can only ask to stay who it is, so a setting it cannot honour stops the
// startup instead of being dropped quietly.
func DropPrivileges() error {
	if Cfg.RunAsUser == "" && Cfg.RunAsGroup == "" {
		return nil
	}
	if !PrivDropSupported() {
		return errors.New("user and group are supported only on Linux, the BSDs (FreeBSD, DragonFlyBSD, OpenBSD, NetBSD) and macOS")
	}

	uid, gid, err := ResolveCredentials(Cfg.RunAsUser, Cfg.RunAsGroup)
	if err != nil {
		return err
	}

	if uid == os.Getuid() && gid == os.Getgid() {
		logPrintf("Running as uid=%d gid=%d: [security] user and group ask for the same", uid, gid)
		return nil
	}
	if os.Getuid() != 0 {
		return fmt.Errorf("cannot drop to uid=%d gid=%d: only root can change the credentials, and this process runs as uid=%d", uid, gid, os.Getuid())
	}

	if err := applyCredentials(uid, gid); err != nil {
		return err
	}
	logPrintf("Privileges dropped: uid=%d gid=%d", uid, gid)

	return nil
}
