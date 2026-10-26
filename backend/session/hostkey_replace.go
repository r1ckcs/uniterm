package session

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/ys-ll/uniterm/backend/log"
)

// A changed key is replaced only in the app's known_hosts, and there every
// entry for that host:port goes, whatever the key type, as `ssh-keygen -R`
// does: the device was replaced, so all its old keys are void, and keeping
// an old key of another type would let whoever holds the old device keep
// impersonating it. Other hosts, pattern lines, @cert-authority and
// @revoked lines are kept. The user's ~/.ssh/known_hosts is never written:
// when the old key lives there, the new key in the app file overrides it
// (lookupHostKey gives the app file precedence for the host).

// replacePlan is a replacement the prompt may offer.
type replacePlan struct {
	appFile  string
	hostname string
	removed  int // entries removed by apply
}

// planReplace fills p's old keys and whether the app can replace them.
// Every app-file entry of the host (appAll, any key type) must be an exact
// line apply can remove: an entry that matches through a wildcard would
// keep an old key trusted after the replace.
func planReplace(appFile, userFile, hostname string, old, appAll []knownhosts.KnownKey, p *HostKeyPrompt) *replacePlan {
	check := append([]knownhosts.KnownKey(nil), appAll...)
	for _, o := range old {
		file := "app"
		if userFile != "" && o.Filename == userFile {
			file = "user"
			p.UserFileKept = true
		} else {
			check = append(check, o)
		}
		bits, _ := rsaKeyBits(o.Key)
		p.OldKeys = append(p.OldKeys, HostKeyOld{KeyType: o.Key.Type(), Fingerprint: ssh.FingerprintSHA256(o.Key), File: file, KeyBits: bits})
	}
	if p.UserFileKept {
		p.RemoveCommand = removeCommand(hostname)
	}
	var matched map[int]bool
	for _, o := range check {
		if o.Filename != appFile {
			continue
		}
		if matched == nil {
			data, err := os.ReadFile(appFile)
			if err != nil {
				p.Blocker = "pattern"
				return nil
			}
			_, matched = filterHostLines(data, knownhosts.Normalize(hostname))
		}
		if !matched[o.Line] {
			p.Blocker = "pattern"
			return nil
		}
	}
	p.CanReplace = true
	return &replacePlan{appFile: appFile, hostname: hostname}
}

// apply removes the host's entries from the app file (read again: the user
// may have edited it during the prompt) and appends key, atomically.
func (r *replacePlan) apply(key ssh.PublicKey) error {
	if r == nil {
		return errors.New("nothing to replace")
	}
	data, err := os.ReadFile(r.appFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.appFile), 0o700); err != nil {
		return err
	}
	norm := knownhosts.Normalize(r.hostname)
	out, matched := filterHostLines(data, norm)
	r.removed = len(matched)
	out = append(out, []byte(knownhosts.Line([]string{norm}, key)+"\n")...)
	return writeFileAtomic(r.appFile, out)
}

// ForgetResult is what ForgetHostKey did.
type ForgetResult struct {
	Removed int `json:"removed"` // entries removed from the app's known_hosts
	// InUserFile: ~/.ssh/known_hosts still trusts the host (not edited).
	InUserFile bool `json:"inUserFile"`
	// RemoveCommand removes it there ("" when unsafe to paste).
	RemoveCommand string `json:"removeCommand,omitempty"`
}

// ForgetHostKey removes every trusted key of host:port from the app's
// known_hosts; the next connection asks as for an unknown host.
func ForgetHostKey(host string, port int) (ForgetResult, error) {
	if host == "" || port <= 0 || port > 65535 {
		return ForgetResult{}, errors.New("invalid host or port")
	}
	return forgetHostKey(appKnownHostsPath(), userKnownHostsPath(), net.JoinHostPort(host, strconv.Itoa(port)))
}

