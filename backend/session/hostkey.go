package session

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/ys-ll/uniterm/backend/log"
)

// HostKeyPrompt describes an SSH server key the user has not trusted yet.
type HostKeyPrompt struct {
	// Host is the "host:port" address the client dialed (per hop).
	Host        string `json:"host"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"` // SHA256:... (OpenSSH format)
	// OtherKeyTypesKnown is set when the host is already trusted with a key
	// of a different algorithm (OpenSSH prompts in that case too, with a
	// warning).
	OtherKeyTypesKnown bool `json:"otherKeyTypesKnown"`
}

// HostKeyPrompter asks the user whether to trust an unknown host key. It
// returns true only on an explicit accept; errors (timeout, cancel) reject.
type HostKeyPrompter func(p HostKeyPrompt) (bool, error)

var (
	hostKeyCfgMu     sync.RWMutex
	knownHostsFile   string
	globalHKPrompter HostKeyPrompter

	// hostKeyPromptMu serializes unknown-key prompts so parallel dials to the
	// same host (terminal + SFTP + monitor) ask once: waiters re-check the
	// store after acquiring it.
	hostKeyPromptMu sync.Mutex
)

// SetKnownHostsPath sets the app-owned known_hosts file (accepted keys are
// appended here). Called once the data directory is known.
func SetKnownHostsPath(p string) {
	hostKeyCfgMu.Lock()
	knownHostsFile = p
	hostKeyCfgMu.Unlock()
}

// SetHostKeyPrompter installs the app-wide prompt (a frontend dialog) used by
// dial paths that have no terminal of their own to ask in.
func SetHostKeyPrompter(p HostKeyPrompter) {
	hostKeyCfgMu.Lock()
	globalHKPrompter = p
	hostKeyCfgMu.Unlock()
}

func appKnownHostsPath() string {
	hostKeyCfgMu.RLock()
	p := knownHostsFile
	hostKeyCfgMu.RUnlock()
	if p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".uniterm", "known_hosts")
}

func userKnownHostsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

// hostKeyCallback returns the verifying callback for every SSH dial. prompt
// is the per-session prompter (the terminal asks inline, like OpenSSH); nil
// falls back to the app-wide dialog prompter, and with neither an unknown
// key is rejected with its fingerprint in the error.
func hostKeyCallback(prompt HostKeyPrompter) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return verifyHostKey(appKnownHostsPath(), userKnownHostsPath(), hostname, key, prompt, true)
	}
}

// hostKeyCallbackNoPrompt never asks: only already-trusted keys pass.
func hostKeyCallbackNoPrompt() ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return verifyHostKey(appKnownHostsPath(), userKnownHostsPath(), hostname, key, nil, false)
	}
}

type hostKeyStatus int

const (
	hostKeyKnown hostKeyStatus = iota
	hostKeyUnknown
	hostKeyUnknownOtherType
	hostKeyChanged
)

// lookupHostKey checks key for hostname against the given known_hosts files
// (missing files are skipped; an unparsable user file is ignored with a log
// line rather than breaking every connection).
func lookupHostKey(files []string, hostname string, key ssh.PublicKey) (hostKeyStatus, error) {
	var existing []string
	for _, f := range files {
		if f == "" {
			continue
		}
		if _, err := os.Stat(f); err == nil {
			existing = append(existing, f)
		}
	}
	var cb ssh.HostKeyCallback
	for len(existing) > 0 {
		c, err := knownhosts.New(existing...)
		if err == nil {
			cb = c
			break
		}
		log.Writef("[hostkey] ignoring %s: %v", existing[len(existing)-1], err)
		existing = existing[:len(existing)-1]
	}
	if cb == nil {
		return hostKeyUnknown, nil
	}
	// knownhosts only needs a parsable remote address; the dialed hostname
	// takes precedence, so the (possibly proxied) peer address is irrelevant.
	err := cb(hostname, &net.TCPAddr{IP: net.IPv4zero}, key)
	if err == nil {
		return hostKeyKnown, nil
	}
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) {
		// RevokedError or a malformed address: never trust.
		return hostKeyChanged, err
	}
	if len(ke.Want) == 0 {
		return hostKeyUnknown, nil
	}
	for _, w := range ke.Want {
		if w.Key.Type() == key.Type() {
			return hostKeyChanged, nil
		}
	}
	return hostKeyUnknownOtherType, nil
}

func verifyHostKey(appFile, userFile, hostname string, key ssh.PublicKey, prompt HostKeyPrompter, allowPrompt bool) error {
	fp := ssh.FingerprintSHA256(key)
	files := []string{appFile, userFile}

	status, err := lookupHostKey(files, hostname, key)
	switch status {
	case hostKeyKnown:
		return nil
	case hostKeyChanged:
		if err != nil {
			return fmt.Errorf("host key verification failed for %s: %w", hostname, err)
		}
		return fmt.Errorf("WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED for %s: server presented %s key %s, which does not match the trusted key. This may be a man-in-the-middle attack; if the change is legitimate, remove the old entry from %s", hostname, key.Type(), fp, appFile)
	}

	if prompt == nil && allowPrompt {
		hostKeyCfgMu.RLock()
		prompt = globalHKPrompter
		hostKeyCfgMu.RUnlock()
	}
	unknownErr := fmt.Errorf("host key for %s is not trusted (%s %s); connect from a terminal session to verify and accept it", hostname, key.Type(), fp)
	if prompt == nil || !allowPrompt {
		return unknownErr
	}

	hostKeyPromptMu.Lock()
	defer hostKeyPromptMu.Unlock()
	// Another dial may have accepted (or the user edited the file) while we
	// waited for the prompt lock.
	status, err = lookupHostKey(files, hostname, key)
	switch status {
	case hostKeyKnown:
		return nil
	case hostKeyChanged:
		if err != nil {
			return fmt.Errorf("host key verification failed for %s: %w", hostname, err)
		}
		return fmt.Errorf("host key for %s changed while verifying", hostname)
	}

	ok, perr := prompt(HostKeyPrompt{
		Host:               hostname,
		KeyType:            key.Type(),
		Fingerprint:        fp,
		OtherKeyTypesKnown: status == hostKeyUnknownOtherType,
	})
	if perr != nil {
		return fmt.Errorf("host key for %s not accepted: %w", hostname, perr)
	}
	if !ok {
		return fmt.Errorf("host key for %s rejected by user (%s %s)", hostname, key.Type(), fp)
	}
	if err := appendKnownHost(appFile, hostname, key); err != nil {
		// Trust for this connection only; next time the user is asked again.
		log.Writef("[hostkey] could not persist key for %s: %v", hostname, err)
	}
	return nil
}

func appendKnownHost(file, hostname string, key ssh.PublicKey) error {
	if file == "" {
		return errors.New("no known_hosts path")
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key) + "\n")
	return err
}
