package vault

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// DeviceKey is one device's identity: a P-256 signing key (ECDSA) and a
// P-256 key-agreement key (ECDH, the HPKE recipient key for the wrapped
// vault key). P-256 because it is what the Secure Enclave and Android
// StrongBox keep non-exportable; implementations backed by them come in
// later phases. Private keys never leave the implementation: callers only
// get signatures and key-exchange results.
type DeviceKey interface {
	// SigningPublicKey is the uncompressed SEC 1 point of the signing key.
	SigningPublicKey() []byte
	// AgreementPublicKey is the uncompressed SEC 1 point of the ECDH key.
	AgreementPublicKey() []byte
	// SignDigest signs a SHA-256 digest (ASN.1 ECDSA signature).
	SignDigest(digest []byte) ([]byte, error)
	// KeyExchanger opens HPKE wraps addressed to this device.
	KeyExchanger() ecdh.KeyExchanger
}

// DeviceID is derived from both public keys, so a roster entry can't pair
// one device's id with another device's keys.
func DeviceID(signPub, kexPub []byte) string {
	h := sha256.New()
	h.Write([]byte("uniterm-vault/v1 device\x00"))
	writeField(h, signPub)
	writeField(h, kexPub)
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// IDOf is DeviceID for a DeviceKey.
func IDOf(k DeviceKey) string { return DeviceID(k.SigningPublicKey(), k.AgreementPublicKey()) }

// SoftwareDeviceKey keeps both private keys in memory. It is the stand-in
// until the platform keystores (Keychain, Secure Enclave, Android Keystore)
// back DeviceKey; its Marshal output is as sensitive as a password.
type SoftwareDeviceKey struct {
	sign *ecdsa.PrivateKey
	kex  *ecdh.PrivateKey
}

// NewSoftwareDeviceKey generates a fresh device identity.
func NewSoftwareDeviceKey() (*SoftwareDeviceKey, error) {
	sign, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	kex, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &SoftwareDeviceKey{sign: sign, kex: kex}, nil
}

func (k *SoftwareDeviceKey) SigningPublicKey() []byte {
	b, err := k.sign.PublicKey.Bytes()
	if err != nil {
		panic(err) // P-256 keys always encode
	}
	return b
}

func (k *SoftwareDeviceKey) AgreementPublicKey() []byte { return k.kex.PublicKey().Bytes() }

func (k *SoftwareDeviceKey) SignDigest(digest []byte) ([]byte, error) {
	return ecdsa.SignASN1(rand.Reader, k.sign, digest)
}

func (k *SoftwareDeviceKey) KeyExchanger() ecdh.KeyExchanger { return k.kex }

type softwareKeyJSON struct {
	Sign []byte `json:"sign"`
	Kex  []byte `json:"kex"`
}

// Marshal serializes the private keys (for the platform keychain).
func (k *SoftwareDeviceKey) Marshal() ([]byte, error) {
	sign, err := k.sign.Bytes()
	if err != nil {
		return nil, err
	}
	return json.Marshal(softwareKeyJSON{Sign: sign, Kex: k.kex.Bytes()})
}

// UnmarshalSoftwareDeviceKey is the inverse of Marshal.
func UnmarshalSoftwareDeviceKey(data []byte) (*SoftwareDeviceKey, error) {
	var j softwareKeyJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	sign, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), j.Sign)
	if err != nil {
		return nil, fmt.Errorf("vault: device signing key: %w", err)
	}
	kex, err := ecdh.P256().NewPrivateKey(j.Kex)
	if err != nil {
		return nil, fmt.Errorf("vault: device agreement key: %w", err)
	}
	return &SoftwareDeviceKey{sign: sign, kex: kex}, nil
}

var errBadSignature = errors.New("vault: bad signature")

// signedDigest is what gets signed: a domain label (so a roster signature
// can never pass as an op signature) and the exact stored body bytes.
func signedDigest(domain string, body []byte) []byte {
	h := sha256.New()
	h.Write([]byte("uniterm-vault/v1 " + domain + "\x00"))
	h.Write(body)
	return h.Sum(nil)
}

func sign(k DeviceKey, domain string, body []byte) ([]byte, error) {
	return k.SignDigest(signedDigest(domain, body))
}

func verify(signPub []byte, domain string, body, sig []byte) error {
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), signPub)
	if err != nil {
		return fmt.Errorf("vault: signing key: %w", err)
	}
	if !ecdsa.VerifyASN1(pub, signedDigest(domain, body), sig) {
		return errBadSignature
	}
	return nil
}

// writeField writes a length-prefixed field, so concatenations of
// variable-length fields can't be reinterpreted.
func writeField(h interface{ Write([]byte) (int, error) }, b []byte) {
	var n [4]byte
	n[0], n[1], n[2], n[3] = byte(len(b)>>24), byte(len(b)>>16), byte(len(b)>>8), byte(len(b))
	h.Write(n[:])
	h.Write(b)
}
