// Package vault is the end-to-end encrypted credential vault shared between
// devices (see DESENHO-credenciais.md). Storage only ever sees ciphertext and
// signed public metadata:
//
//	roster/<counter>          signed device roster, a hash chain (anti-rollback)
//	keys/<deviceID>.<epoch>   vault key of an epoch, HPKE-wrapped per device
//	ops/<deviceID>/<seq>      encrypted, signed operation (append-only)
//
// Every name is written by a single device and never rewritten, so a Store
// needs no compare-and-swap: only create-if-absent.
package vault

import (
	"errors"
	"strings"
)

var (
	// ErrNotFound: no object under that name.
	ErrNotFound = errors.New("vault: object not found")
	// ErrExists: Create on a name that is already taken.
	ErrExists = errors.New("vault: object already exists")
	// ErrBadName: the name is not a valid object name.
	ErrBadName = errors.New("vault: invalid object name")
)

// Version identifies one content of an object (a content hash for the local
// file store, a revision id for remote stores). It changes whenever the
// bytes change, so a caller can tell a cached copy is stale.
type Version string

// Store is where a vault's objects live: a local folder (also the offline
// cache), later Google Drive or WebDAV. Implementations must be safe for
// concurrent use.
type Store interface {
	// Get returns the object and its version, or ErrNotFound.
	Get(name string) ([]byte, Version, error)
	// Create writes a new object, or fails with ErrExists without touching
	// the existing one. Vault objects are only ever created this way.
	Create(name string, data []byte) (Version, error)
	// Put writes the object, replacing any existing one (local state only;
	// vault objects use Create).
	Put(name string, data []byte) (Version, error)
	// List returns the names under prefix ("ops/" lists every op), sorted.
	List(prefix string) ([]string, error)
	// Delete removes the object; deleting a missing one is not an error.
	Delete(name string) error
}

// ValidName reports whether name is a relative object name made of
// "/"-separated segments of [A-Za-z0-9._-] that start with a letter or
// digit (so no ".", ".." or hidden names). Remote stores map names onto
// paths or file titles, so nothing that could escape the vault folder is
// accepted.
func ValidName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg[0] == '.' || seg[0] == '_' || seg[0] == '-' {
			return false
		}
		for _, c := range seg {
			ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
			if !ok {
				return false
			}
		}
	}
	return true
}
