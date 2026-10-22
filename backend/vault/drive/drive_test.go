package drive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ys-ll/uniterm/backend/vault"
)

// fakeDrive implements the few Drive v3 calls the store makes.
type fakeDrive struct {
	mu     sync.Mutex
	n      int
	files  map[string]*fakeFile
	tokens int
	down   bool
	unauth int // next N requests answer 401
	clock  time.Time
}

type fakeFile struct {
	id, name, mime, parent, created string
	props                           map[string]string
	data                            []byte
}

var (
	reParent = regexp.MustCompile(`'([^']+)' in parents`)
	reDir    = regexp.MustCompile(`value='([^']+)'`)
	reName   = regexp.MustCompile(`name='([^']+)'`)
)

func (f *fakeDrive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		http.Error(w, `{"error":{"errors":[{"reason":"backendError"}]}}`, 503)
		return
	}
	if r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, "{}", 401)
		return
	}
	if f.unauth > 0 {
		f.unauth--
		http.Error(w, "{}", 401)
		return
	}
	newFile := func(name, mt, parent string, props map[string]string, data []byte) *fakeFile {
		f.n++
		f.clock = f.clock.Add(time.Millisecond)
		ff := &fakeFile{id: fmt.Sprintf("f%d", f.n), name: name, mime: mt, parent: parent, props: props, data: data,
			created: f.clock.Format(time.RFC3339Nano)}
		f.files[ff.id] = ff
		return ff
	}
	reply := func(ff *fakeFile) {
		json.NewEncoder(w).Encode(map[string]any{"id": ff.id, "createdTime": ff.created})
	}
	switch {
	case r.URL.Path == "/upload/files" && r.Method == "POST":
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		p1, _ := mr.NextPart()
		var meta struct {
			Name          string            `json:"name"`
			Parents       []string          `json:"parents"`
			MimeType      string            `json:"mimeType"`
			AppProperties map[string]string `json:"appProperties"`
		}
		json.NewDecoder(p1).Decode(&meta)
		p2, _ := mr.NextPart()
		data, _ := io.ReadAll(p2)
		reply(newFile(meta.Name, meta.MimeType, meta.Parents[0], meta.AppProperties, data))
	case r.URL.Path == "/files" && r.Method == "POST":
		var meta struct{ Name, MimeType string }
		json.NewDecoder(r.Body).Decode(&meta)
		reply(newFile(meta.Name, meta.MimeType, "root", nil, nil))
	case r.URL.Path == "/files" && r.Method == "GET":
		q := r.URL.Query().Get("q")
		var out []map[string]any
		for _, ff := range f.files {
			if m := reParent.FindStringSubmatch(q); m != nil && ff.parent != m[1] {
				continue
			}
			if m := reDir.FindStringSubmatch(q); m != nil && ff.props["d"] != m[1] {
				continue
			}
			if strings.Contains(q, "mimeType=") {
				if m := reName.FindStringSubmatch(q); ff.mime != folderMime || m == nil || ff.name != m[1] {
					continue
				}
			}
			out = append(out, map[string]any{"id": ff.id, "createdTime": ff.created, "appProperties": ff.props})
		}
		json.NewEncoder(w).Encode(map[string]any{"files": out})
	case r.URL.Path == "/about":
		w.Write([]byte(`{"user":{"emailAddress":"henrique@example.com"}}`))
	case strings.HasPrefix(r.URL.Path, "/files/"):
		id := strings.TrimPrefix(r.URL.Path, "/files/")
		ff, ok := f.files[id]
		if !ok {
			http.Error(w, `{"error":{"errors":[{"reason":"notFound"}]}}`, 404)
			return
		}
		if r.Method == "DELETE" {
			delete(f.files, id)
			w.WriteHeader(204)
			return
		}
		w.Write(ff.data)
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), 400)
	}
}

type staticTokens struct{ invalidated int }

func (s *staticTokens) Token(context.Context) (string, error) { return "tok", nil }
func (s *staticTokens) Invalidate()                           { s.invalidated++ }