func forgetHostKey(appFile, userFile, hostname string) (ForgetResult, error) {
	var res ForgetResult
	norm := knownhosts.Normalize(hostname)
	hostKeyPromptMu.Lock()
	defer hostKeyPromptMu.Unlock()
	if data, err := os.ReadFile(appFile); err == nil {
		out, matched := filterHostLines(data, norm)
		if len(matched) > 0 {
			if err := writeFileAtomic(appFile, out); err != nil {
				return res, err
			}
			res.Removed = len(matched)
			log.Writef("[hostkey] forgot %d entries for %s in %s", res.Removed, hostname, appFile)
		}
	} else if !os.IsNotExist(err) {
		return res, err
	}
	if userFile != "" {
		if data, err := os.ReadFile(userFile); err == nil {
			if _, m := filterHostLines(data, norm); len(m) > 0 {
				res.InUserFile = true
				res.RemoveCommand = removeCommand(hostname)
			}
		}
	}
	return res, nil
}

// filterHostLines drops norm from every plain host entry (exact name or
// hashed |1| name) and returns the remaining file plus the 1-based numbers
// of the lines that had it. A line naming other hosts too keeps them.
func filterHostLines(data []byte, norm string) ([]byte, map[int]bool) {
	matched := map[int]bool{}
	var out bytes.Buffer
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1] // trailing newline
	}
	for i, line := range lines {
		keep := line
		if hosts, rest, ok := hostField(line); ok {
			var remaining []string
			for _, h := range hosts {
				if hostEntryMatches(h, norm) {
					continue
				}
				remaining = append(remaining, h)
			}
			if len(remaining) != len(hosts) {
				matched[i+1] = true
				keep = nil
				if len(remaining) > 0 {
					keep = []byte(strings.Join(remaining, ",") + " " + rest)
				}
			}
		}
		if keep != nil {
			out.Write(keep)
			out.WriteByte('\n')
		}
	}
	return out.Bytes(), matched
}

// hostField splits a plain host entry into its host list and the rest of
// the line (key type, key, comment). Comments, blank lines, @markers and
// lines that do not parse return ok=false and are kept as they are.
func hostField(line []byte) (hosts []string, rest string, ok bool) {
	t := bytes.TrimSpace(line)
	if len(t) == 0 || t[0] == '#' || t[0] == '@' {
		return nil, "", false
	}
	if _, _, _, _, _, err := ssh.ParseKnownHosts(t); err != nil {
		return nil, "", false
	}
	f := strings.TrimRight(string(t), "\r")
	i := strings.IndexAny(f, " \t")
	if i < 0 {
		return nil, "", false
	}
	return strings.Split(f[:i], ","), strings.TrimLeft(f[i:], " \t"), true
}

// hostEntryMatches compares one host entry with the normalized address.
// Wildcard and negated patterns never match: they are not "this host".
func hostEntryMatches(entry, norm string) bool {
	if entry == norm {
		return true
	}
	// "[host]:22" written by hand is the same as "host".
	if strings.HasPrefix(entry, "[") {
		if i := strings.Index(entry, "]:"); i > 0 && !strings.ContainsAny(entry, "*?!") {
			return knownhosts.Normalize(net.JoinHostPort(entry[1:i], entry[i+2:])) == norm
		}
	}
	if !strings.HasPrefix(entry, "|1|") {
		return false
	}
	parts := strings.Split(entry[3:], "|")
	if len(parts) != 2 {
		return false
	}
	salt, err1 := base64.StdEncoding.DecodeString(parts[0])
	want, err2 := base64.StdEncoding.DecodeString(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(norm))
	return hmac.Equal(mac.Sum(nil), want)
}

// safeHost is what removeCommand will paste into a shell command.
var safeHost = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// removeCommand is the OpenSSH command that removes the host from the
// user's known_hosts, or "" when the host has characters a shell would
// interpret (never show something unsafe to paste).
func removeCommand(hostname string) string {
	host, port, err := net.SplitHostPort(hostname)
	if err != nil {
		host, port = hostname, "22"
	}
	if !safeHost.MatchString(host) || !safeHost.MatchString(port) || strings.HasPrefix(host, "-") {
		return ""
	}
	if port == "22" {
		return "ssh-keygen -R " + host
	}
	return fmt.Sprintf("ssh-keygen -R '[%s]:%s'", host, port)
}

// writeFileAtomic replaces file with data (0600) through a temp file in the
// same directory, so a crash leaves the old or the new file, never half.
func writeFileAtomic(file string, data []byte) error {
	dir := filepath.Dir(file)
	tmp, err := os.CreateTemp(dir, ".known_hosts-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op after the rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, file)
}
