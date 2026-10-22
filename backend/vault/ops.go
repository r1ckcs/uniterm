package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Op is one decrypted, verified entry of the vault log.
type Op struct {
	Name    string // object name, ops/<author>/<seq>
	Author  string // device id
	Seq     uint64 // per-author sequence, from 1
	Epoch   uint64
	Lamport uint64 // logical clock for ordering and per-field merges
	Payload []byte
}

// Rejected is a log object that failed verification; it is skipped, never
// applied.
type Rejected struct {
	Name   string
	Reason string
}

// Log is the result of reading the vault log.
type Log struct {
	// Ops verified, ordered by (Lamport, author, seq).
	Ops []Op
	// Rejected candidates (never applied).
	Rejected []Rejected
	// Warnings worth showing: ignored roster candidates, admin races,
	// signs that storage hides roster versions, equivocating authors.
	Warnings []string
}

// RosterRef names a roster version.
type RosterRef struct {
	Counter uint64 `json:"counter"`
	Hash    string `json:"hash"`
}

// opHeader is the signed body of an op object. Author and Seq repeat the
// object name, and the name is in the AEAD's AAD, so an op can't be moved
// to another name or vault.
type opHeader struct {
	Format  int    `json:"format"`
	VaultID string `json:"vaultId"`
	Author  string `json:"author"`
	Seq     uint64 `json:"seq"`
	Epoch   uint64 `json:"epoch"`
	Lamport uint64 `json:"lamport"`
	// RosterSeen is the newest roster the author had accepted. A reader that
	// sees a newer one cited than it can load knows storage is hiding
	// roster versions from it (freeze).
	RosterSeen RosterRef `json:"rosterSeen"`
	Nonce      []byte    `json:"nonce"`
	Ciphertext []byte    `json:"ciphertext"`
}

const opFormat = 1

// Lamport bounds: a writer must not jump the clock so far ahead that its
// per-field writes win every later merge. maxLamport keeps values exact in
// JSON numbers on every client; maxLamportJump is the slack over the
// clocks of the other authors (a reader may not list everything the
// author had seen yet).
const (
	maxLamport     = 1 << 53
	maxLamportJump = 1 << 20
)

func opName(author string, seq uint64) string { return fmt.Sprintf("ops/%s/%012d", author, seq) }

func parseOpName(name string) (author string, seq uint64, ok bool) {
	rest, ok := strings.CutPrefix(name, "ops/")
	if !ok {
		return "", 0, false
	}
	author, s, ok := strings.Cut(rest, "/")
	if !ok || author == "" || len(s) != 12 {
		return "", 0, false
	}
	seq, err := strconv.ParseUint(s, 10, 64)
	return author, seq, err == nil && seq > 0
}

// opAAD = vaultID|objectID|epoch|seq (design), length-prefixed.
func opAAD(vaultID, name string, epoch, seq uint64) []byte {
	var b strings.Builder
	b.WriteString("uniterm-vault/v1 op\x00")
	for _, f := range []string{vaultID, name, strconv.FormatUint(epoch, 10), strconv.FormatUint(seq, 10)} {
		b.WriteString(strconv.Itoa(len(f)))
		b.WriteByte(':')
		b.WriteString(f)
	}
	return []byte(b.String())
}

