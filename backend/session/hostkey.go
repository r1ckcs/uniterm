package session

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/ys-ll/uniterm/backend/appname"
	"github.com/ys-ll/uniterm/backend/log"
)

// HostKeyPrompt describes an SSH server key the user has not trusted yet,
// or (Changed) one that differs from the trusted key for the host.
type HostKeyPrompt struct {
	// Host is the "host:port" address the client dialed (per hop).
	Host        string `json:"host"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"` // SHA256:... (OpenSSH format)
	// OtherKeyTypesKnown is set when the host is already trusted with a key
	// of a different algorithm (OpenSSH prompts in that case too, with a
	// warning).
	OtherKeyTypesKnown bool `json:"otherKeyTypesKnown"`
	// KeyBits is the RSA modulus size (0 for other key types).
	KeyBits int `json:"keyBits,omitempty"`
	// Weak marks an RSA key below 1024 bits (accepted only on a connection
	// marked as a legacy device): the dialog warns before trusting it.
	Weak bool `json:"weak,omitempty"`

	// Changed: the host is trusted with a different key of the same type.
	// Either a man-in-the-middle or a replaced device. Only an explicit
	// HostKeyReplace answer replaces it.
	Changed bool `json:"changed,omitempty"`
	// OldKeys are the trusted keys that no longer match (Changed only).
	OldKeys []HostKeyOld `json:"oldKeys,omitempty"`
	// CanReplace is false only when the old key comes from a wildcard
	// pattern line in the app's known_hosts (Blocker "pattern"): the
	// prompt is then a notice and any answer rejects.
	CanReplace bool   `json:"canReplace,omitempty"`
	Blocker    string `json:"blocker,omitempty"`
	// UserFileKept: an old key is in ~/.ssh/known_hosts. The app never
	// edits that file; the new key goes to the app's file, which takes
	// precedence for this host from then on. RemoveCommand (empty for a
	// host unsafe to paste into a shell) tidies the user's file, optional.
	UserFileKept  bool   `json:"userFileKept,omitempty"`
	RemoveCommand string `json:"removeCommand,omitempty"`
}

// HostKeyOld is one trusted key that no longer matches.
type HostKeyOld struct {
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
	// File is "app" (the app's known_hosts) or "user" (~/.ssh/known_hosts).
	File string `json:"file"`
}

// HostKeyAnswer is the user's decision on a host key prompt.
type HostKeyAnswer int

const (
	HostKeyReject  HostKeyAnswer = iota
	HostKeyTrust                 // trust an unknown key; never replaces a changed one
	HostKeyReplace               // replace a changed key; never trusts an unknown one
)

// HostKeyPrompter asks the user about a host key. Only the answer matching
// the prompt (Trust for unknown, Replace for Changed) accepts; errors
// (timeout, cancel) reject.
type HostKeyPrompter func(p HostKeyPrompt) (HostKeyAnswer, error)

var (
	hostKeyCfgMu     sync.RWMutex
	knownHostsFile   string
	globalHKPrompter HostKeyPrompter

	// hostKeyPromptMu serializes prompts and every write to the app file so
	// parallel dials to the same host (terminal + SFTP + monitor) ask once:
	// waiters re-check the store after acquiring it, and reuse a rejection
	// made while they waited (hostKeyDecisions).
	hostKeyPromptMu sync.Mutex
	// hostKeyPromptSeq counts finished prompts (written under
	// hostKeyPromptMu, read before it). hostKeyRejected keeps the sequence
	// number of the last rejection per host+fingerprint (guarded by
	// hostKeyPromptMu): a dial that took its ticket before that rejection
	// was waiting during the prompt and shares the answer; a later dial
	// asks again.
	hostKeyPromptSeq atomic.Uint64
	hostKeyRejected  = map[string]uint64{}
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
	return filepath.Join(home, appname.HomeDir, "known_hosts")
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
	hostKeyRevoked // or a malformed address: never trusted, never asked
)

// checkKnownHosts checks key for hostname against the given known_hosts
// files (missing files are skipped; an unparsable user file is ignored with
// a log line rather than breaking every connection). For hostKeyChanged it
// also returns the trusted keys of the same type that no longer match.
func checkKnownHosts(files []string, hostname string, key ssh.PublicKey) (hostKeyStatus, []knownhosts.KnownKey, error) {
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
		return hostKeyUnknown, nil, nil
	}
	// knownhosts only needs a parsable remote address; the dialed hostname
	// takes precedence, so the (possibly proxied) peer address is irrelevant.
	err := cb(hostname, &net.TCPAddr{IP: net.IPv4zero}, key)
	if err == nil {
		return hostKeyKnown, nil, nil
	}
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) {
		// RevokedError or a malformed address: never trust.
		return hostKeyRevoked, nil, err
	}
	if len(ke.Want) == 0 {
		return hostKeyUnknown, nil, nil
	}
	var same []knownhosts.KnownKey
	for _, w := range ke.Want {
		if w.Key.Type() == key.Type() {
			same = append(same, w)
		}
	}
	if len(same) > 0 {
		return hostKeyChanged, same, nil
	}
	return hostKeyUnknownOtherType, nil, nil
}

// lookupHostKey decides with both files, the app's taking precedence: a
// key of this type that the app file trusts or contradicts for the host
// settles it, whatever ~/.ssh/known_hosts says. That is what lets a replace
// work without editing the user's file, and it keeps a replaced key out:
// the old key left in ~/.ssh/known_hosts no longer passes. @revoked in
// either file always wins. Otherwise both files count together, as before.
func lookupHostKey(appFile, userFile, hostname string, key ssh.PublicKey) (hostKeyStatus, []knownhosts.KnownKey, error) {
	merged, mergedOld, err := checkKnownHosts([]string{appFile, userFile}, hostname, key)
	if merged == hostKeyRevoked {
		return merged, nil, err
	}
	if app, appOld, _ := checkKnownHosts([]string{appFile}, hostname, key); app == hostKeyKnown || app == hostKeyChanged {
		return app, appOld, nil
	}
	return merged, mergedOld, err
}

