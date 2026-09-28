package session

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ys-ll/uniterm/backend/log"
)

// safeRemoteName reports whether a directory-entry name received from a
// remote server (SFTP READDIR, SCP directive, FTP LIST, SMB, WebDAV, S3 key)
// can be used as a single local path component. A hostile server can
// otherwise send "../../.ssh/authorized_keys" (or "..\\..\\x.bat" on Windows)
// and escape the user's chosen download directory.
func safeRemoteName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return false
	}
	return filepath.IsLocal(name)
}

// safeLocalChild joins a server-supplied entry name under dir, refusing any
// name that is not a single, local path component.
func safeLocalChild(dir, name string) (string, error) {
	if !safeRemoteName(name) {
		log.Writef("[transfer] refusing unsafe remote entry name %q", name)
		return "", fmt.Errorf("unsafe remote file name %q", name)
	}
	return filepath.Join(dir, name), nil
}

// safeLocalRelPath validates a server-supplied relative path made of
// "/"-separated components (S3 keys below a prefix) and joins it under dir.
func safeLocalRelPath(dir, rel string) (string, error) {
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if !safeRemoteName(p) {
			log.Writef("[transfer] refusing unsafe remote path %q", rel)
			return "", fmt.Errorf("unsafe remote file name %q", rel)
		}
	}
	return filepath.Join(append([]string{dir}, parts...)...), nil
}
