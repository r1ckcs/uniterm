package log

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	mu   sync.Mutex
	file *os.File
)

func Init() error {
	mu.Lock()
	defer mu.Unlock()

	if file != nil {
		return nil
	}

	dir := filepath.Dir(logPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	// The app log records hosts, users and frontend diagnostics: keep it
	// private to the user (and tighten logs created by older versions).
	f, err := os.OpenFile(logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_ = f.Chmod(0o600)
	file = f
	return nil
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		file.Close()
		file = nil
	}
}

func Writef(format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), msg)

	if file != nil {
		file.WriteString(line)
		file.Sync()
	}
}