func verifyHostKey(appFile, userFile, hostname string, key ssh.PublicKey, prompt HostKeyPrompter, allowPrompt bool) error {
	fp := ssh.FingerprintSHA256(key)

	status, _, err := lookupHostKey(appFile, userFile, hostname, key)
	switch status {
	case hostKeyKnown:
		return nil
	case hostKeyRevoked:
		return fmt.Errorf("host key verification failed for %s: %w", hostname, err)
	}

	if prompt == nil && allowPrompt {
		hostKeyCfgMu.RLock()
		prompt = globalHKPrompter
		hostKeyCfgMu.RUnlock()
	}
	if prompt == nil || !allowPrompt {
		if status == hostKeyChanged {
			return changedKeyError(hostname, key)
		}
		return fmt.Errorf("host key for %s is not trusted (%s %s); connect from a terminal session to verify and accept it", hostname, key.Type(), fp)
	}

	ticket := hostKeyPromptSeq.Load()
	hostKeyPromptMu.Lock()
	defer hostKeyPromptMu.Unlock()
	// Another dial may have accepted (or the user edited the file) while we
	// waited for the prompt lock.
	status, old, err := lookupHostKey(appFile, userFile, hostname, key)
	switch status {
	case hostKeyKnown:
		return nil
	case hostKeyRevoked:
		return fmt.Errorf("host key verification failed for %s: %w", hostname, err)
	}
	decision := hostname + " " + fp
	if seq, ok := hostKeyRejected[decision]; ok && seq > ticket {
		return fmt.Errorf("host key for %s rejected by user (%s %s)", hostname, key.Type(), fp)
	}

	p := HostKeyPrompt{
		Host:               hostname,
		KeyType:            key.Type(),
		Fingerprint:        fp,
		OtherKeyTypesKnown: status == hostKeyUnknownOtherType,
	}
	p.KeyBits, p.Weak = rsaKeyBits(key)
	want := HostKeyTrust
	var plan *replacePlan
	if status == hostKeyChanged {
		want = HostKeyReplace
		p.Changed = true
		plan = planReplace(appFile, userFile, hostname, old, &p)
	}

	answer, perr := prompt(p)
	seq := hostKeyPromptSeq.Add(1)
	accepted := perr == nil && answer == want && (!p.Changed || p.CanReplace)
	if !accepted {
		hostKeyRejected[decision] = seq
	} else {
		delete(hostKeyRejected, decision)
	}
	if perr != nil {
		return fmt.Errorf("host key for %s not accepted: %w", hostname, perr)
	}
	if !accepted {
		if p.Changed {
			if !p.CanReplace {
				return changedBlockedError(p)
			}
			return fmt.Errorf("changed host key for %s not replaced (%s %s)", hostname, key.Type(), fp)
		}
		return fmt.Errorf("host key for %s rejected by user (%s %s)", hostname, key.Type(), fp)
	}

	if p.Changed {
		if err := plan.apply(key); err != nil {
			return fmt.Errorf("could not replace the host key for %s: %w", hostname, err)
		}
		// The file must now trust exactly this key; anything else would
		// mean the plan missed an entry, so refuse rather than connect.
		if st, _, _ := lookupHostKey(appFile, userFile, hostname, key); st != hostKeyKnown {
			return fmt.Errorf("host key for %s still does not match after replacing it", hostname)
		}
		log.Writef("[hostkey] replaced key for %s: old %s, new %s %s (%d entries removed from %s; user file kept: %v)",
			hostname, describeOld(p.OldKeys), key.Type(), fp, plan.removed, appFile, p.UserFileKept)
		return nil
	}
	if err := appendKnownHost(appFile, hostname, key); err != nil {
		// Trust for this connection only; next time the user is asked again.
		log.Writef("[hostkey] could not persist key for %s: %v", hostname, err)
	}
	return nil
}

func describeOld(old []HostKeyOld) string {
	parts := make([]string, 0, len(old))
	for _, o := range old {
		parts = append(parts, o.KeyType+" "+o.Fingerprint)
	}
	return strings.Join(parts, ", ")
}

func changedBlockedError(p HostKeyPrompt) error {
	return fmt.Errorf("WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED for %s (%s %s). The old key matches a pattern line in the app known_hosts; edit that file if the change is legitimate", p.Host, p.KeyType, p.Fingerprint)
}

// changedKeyError is the hard failure for a changed key on a path that
// cannot ask (no prompter, or a no-prompt dial such as the cwd probe).
func changedKeyError(hostname string, key ssh.PublicKey) error {
	return fmt.Errorf("WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED for %s: server presented %s key %s, which does not match the trusted key. This may be a man-in-the-middle attack or a replaced device; connect from a terminal session to review it", hostname, key.Type(), ssh.FingerprintSHA256(key))
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

// rsaKeyBits returns an RSA key's modulus size and whether it is below the
// 1024-bit minimum (0, false for other key types).
func rsaKeyBits(key ssh.PublicKey) (int, bool) {
	ck, ok := key.(ssh.CryptoPublicKey)
	if !ok {
		return 0, false
	}
	pub, ok := ck.CryptoPublicKey().(*rsa.PublicKey)
	if !ok {
		return 0, false
	}
	return pub.N.BitLen(), pub.N.BitLen() < 1024
}
