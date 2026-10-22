package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
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

// opHeader is the signed body of an op object. Author and Seq repeat the
// object name, and the name is in the AEAD's AAD, so an op can't be moved
// to another name or vault.
type opHeader struct {
	Format     int    `json:"format"`
	VaultID    string `json:"vaultId"`
	Author     string `json:"author"`
	Seq        uint64 `json:"seq"`
	Epoch      uint64 `json:"epoch"`
	Lamport    uint64 `json:"lamport"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

const opFormat = 1

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
func sealOp(dev DeviceKey, vaultID string, seq, epoch, lamport uint64, vk, payload []byte) ([]byte, error) {
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
		Nonce:      nonce,
		Ciphertext: gcm.Seal(nil, nonce, payload, opAAD(vaultID, opName(author, seq), epoch, seq)),
	}
	obj, _, err := seal(dev, "op", h)
	return obj, err
}

// openOp verifies and decrypts one stored op against the current roster.
func openOp(r *Roster, keys map[uint64][]byte, name string, data []byte) (Op, error) {
	author, seq, ok := parseOpName(name)
	if !ok {
		return Op{}, fmt.Errorf("malformed op name")
	}
	var obj signed
	if err := json.Unmarshal(data, &obj); err != nil {
		return Op{}, fmt.Errorf("malformed op: %v", err)
	}
	if obj.Signer != author {
		return Op{}, fmt.Errorf("signed by %s, stored under %s", obj.Signer, author)
	}
	// Authors are judged by the current roster: a removed device's ops are
	// refused, even ones tagged with an old epoch whose key it still knows.
	m, ok := r.member(author)
	if !ok {
		return Op{}, fmt.Errorf("author %s is not a member", author)
	}
	if !m.Role.canWrite() {
		return Op{}, fmt.Errorf("author %s is a %s, not a writer", author, m.Role)
	}
	if err := verify(m.SignPub, "op", obj.Body, obj.Sig); err != nil {
		return Op{}, err
	}
	var h opHeader
	if err := json.Unmarshal(obj.Body, &h); err != nil {
		return Op{}, fmt.Errorf("malformed op body: %v", err)
	}
	if h.Format != opFormat || h.VaultID != r.VaultID || h.Author != author || h.Seq != seq {
		return Op{}, fmt.Errorf("op header does not match its name or vault")
	}
	vk, ok := keys[h.Epoch]
	if !ok {
		return Op{}, fmt.Errorf("no key for epoch %d", h.Epoch)
	}
	gcm, err := newGCM(vk)
	if err != nil {
		return Op{}, err
	}
	if len(h.Nonce) != gcm.NonceSize() {
		return Op{}, fmt.Errorf("bad nonce")
	}
	payload, err := gcm.Open(nil, h.Nonce, h.Ciphertext, opAAD(h.VaultID, name, h.Epoch, h.Seq))
	if err != nil {
		return Op{}, fmt.Errorf("decrypt: %v", err)
	}
	return Op{Name: name, Author: author, Seq: seq, Epoch: h.Epoch, Lamport: h.Lamport, Payload: payload}, nil
}
