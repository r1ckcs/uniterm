//go:build !android

package log

import (
	"os"
	"path/filepath"

	"github.com/ys-ll/uniterm/backend/appname"
)

// logPath returns the desktop log location (~/.flexterm/flexterm.log).
func logPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "flexterm.log"
	}
	return filepath.Join(home, appname.HomeDir, "flexterm.log")
}
