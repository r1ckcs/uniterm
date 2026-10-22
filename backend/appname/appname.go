// Package appname holds the product name and the on-disk names derived from
// it, plus the one-time copy of the folders used before the FlexTerm rename.
//
// FlexTerm is a fork of uniTerm (ys-ll/uniterm). The Go module path, the .utm
// export format, UNITERM_* variables and wire/crypto domain strings keep the
// old name on purpose; only what the user sees and the folders change.
package appname

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	// Name is the product name shown to the user.
	Name = "FlexTerm"
	// DirName is the folder under the OS config/cache/documents dirs.
	DirName = "FlexTerm"
	// HomeDir is the dot-folder in the user's home (log, known_hosts).
	HomeDir = ".flexterm"

	LegacyDirName = "uniTerm"
	LegacyHomeDir = ".uniterm"
)

// MigrateLegacyDirs copies the pre-rename folders to the new names on the
// first FlexTerm start. Copy, not move: an older build keeps working and a
// downgrade loses nothing. Errors are returned for logging only; the app
// starts either way (with an empty folder at worst).
func MigrateLegacyDirs() []error {
	var errs []error
	if cfg, err := os.UserConfigDir(); err == nil {
		if _, err := CopyLegacyDir(filepath.Join(cfg, LegacyDirName), filepath.Join(cfg, DirName)); err != nil {
			errs = append(errs, err)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := CopyLegacyDir(filepath.Join(home, LegacyHomeDir), filepath.Join(home, HomeDir)); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// CopyLegacyDir copies oldDir to newDir when newDir does not exist and oldDir
// does. It reports whether a copy happened. The tree is staged in a sibling
// temp dir and renamed into place, so a crash mid-copy never leaves a
// half-filled newDir that would block a retry. File modes are kept (secrets
// are 0600); symlinks are recreated as links, other special files skipped.
func CopyLegacyDir(oldDir, newDir string) (bool, error) {
	if _, err := os.Lstat(newDir); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	info, err := os.Stat(oldDir)
	if err != nil || !info.IsDir() {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0o700); err != nil {
		return false, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(newDir), "."+filepath.Base(newDir)+".migrating-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(stage) // no-op after a successful rename
	if err := copyTree(oldDir, stage); err != nil {
		return false, fmt.Errorf("copy %s: %w", oldDir, err)
	}
	if err := os.Chmod(stage, info.Mode().Perm()); err != nil {
		return false, err
	}
	if err := os.Rename(stage, newDir); err != nil {
		// Another instance won the race: its copy is as good as ours.
		if _, statErr := os.Stat(newDir); statErr == nil {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.IsDir():
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.Mkdir(target, info.Mode().Perm()|0o700)
		case d.Type().IsRegular():
			return copyFile(path, target)
		default:
			return nil // sockets, pipes: nothing to carry over
		}
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// OpenFile's mode is masked by umask; set it exactly.
	return os.Chmod(dst, info.Mode().Perm())
}