func setup(t *testing.T) (*fakeDrive, *Store, *staticTokens) {
	t.Helper()
	fd := &fakeDrive{files: map[string]*fakeFile{}, clock: time.Now()}
	srv := httptest.NewServer(fd)
	t.Cleanup(srv.Close)
	oldA, oldU := APIBase, UploadBase
	APIBase, UploadBase = srv.URL, srv.URL+"/upload"
	t.Cleanup(func() { APIBase, UploadBase = oldA, oldU })
	oldTTL := listTTL
	listTTL = 20 * time.Millisecond
	t.Cleanup(func() { listTTL = oldTTL })
	tk := &staticTokens{}
	return fd, New(srv.Client(), tk, ""), tk
}

func TestCandidatesAndListing(t *testing.T) {
	fd, s, _ := setup(t)
	a, err := s.Create("roster/000000000001", []byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create("roster/000000000001", []byte("b")) // same name: another candidate
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("ops/dev/000000000001", []byte("op")); err != nil {
		t.Fatal(err)
	}
	es, err := s.List("roster/")
	if err != nil || len(es) != 2 || es[0].ID != a.ID || es[1].ID != b.ID || es[0].Created >= es[1].Created {
		t.Fatalf("roster listing = %+v %v", es, err)
	}
	all, _ := s.List("")
	if len(all) != 3 {
		t.Fatalf("full listing = %+v", all)
	}
	data, _, err := s.Get(b.ID)
	if err != nil || string(data) != "b" {
		t.Fatalf("get = %q %v", data, err)
	}
	// Titles are random, the vault name is only in appProperties.
	for _, ff := range fd.files {
		if strings.Contains(ff.name, "roster") || strings.Contains(ff.name, "ops") {
			t.Fatalf("visible title leaks the name: %q", ff.name)
		}
	}
	if _, err := s.Create("../x", nil); !errors.Is(err, vault.ErrBadName) {
		t.Fatalf("bad name = %v", err)
	}
	must(t, s.Delete(a.ID))
	must(t, s.Delete(a.ID)) // missing is fine
	if _, _, err := s.Get(a.ID); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("get deleted = %v", err)
	}
	if e, _ := s.Email(); e != "henrique@example.com" {
		t.Fatalf("email = %q", e)
	}
}

func TestUnavailableAndReauth(t *testing.T) {
	fd, s, tk := setup(t)
	if _, err := s.Create("ops/a/000000000001", []byte("x")); err != nil {
		t.Fatal(err)
	}
	fd.unauth = 1
	time.Sleep(listTTL + 10*time.Millisecond)
	if _, err := s.List("ops/"); err != nil || tk.invalidated != 1 {
		t.Fatalf("401 retry = %v, invalidated %d", err, tk.invalidated)
	}
	fd.down = true
	time.Sleep(listTTL + 10*time.Millisecond)
	if _, err := s.List("ops/"); !errors.Is(err, vault.ErrUnavailable) {
		t.Fatalf("503 = %v, want ErrUnavailable", err)
	}
}

func TestVaultOnDrive(t *testing.T) {
	_, s, _ := setup(t)
	a, _ := vault.NewSoftwareDeviceKey()
	w, _ := vault.NewSoftwareDeviceKey()
	va, err := vault.Create(s, vault.NewMemState(vault.State{}), "pessoal", a, "h", "mac-a")
	must(t, err)
	must(t, va.AddMember(vault.MemberOf(w, "h", "mac-w", vault.RoleWriter)))
	st := vault.NewMemState(vault.State{Trust: vault.TrustState{VaultID: "pessoal", Counter: 1, Hash: genesis(t, s)}, NextSeq: 1})
	vw, err := vault.Open(s, st, w)
	must(t, err)
	_, err = vw.Append([]byte("olá"))
	must(t, err)
	time.Sleep(listTTL)
	must(t, va.Refresh())
	log, err := va.ReadLog()
	must(t, err)
	if len(log.Ops) != 1 || string(log.Ops[0].Payload) != "olá" {
		t.Fatalf("log = %+v", log)
	}
}

func genesis(t *testing.T, s *Store) string {
	t.Helper()
	es, _ := s.List("roster/000000000001")
	data, _, err := s.Get(es[0].ID)
	must(t, err)
	var obj struct{ Body []byte }
	must(t, json.Unmarshal(data, &obj))
	return vault.RosterHash(obj.Body)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
