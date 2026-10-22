package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileStore keeps a vault's objects as files under a folder. It is the
// local backend and, in front of a remote store, the offline cache. Names
// are unique here (one candidate per name) and an entry's ID is its name.
type FileStore struct {
	dir string
}

// tmpPrefix marks in-flight writes; ValidName never accepts a name starting
// with ".", so they can't collide with objects and List skips them.
const tmpPrefix = ".vault-tmp-"

// NewFileStore uses dir (created 0700 if missing).
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &FileStore{dir: dir}, nil
}

func (s *FileStore) path(name string) (string, error) {
	if !ValidName(name) {
		return "", ErrBadName
	}
	return filepath.Join(s.dir, filepath.FromSlash(name)), nil
}

func contentVersion(data []byte) Version {
	sum := sha256.Sum256(data)
	return Version(hex.EncodeToString(sum[:16]))
}

func (s *FileStore) Get(id string) ([]byte, Version, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, "", err
	}
	// Objects are plain files: a symlink planted in the folder is not
	// followed.
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if !fi.Mode().IsRegular() {
		return nil, "", ErrNotFound
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, "", err
	}
	return data, contentVersion(data), nil
}

// writeTemp writes data to a synced temp file next to p.
func writeTemp(p string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(p), tmpPrefix+"*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func (s *FileStore) Create(name string, data []byte) (Entry, error) {
	p, err := s.path(name)
	if err != nil {
		return Entry{}, err
	}
	tmp, err := writeTemp(p, data)
	if err != nil {
		return Entry{}, err
	}
	defer os.Remove(tmp)
	// link(2) fails if the target exists, so the object appears complete or
	// not at all, and two writers racing on one name can't both win.
	if err := os.Link(tmp, p); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Entry{}, ErrExists
		}
		return Entry{}, err
	}
	return s.entry(name, p)
}

func (s *FileStore) entry(name, p string) (Entry, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return Entry{}, err
	}
	return Entry{ID: name, Name: name, Created: fi.ModTime().UnixNano()}, nil
}

// Put writes name, replacing any existing object. Not part of Store (vault
// objects are only created); used for local copies and by tests.
func (s *FileStore) Put(name string, data []byte) (Version, error) {
	p, err := s.path(name)
	if err != nil {
		return "", err
	}
	tmp, err := writeTemp(p, data)
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return contentVersion(data), nil
}

func (s *FileStore) List(prefix string) ([]Entry, error) {
	var out []Entry
	err := filepath.WalkDir(s.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(s.dir, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if strings.HasPrefix(name, prefix) && ValidName(name) {
			info, err := d.Info()
			if err != nil {
				return err
			}
			out = append(out, Entry{ID: name, Name: name, Created: info.ModTime().UnixNano()})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, err
}

func (s *FileStore) Delete(id string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
