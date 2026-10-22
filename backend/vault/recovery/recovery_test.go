package recovery

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/ys-ll/uniterm/backend/vault"
)

func TestWordListIsBIP39English(t *testing.T) {
	sum := sha256.Sum256([]byte(wordFile))
	if got := hex.EncodeToString(sum[:]); got != "2f5eed53a4727b4bf8880d8f3f199efc90e58503646d9ff8eff3a2ed3b24dbda" {
		t.Fatalf("bip39-english.txt sha256 = %s", got)
	}
	if len(wordList) != 2048 || len(wordIndex) != 2048 {
		t.Fatalf("%d words, %d distinct", len(wordList), len(wordIndex))
	}
	prefixes := map[string]string{}
	for _, w := range wordList {
		if len(w) < 4 {
			continue
		}
		if o, dup := prefixes[w[:4]]; dup {
			t.Fatalf("%q and %q share a 4-letter prefix", o, w)
		}
		prefixes[w[:4]] = w
	}
}

// Vectors from the BIP-39 reference (trezor/python-mnemonic vectors.json).
func TestBIP39Vectors(t *testing.T) {
	for _, tc := range []struct{ entropy, phrase string }{
		{strings.Repeat("00", 32), strings.Repeat("abandon ", 23) + "art"},
		{strings.Repeat("7f", 32), "legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth title"},
		{strings.Repeat("ff", 32), strings.Repeat("zoo ", 23) + "vote"},
	} {
		e, _ := hex.DecodeString(tc.entropy)
		if got := strings.Join(entropyToWords(e), " "); got != tc.phrase {
			t.Errorf("entropy %s…: got %q", tc.entropy[:4], got)
		}
		back, err := wordsToEntropy(SplitPhrase(strings.ToUpper(tc.phrase)))
		if err != nil || !bytes.Equal(back, e) {
			t.Errorf("decode %q: %x, %v", tc.phrase[:12], back, err)
		}
	}
}

func TestPhraseErrors(t *testing.T) {
	words, k, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := KeyFromWords(words)
	if err != nil || k2.DeviceID() != k.DeviceID() {
		t.Fatalf("same phrase must give the same key: %v", err)
	}
	short := make([]string, len(words))
	for i, w := range words {
		short[i] = w[:min(4, len(w))]
	}
	if k3, err := KeyFromWords(short); err != nil || k3.DeviceID() != k.DeviceID() {
		t.Fatalf("4-letter form must work: %v", err)
	}

	bad := append([]string(nil), words...)
	bad[6] = "notaword"
	var we *WordError
	if _, err := KeyFromWords(bad); !errors.As(err, &we) || we.Position != 7 {
		t.Fatalf("unknown word: %v", err)
	}
	swapped := append([]string(nil), words...)
	for _, w := range wordList { // replace one word with another list word
		if w != swapped[3] {
			swapped[3] = w
			if _, err := wordsToEntropy(swapped); errors.Is(err, ErrChecksum) {
				break
			}
		}
	}
	if _, err := KeyFromWords(swapped); !errors.Is(err, ErrChecksum) {
		t.Fatalf("swapped word: %v", err)
	}
	if _, err := KeyFromWords(words[:12]); err == nil {
		t.Fatal("12 words must be refused")
	}
}

func TestChallenge(t *testing.T) {
	words, _, _ := Generate()
	pos, err := Challenge(3)
	if err != nil || len(pos) != 3 || pos[0] == pos[1] || pos[1] == pos[2] || pos[0] == pos[2] {
		t.Fatalf("challenge %v %v", pos, err)
	}
	ans := []string{" " + strings.ToUpper(words[pos[0]-1]), words[pos[1]-1], words[pos[2]-1]}
	if !Check(words, pos, ans) {
		t.Fatal("right answers refused")
	}
	ans[2] = "abandon"
	if words[pos[2]-1] != "abandon" && Check(words, pos, ans) {
		t.Fatal("wrong answer accepted")
	}
	if Check(words, nil, nil) {
		t.Fatal("empty challenge accepted")
	}
}

func newVault(t *testing.T, s vault.Store) (*vault.Vault, *vault.SoftwareDeviceKey) {
	t.Helper()
	dev, err := vault.NewSoftwareDeviceKey()
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Create(s, vault.NewMemState(vault.State{}), "pessoal", dev, "henrique", "Mac mini")
	if err != nil {
		t.Fatal(err)
	}
	return v, dev
}

