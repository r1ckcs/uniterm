// Package vault is the end-to-end encrypted credential vault shared between
// devices (see DESENHO-credenciais.md). Storage only ever sees ciphertext and
// signed public metadata:
//
//	roster/<counter>          signed device roster, a hash chain (anti-rollback)
//	keys/<deviceID>.<epoch>   vault key of an epoch, HPKE-wrapped per device
//	ops/<deviceID>/<seq>      encrypted, signed operation (append-only)
//
// Remote stores (Google Drive) can't create a file only if its name is
// free, and two files may share a name. So a name holds a set of
// candidates: anyone with write access can add one, and clients keep only
// the candidates that verify (signature, chain, key check), ignoring the
// rest instead of failing on them.
package vault

import (
	"errors"
	"strings"
)

var (
	// ErrNotFound: no object with that id.
	ErrNotFound = errors.New("vault: object not found")
	// ErrExists: Create on a name a unique-name store already has.
	ErrExists = errors.New("vault: object already exists")
	// ErrBadName: the name is not a valid object name.
	ErrBadName = errors.New("vault: invalid object name")
)

// Version identifies one content of an object (a content hash for the local
// file store, a revision id for remote stores).
type Version string

// Entry is one stored object. Several entries may share a Name.
type Entry struct {
	// ID is unique per object (the name in FileStore, a file id in Drive).
	ID   string
	Name string
	// Created is the server-side creation time (Unix nanoseconds). With the
	// content hash it breaks ties between valid roster candidates, and it
	// delays pinning a fresh version (see PinDelay). Drive lets the creator
	// set createdTime, so it must never carry any other security role: only
	// admin-signed versions compete, and those authors are trusted anyway.
	Created int64
}

// Store is where a vault's objects live: a local folder (also the offline
// cache), later Google Drive or WebDAV. Implementations must be safe for
// concurrent use.
type Store interface {
	// List returns the entries whose name starts with prefix, sorted by
	// name, then Created. Names may repeat.
	List(prefix string) ([]Entry, error)
	// Get returns an object's content by id, or ErrNotFound.
	Get(id string) ([]byte, Version, error)
	// Create adds an object under name. A store with unique names (the
	// local folder) fails with ErrExists if the name is taken; a store
	// without (Drive) just adds another candidate.
	Create(name string, data []byte) (Entry, error)
	// Delete removes an object by id; a missing one is not an error.
	Delete(id string) error
}

// candidates returns the entries stored under exactly name.
func candidates(s Store, name string) ([]Entry, error) {
	entries, err := s.List(name)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range entries {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out, nil
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
