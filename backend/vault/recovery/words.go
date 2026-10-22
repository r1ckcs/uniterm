package recovery

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"strings"
)

// The BIP-39 English list (github.com/bitcoin/bips, bip-0039/english.txt).
// Only the list and the checksum rule are used: no seed derivation, no
// cryptocurrency code. Its SHA-256 is pinned in the tests.
//
//go:embed bip39-english.txt
var wordFile string

var (
	wordList  = strings.Fields(wordFile)
	wordIndex = func() map[string]int {
		m := make(map[string]int, len(wordList))
		for i, w := range wordList {
			m[w] = i
		}
		return m
	}()
)

// WordCount is the length of a recovery phrase: 256 bits of entropy plus
// an 8-bit checksum, 11 bits per word.
const WordCount = 24

const entropySize = 32

// ErrChecksum: every word is valid but the phrase is not one this app
// generated (a word swapped or mistyped into another list word).
var ErrChecksum = errors.New("recovery: phrase checksum does not match")

// WordError points at the word that is not on the list.
type WordError struct {
	Position int // 1-based
	Word     string
}

func (e *WordError) Error() string {
	return fmt.Sprintf("recovery: word %d (%q) is not a recovery word", e.Position, e.Word)
}

// entropyToWords encodes 32 bytes as 24 words (BIP-39).
func entropyToWords(entropy []byte) []string {
	sum := sha256.Sum256(entropy)
	bits := append(append([]byte(nil), entropy...), sum[0]) // 264 bits
	words := make([]string, WordCount)
	for i := range words {
		idx := 0
		for b := i * 11; b < i*11+11; b++ {
			idx = idx<<1 | int(bits[b/8]>>(7-b%8)&1)
		}
		words[i] = wordList[idx]
	}
	return words
}

// SplitPhrase normalizes typed input: any whitespace, case-insensitive.
func SplitPhrase(phrase string) []string {
	return strings.Fields(strings.ToLower(phrase))
}

// lookup accepts a list word or its first four letters (BIP-39 words are
// unique by their first four letters, so a shortened word is never
// ambiguous).
func lookup(w string) (int, bool) {
	if i, ok := wordIndex[w]; ok {
		return i, true
	}
	if len(w) != 4 {
		return 0, false
	}
	for i, cand := range wordList {
		if strings.HasPrefix(cand, w) {
			return i, true
		}
	}
	return 0, false
}

// wordsToEntropy decodes and checks a 24-word phrase.
func wordsToEntropy(words []string) ([]byte, error) {
	if len(words) != WordCount {
		return nil, fmt.Errorf("recovery: expected %d words, got %d", WordCount, len(words))
	}
	bits := make([]byte, entropySize+1)
	for i, w := range words {
		idx, ok := lookup(w)
		if !ok {
			return nil, &WordError{Position: i + 1, Word: w}
		}
		for k := range 11 {
			b := i*11 + k
			bits[b/8] |= byte(idx>>(10-k)&1) << (7 - b%8)
		}
	}
	entropy := bits[:entropySize]
	if sum := sha256.Sum256(entropy); sum[0] != bits[entropySize] {
		return nil, ErrChecksum
	}
	return entropy, nil
}