func openAs(t *testing.T, s vault.Store, tr vault.TrustState, dev vault.DeviceKey) *vault.Vault {
	t.Helper()
	v, err := vault.Open(s, vault.NewMemState(vault.State{Trust: tr, NextSeq: 1}), dev)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// All devices lost: the phrase alone brings a new device in, and it reads
// what was written before, across a key rotation.
func TestSetupAndRecover(t *testing.T) {
	s := vault.NewMemStore()
	v, _ := newVault(t, s)
	words, err := Setup(s, v)
	if err != nil {
		t.Fatal(err)
	}
	if len(words) != WordCount {
		t.Fatalf("%d words", len(words))
	}
	var rec int
	for _, m := range v.Roster().Members {
		if IsRecoveryMember(m) {
			rec++
			if m.Role != vault.RoleAdmin {
				t.Fatal("recovery member must be admin")
			}
		}
	}
	if rec != 1 {
		t.Fatalf("%d recovery members", rec)
	}
	if _, err := v.Append([]byte("senha-antiga")); err != nil {
		t.Fatal(err)
	}
	// A rotation after setup: the recovery key gets the new epoch too.
	other, _ := vault.NewSoftwareDeviceKey()
	if err := v.AddMember(vault.MemberOf(other, "henrique", "iPhone", vault.RoleWriter)); err != nil {
		t.Fatal(err)
	}
	if err := v.Revoke(vault.IDOf(other)); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Append([]byte("senha-nova")); err != nil {
		t.Fatal(err)
	}

	fresh, _ := vault.NewSoftwareDeviceKey()
	tr, err := Recover(s, words, fresh, "henrique", "MacBook novo")
	if err != nil {
		t.Fatal(err)
	}
	nv := openAs(t, s, tr, fresh)
	log, err := nv.ReadLog()
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Ops) != 2 || string(log.Ops[0].Payload) != "senha-antiga" || string(log.Ops[1].Payload) != "senha-nova" {
		t.Fatalf("recovered log: %+v", log.Ops)
	}
	if _, err := nv.Append([]byte("depois")); err != nil {
		t.Fatalf("recovered device must be able to write: %v", err)
	}
	// Recovering again (same device) is idempotent.
	if _, err := Recover(s, words, fresh, "henrique", "MacBook novo"); err != nil {
		t.Fatal(err)
	}
}

func TestWrongPhraseOrForgedAnchor(t *testing.T) {
	s := vault.NewMemStore()
	v, _ := newVault(t, s)
	if _, err := Setup(s, v); err != nil {
		t.Fatal(err)
	}
	other, _, _ := Generate()
	dev, _ := vault.NewSoftwareDeviceKey()
	if _, err := Recover(s, other, dev, "x", "y"); !errors.Is(err, ErrNoAnchor) {
		t.Fatalf("other phrase: %v", err)
	}
}

// Storage builds a look-alike vault around the recovery member's public
// keys (they are in the plaintext roster). Without an anchor signed by the
// phrase, Recover must not enter it.
func TestLookAlikeVaultNotRecovered(t *testing.T) {
	words, k, _ := Generate()
	fake := vault.NewMemStore()
	attacker, _ := vault.NewSoftwareDeviceKey()
	fv, err := vault.Create(fake, vault.NewMemState(vault.State{}), "pessoal", attacker, "x", "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := fv.AddMember(vault.MemberOf(k, "", RecoveryName, vault.RoleAdmin)); err != nil {
		t.Fatal(err)
	}
	// An unsigned / attacker-signed anchor under the right name is ignored.
	if _, err := fake.Create(anchorPrefix(k.DeviceID())+"2", []byte(`{"body":"e30=","sig":"AAAA"}`)); err != nil {
		t.Fatal(err)
	}
	dev, _ := vault.NewSoftwareDeviceKey()
	if _, err := Recover(fake, words, dev, "h", "novo"); !errors.Is(err, ErrNoAnchor) {
		t.Fatalf("look-alike vault: %v", err)
	}
}

func TestSetupReplacesOldPhrase(t *testing.T) {
	s := vault.NewMemStore()
	v, _ := newVault(t, s)
	oldWords, err := Setup(s, v)
	if err != nil {
		t.Fatal(err)
	}
	newWords, err := Setup(s, v)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range v.Roster().Members {
		if IsRecoveryMember(m) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d recovery members after replacing", n)
	}
	dev, _ := vault.NewSoftwareDeviceKey()
	if _, err := Recover(s, oldWords, dev, "h", "novo"); err == nil {
		t.Fatal("the replaced phrase must stop working")
	}
	if _, err := Recover(s, newWords, dev, "h", "novo"); err != nil {
		t.Fatal(err)
	}
}

// The Engrenagem flow: create + setup on a draft store, copy to the real
// one after the user confirms the words.
func TestSetupOnDraftStoreThenCopy(t *testing.T) {
	draft := vault.NewMemStore()
	v, dev := newVault(t, draft)
	words, err := Setup(draft, v)
	if err != nil {
		t.Fatal(err)
	}
	real, err := vault.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := draft.List("")
	for _, e := range entries {
		data, _, _ := draft.Get(e.ID)
		if _, err := real.Create(e.Name, data); err != nil {
			t.Fatal(err)
		}
	}
	openAs(t, real, v.Trust(), dev)
	nd, _ := vault.NewSoftwareDeviceKey()
	if _, err := Recover(real, words, nd, "h", "novo"); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverRefusesReservedName(t *testing.T) {
	s := vault.NewMemStore()
	v, _ := newVault(t, s)
	words, _ := Setup(s, v)
	dev, _ := vault.NewSoftwareDeviceKey()
	if _, err := Recover(s, words, dev, "h", RecoveryName); err == nil {
		t.Fatal("reserved name accepted")
	}
}
