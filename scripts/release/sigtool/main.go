// Command sigtool is the online half of the release-signing workflow (see
// RELEASES.md). It never touches the private key: signing happens on an
// offline machine with scripts/release/offline-sign.sh.
//
//	sigtool checksums <dir>                 write <dir>/checksums.txt for uniterm-* assets
//	sigtool verify -pub <key|@file> <dir>   check checksums.txt.sig and every asset hash
//	sigtool binkey -expect <key|@file> <binary>
//	                                        check that a built binary embeds the release key
//	sigtool fingerprint -pub <key|@file>    print the key fingerprint shown by the offline scripts
//	sigtool embedded                        print the key compiled into this sigtool (test hook)
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ys-ll/uniterm/backend/update"
)

// keySymbol is the -ldflags -X target the build embeds the release key into.
const keySymbol = "github.com/ys-ll/uniterm/backend/update.releasePublicKey"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "checksums":
		err = cmdChecksums(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "binkey":
		err = cmdBinKey(os.Args[2:])
	case "fingerprint":
		err = cmdFingerprint(os.Args[2:])
	case "embedded":
		fmt.Println(update.EmbeddedReleasePublicKey())
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sigtool:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: sigtool checksums <dir> | verify -pub <key|@file> <dir> | binkey -expect <key|@file> <binary> | fingerprint -pub <key|@file> | embedded")
	os.Exit(2)
}

// readKeyArg accepts the base64 key itself or @path to a file holding it.
func readKeyArg(v string) (string, error) {
	if strings.HasPrefix(v, "@") {
		data, err := os.ReadFile(v[1:])
		if err != nil {
			return "", err
		}
		v = string(data)
	}
	v = strings.TrimSpace(v)
	if _, err := update.ParsePublicKey(v); err != nil {
		return "", err
	}
	return v, nil
}

// Fingerprint is SHA-256 over the 32 raw key bytes, first 32 hex digits in
// groups of 4 — the same value offline-keygen.sh prints.
func Fingerprint(b64 string) (string, error) {
	key, err := update.ParsePublicKey(b64)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(key)
	h := hex.EncodeToString(sum[:])[:32]
	var parts []string
	for i := 0; i < len(h); i += 4 {
		parts = append(parts, h[i:i+4])
	}
	return strings.Join(parts, " "), nil
}

// releaseAssets lists the files that belong in checksums.txt.
func releaseAssets(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasPrefix(e.Name(), "uniterm-") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no uniterm-* assets in %s", dir)
	}
	return names, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func cmdChecksums(args []string) error {
	if len(args) != 1 {
		usage()
	}
	dir := args[0]
	names, err := releaseAssets(dir)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, n := range names {
		sum, err := sha256File(filepath.Join(dir, n))
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  %s\n", sum, n)
	}
	// A stale signature never matches the new list; drop it so verify
	// cannot pass on a leftover.
	_ = os.Remove(filepath.Join(dir, "checksums.txt.sig"))
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(b.String()), 0644); err != nil {
		return err
	}
	fmt.Print(b.String())
	return nil
}

// Verify checks the signature over dir/checksums.txt and that the listed
// hashes and the uniterm-* files in dir match exactly.
func Verify(pub, dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(filepath.Join(dir, "checksums.txt.sig"))
	if err != nil {
		return err
	}
	if !update.VerifyChecksumsSignature(pub, data, sig) {
		return errors.New("checksums.txt.sig does not verify against the release public key")
	}
	sums := update.ParseChecksums(data)
	names, err := releaseAssets(dir)
	if err != nil {
		return err
	}
	if len(sums) != len(names) {
		return fmt.Errorf("checksums.txt lists %d files, %s has %d uniterm-* assets", len(sums), dir, len(names))
	}
	for _, n := range names {
		want, ok := sums[n]
		if !ok {
			return fmt.Errorf("%s is not listed in checksums.txt", n)
		}
		got, err := sha256File(filepath.Join(dir, n))
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("sha256 mismatch for %s", n)
		}
	}
	return nil
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	pubArg := fs.String("pub", "", "base64 public key or @file")
	fs.Parse(args)
	if fs.NArg() != 1 || *pubArg == "" {
		usage()
	}
	pub, err := readKeyArg(*pubArg)
	if err != nil {
		return err
	}
	if err := Verify(pub, fs.Arg(0)); err != nil {
		return err
	}
	fp, _ := Fingerprint(pub)
	fmt.Printf("OK: signature and all asset hashes verified (key %s)\n", fp)
	return nil
}

// BinaryHasKey reports whether the release key string is present in a built
// binary. The -X value is stored verbatim as a string constant; build info
// cannot be used because -trimpath (release builds) omits -ldflags from it.
// A 44-character random base64 string does not occur by chance.
func BinaryHasKey(path, key string) (bool, error) {
	if key == "" {
		return false, errors.New("empty key")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return bytes.Contains(data, []byte(key)), nil
}

func cmdBinKey(args []string) error {
	fs := flag.NewFlagSet("binkey", flag.ExitOnError)
	expectArg := fs.String("expect", "", "release key that must be embedded (base64 or @file)")
	fs.Parse(args)
	if fs.NArg() != 1 || *expectArg == "" {
		usage()
	}
	want, err := readKeyArg(*expectArg)
	if err != nil {
		return err
	}
	ok, err := BinaryHasKey(fs.Arg(0), want)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s does not embed the release key (its updater would never install)", fs.Arg(0))
	}
	fp, _ := Fingerprint(want)
	fmt.Printf("OK: %s embeds release key %s\n", fs.Arg(0), fp)
	return nil
}

func cmdFingerprint(args []string) error {
	fs := flag.NewFlagSet("fingerprint", flag.ExitOnError)
	pubArg := fs.String("pub", "", "base64 public key or @file")
	fs.Parse(args)
	if *pubArg == "" {
		usage()
	}
	pub, err := readKeyArg(*pubArg)
	if err != nil {
		return err
	}
	fp, err := Fingerprint(pub)
	if err != nil {
		return err
	}
	fmt.Println(fp)
	return nil
}