func newGCM(vk []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(vk)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// sealOp encrypts and signs an op as dev. It does not check dev's role:
// readers' ops are refused on read, by every client.
func sealOp(dev DeviceKey, vaultID string, seq, epoch, lamport uint64, seen RosterRef, vk, payload []byte) ([]byte, error) {
	gcm, err := newGCM(vk)
	if err != nil {
		return nil, err
	}
	author := IDOf(dev)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	h := opHeader{
		Format: opFormat, VaultID: vaultID, Author: author, Seq: seq, Epoch: epoch, Lamport: lamport,
		RosterSeen: seen,
		Nonce:      nonce,
		Ciphertext: gcm.Seal(nil, nonce, payload, opAAD(vaultID, opName(author, seq), epoch, seq)),
	}
	obj, _, err := seal(dev, "op", h)
	return obj, err
}

// openOp verifies and decrypts one stored op candidate against the current
// roster. Epoch rules:
//   - an op under a closed epoch e is valid only if its seq is within the
//     author's cutoff for e (written before the rotation, as attested by the
//     rotating admin); then the author may also be a former member;
//   - any other op must be under the current epoch, by a current writer,
//     with a seq past the author's cutoff of the previous epoch.
func openOp(r *Roster, keys map[uint64][]byte, name string, data []byte) (Op, opHeader, error) {
	author, seq, ok := parseOpName(name)
	if !ok {
		return Op{}, opHeader{}, fmt.Errorf("malformed op name")
	}
	var obj signed
	if err := json.Unmarshal(data, &obj); err != nil {
		return Op{}, opHeader{}, fmt.Errorf("malformed op: %v", err)
	}
	if obj.Signer != author {
		return Op{}, opHeader{}, fmt.Errorf("signed by %s, stored under %s", obj.Signer, author)
	}
	var h opHeader
	if err := json.Unmarshal(obj.Body, &h); err != nil {
		return Op{}, h, fmt.Errorf("malformed op body: %v", err)
	}
	if h.Format != opFormat || h.VaultID != r.VaultID || h.Author != author || h.Seq != seq {
		return Op{}, h, fmt.Errorf("op header does not match its name or vault")
	}
	if h.Lamport == 0 || h.Lamport > maxLamport {
		return Op{}, h, fmt.Errorf("lamport %d out of range", h.Lamport)
	}

	var signer Member
	switch {
	case h.Epoch == 0 || h.Epoch > r.Epoch:
		return Op{}, h, fmt.Errorf("unknown epoch %d", h.Epoch)
	case h.Epoch < r.Epoch:
		if seq > r.cutoff(h.Epoch, author) {
			return Op{}, h, fmt.Errorf("written under epoch %d after it was closed", h.Epoch)
		}
		m, ok := r.member(author)
		if !ok {
			if m, ok = r.former(author); !ok {
				return Op{}, h, fmt.Errorf("author %s is not a member", author)
			}
		}
		signer = m
	default:
		m, ok := r.member(author)
		if !ok {
			return Op{}, h, fmt.Errorf("author %s is not a member", author)
		}
		if !m.Role.canWrite() {
			return Op{}, h, fmt.Errorf("author %s is a %s, not a writer", author, m.Role)
		}
		if seq <= r.cutoff(h.Epoch-1, author) {
			return Op{}, h, fmt.Errorf("seq %d reuses a number of a closed epoch", seq)
		}
		signer = m
	}
	if err := verify(signer.SignPub, "op", obj.Body, obj.Sig); err != nil {
		return Op{}, h, err
	}
	vk, ok := keys[h.Epoch]
	if !ok {
		return Op{}, h, fmt.Errorf("no key for epoch %d", h.Epoch)
	}
	gcm, err := newGCM(vk)
	if err != nil {
		return Op{}, h, err
	}
	if len(h.Nonce) != gcm.NonceSize() {
		return Op{}, h, fmt.Errorf("bad nonce")
	}
	payload, err := gcm.Open(nil, h.Nonce, h.Ciphertext, opAAD(h.VaultID, name, h.Epoch, h.Seq))
	if err != nil {
		return Op{}, h, fmt.Errorf("decrypt: %v", err)
	}
	return Op{Name: name, Author: author, Seq: seq, Epoch: h.Epoch, Lamport: h.Lamport, Payload: payload}, h, nil
}

// readLog verifies every op candidate in s against r. Beyond per-object
// checks it refuses:
//   - equivocation: two different valid ops under one name (the author's
//     every op at that name is refused and the author flagged);
//   - Lamport clocks that don't grow with the author's seq, or that jump
//     more than maxLamportJump past both the other authors' clocks and the
//     author's own previous op + 1.
func readLog(s Store, r *Roster, keys map[uint64][]byte, chain map[uint64]string) (Log, error) {
	var log Log
	entries, err := s.List("ops/")
	if err != nil {
		return log, err
	}
	type valid struct {
		op   Op
		body []byte
		seen RosterRef
	}
	byName := map[string][]valid{}
	var names []string
	for _, e := range entries {
		data, _, err := s.Get(e.ID)
		if err != nil {
			return log, err
		}
		op, h, err := openOp(r, keys, e.Name, data)
		if err != nil {
			log.Rejected = append(log.Rejected, Rejected{Name: e.Name, Reason: err.Error()})
			continue
		}
		var obj signed
		_ = json.Unmarshal(data, &obj)
		if _, ok := byName[e.Name]; !ok {
			names = append(names, e.Name)
		}
		byName[e.Name] = append(byName[e.Name], valid{op, obj.Body, h.RosterSeen})
	}

	byAuthor := map[string][]valid{}
	for _, n := range names {
		vs := byName[n]
		dup := false
		for _, v := range vs[1:] {
			if !bytes.Equal(v.body, vs[0].body) {
				dup = true
			}
		}
		if dup {
			for range vs {
				log.Rejected = append(log.Rejected, Rejected{Name: n, Reason: "equivocation: several different ops under one name"})
			}
			log.Warnings = append(log.Warnings, fmt.Sprintf("device %s wrote two different ops as %s", vs[0].op.Author, n))
			continue
		}
		v := vs[0]
		if v.seen.Counter > r.Counter {
			log.Warnings = append(log.Warnings, fmt.Sprintf("%s cites roster %d, newer than the accepted %d: storage may be hiding roster versions", n, v.seen.Counter, r.Counter))
		} else if h, ok := chain[v.seen.Counter]; ok && h != v.seen.Hash {
			log.Warnings = append(log.Warnings, fmt.Sprintf("%s cites a roster %d that differs from the accepted one", n, v.seen.Counter))
		}
		byAuthor[v.op.Author] = append(byAuthor[v.op.Author], v)
	}

	// Per author, the clock must grow with the seq.
	var ops []Op
	for _, vs := range byAuthor {
		sort.Slice(vs, func(i, j int) bool { return vs[i].op.Seq < vs[j].op.Seq })
		var last uint64
		for _, v := range vs {
			if v.op.Lamport <= last {
				log.Rejected = append(log.Rejected, Rejected{Name: v.op.Name, Reason: "lamport does not grow with seq"})
				continue
			}
			last = v.op.Lamport
			ops = append(ops, v.op)
		}
	}
	sortOps(ops)

	// Jump bound, in clock order.
	maxBy := map[string]uint64{}
	accepted := ops[:0]
	for _, op := range ops {
		var others uint64
		for a, l := range maxBy {
			if a != op.Author {
				others = max(others, l)
			}
		}
		if op.Lamport > max(others+maxLamportJump, maxBy[op.Author]+1) {
			log.Rejected = append(log.Rejected, Rejected{Name: op.Name, Reason: fmt.Sprintf("lamport %d jumps too far ahead", op.Lamport)})
			continue
		}
		maxBy[op.Author] = op.Lamport
		accepted = append(accepted, op)
	}
	log.Ops = accepted
	sort.Slice(log.Rejected, func(i, j int) bool { return log.Rejected[i].Name < log.Rejected[j].Name })
	return log, nil
}

func sortOps(ops []Op) {
	sort.Slice(ops, func(i, j int) bool {
		a, b := ops[i], ops[j]
		if a.Lamport != b.Lamport {
			return a.Lamport < b.Lamport
		}
		if a.Author != b.Author {
			return a.Author < b.Author
		}
		return a.Seq < b.Seq
	})
}
